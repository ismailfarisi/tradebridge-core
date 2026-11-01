package negotiation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/internal/quotes"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// QuoteActor is the subset of quotes.Service the engine drives: it counters
// and accepts through the same code paths a human buyer uses, so every
// engine move lands in the durable quote thread, notifies the seller's agent
// and respects tenant checks.
//
// Counters are split up so the engine can make them crash-safe:
// PrepareCounter validates and resolves the seller's autonomy before any
// transaction is open, RecordCounterTx does every durable write inside the engine's transaction
// (together with the round row), DeliverCounter runs the live side effects
// after commit, and LoadCounterDelivery rebuilds an undelivered counter from
// its rows for replay. See recordRound for the full protocol.
type QuoteActor interface {
	PrepareCounter(ctx context.Context, buyerCompanyID, quoteID string) (quotes.CounterPlan, error)
	RecordCounterTx(ctx context.Context, qtx *sqlc.Queries, plan quotes.CounterPlan, priceCents *int64, deliveryDays *int32, message string) (quotes.CounterDelivery, error)
	DeliverCounter(ctx context.Context, d quotes.CounterDelivery)
	LoadCounterDelivery(ctx context.Context, quoteMessageID string, agentEventID *string) (quotes.CounterDelivery, error)
	Accept(ctx context.Context, buyerCompanyID, quoteID string) (sqlc.Order, error)
}

// ErrNotFound is returned when the RFQ or strategy is missing or belongs to
// another tenant.
var ErrNotFound = errors.New("not found")

// ConflictError is a state conflict surfaced to the API as 409.
type ConflictError struct{ Msg string }

func (e ConflictError) Error() string { return e.Msg }

// ErrBusy is returned to a buyer action that could not obtain the RFQ's
// negotiation lock in time because another API instance is working on it.
var ErrBusy = ConflictError{Msg: "negotiation is being updated; retry in a moment"}

// Engine runs negotiations.
//
// Concurrency: Run executes on every API replica, and buyer actions arrive on
// any replica, so every state-changing entry point (Tick — which covers close
// and finishExternally — Approve, Decline, SetPaused) runs under guard(): an
// in-process mutex plus a Postgres transaction-scoped advisory lock keyed on
// the RFQ. The DB constraints remain a second line of defence:
// UNIQUE(quote_id, responded_to_version) on rounds (the counter message
// commits in the same transaction as its round) and the RFQ row lock +
// status-guarded AcceptQuote in quotes.Accept.
type Engine struct {
	q             *sqlc.Queries
	pool          *pgxpool.Pool // nil disables the cross-instance lock (unit tests)
	guardSlots    chan struct{} // bounds connections parked holding RFQ locks
	quotes        QuoteActor
	notifier      *notify.Notifier
	logger        zerolog.Logger
	sellerTimeout time.Duration
	locks         sync.Map // rfqID -> *sync.Mutex
}

// NewEngine builds an engine. sellerTimeout is how long the engine waits for
// a supplier to answer a counter before treating its last terms as final.
func NewEngine(q *sqlc.Queries, pool *pgxpool.Pool, quotes QuoteActor, notifier *notify.Notifier, logger zerolog.Logger, sellerTimeout time.Duration) *Engine {
	if sellerTimeout <= 0 {
		sellerTimeout = 24 * time.Hour
	}
	e := &Engine{q: q, pool: pool, quotes: quotes, notifier: notifier, logger: logger, sellerTimeout: sellerTimeout}
	if pool != nil {
		// A guard parks one pooled connection for the whole tick and the tick
		// takes at most one more at a time. Capping guards at a quarter of the
		// pool means lock holders can never starve each other (or the rest of
		// the API) of the second connection they need to finish.
		e.guardSlots = make(chan struct{}, max(1, int(pool.Config().MaxConns)/4))
	}
	return e
}

func (e *Engine) lock(rfqID string) func() {
	m, _ := e.locks.LoadOrStore(rfqID, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// buyerLockWait bounds how long a buyer action waits for another instance's
// tick on the same RFQ before answering 409 ErrBusy.
const buyerLockWait = "10s"

// guard takes exclusive ownership of one RFQ's negotiation across every API
// instance. It serialises in-process first (so local contenders don't each
// hold a pooled connection), then opens a guard transaction on its own
// connection and takes pg_(try_)advisory_xact_lock on the RFQ. The lock lives
// exactly as long as that transaction: release() rolls it back, and if the
// process dies Postgres drops the connection and the lock with it, so a
// crashed instance never wedges an RFQ.
//
// The guard transaction holds only the lock; the engine's writes run in their
// own short transactions. That way the lock also covers the post-commit side
// effects of a counter (Redis publish, notifications), which is what makes the
// outbox replay in flushPending safe: a pending round seen while holding the
// lock can only belong to a holder that crashed.
//
// wait=false (engine ticks) gives up immediately when another instance holds
// the RFQ (ok=false); wait=true (buyer actions) blocks up to buyerLockWait.
func (e *Engine) guard(ctx context.Context, rfqID string, wait bool) (release func(), ok bool, err error) {
	unlockMu := e.lock(rfqID)
	if e.pool == nil {
		return unlockMu, true, nil
	}
	select {
	case e.guardSlots <- struct{}{}:
	case <-ctx.Done():
		unlockMu()
		return nil, false, ctx.Err()
	}
	unlock := func() {
		<-e.guardSlots
		unlockMu()
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		unlock()
		return nil, false, err
	}
	abort := func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		unlock()
	}
	gq := e.q.WithTx(tx)
	if wait {
		if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '"+buyerLockWait+"'"); err == nil {
			err = gq.LockNegotiation(ctx, rfqID)
		}
		if err != nil {
			abort()
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "55P03" { // lock_not_available
				return nil, false, ErrBusy
			}
			return nil, false, err
		}
	} else {
		locked, err := gq.TryLockNegotiation(ctx, rfqID)
		if err != nil || !locked {
			abort()
			return nil, false, err
		}
	}
	return abort, true, nil
}

// Run ticks every active negotiation on each interval until ctx is done.
func (e *Engine) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			ids, err := e.q.ListActiveNegotiationRFQIDs(ctx, 200)
			if err != nil {
				e.logger.Error().Err(err).Msg("negotiation: list active")
				continue
			}
			for _, id := range ids {
				if err := e.Tick(ctx, id); err != nil {
					e.logger.Error().Err(err).Str("rfq_id", id).Msg("negotiation: tick")
				}
			}
		}
	}
}

type quoteState struct {
	counters        int
	lastOfferCents  int64
	lastSellerCents int64
	last            *sqlc.NegotiationRound
}

// Tick advances one RFQ's negotiation: answers every unanswered supplier
// version and closes the negotiation once every live quote is settled. When
// another instance is already ticking this RFQ it returns nil without doing
// anything — that instance will make the same moves.
func (e *Engine) Tick(ctx context.Context, rfqID string) error {
	release, ok, err := e.guard(ctx, rfqID, false)
	if err != nil || !ok {
		return err
	}
	defer release()
	return e.tick(ctx, rfqID)
}

// tick is Tick's body; the caller holds guard(rfqID).
func (e *Engine) tick(ctx context.Context, rfqID string) error {
	s, err := e.q.GetNegotiationStrategyByRFQ(ctx, rfqID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	// Finish any counter a crashed holder committed but never delivered,
	// whatever the strategy's status: the counter is already in the seller's
	// thread, only the live notification is owed.
	if err := e.flushPending(ctx, s.ID); err != nil {
		return err
	}
	if s.Status != StatusActive {
		return nil
	}
	r, err := e.q.GetRFQ(ctx, rfqID)
	if err != nil {
		return err
	}
	quotes, err := e.q.RankQuotesByRFQ(ctx, rfqID)
	if err != nil {
		return err
	}
	if r.Status == rfq.StatusClosed || r.Status == rfq.StatusExpired {
		// Bookkeeping only (no counter, no accept), so it runs even while a
		// kill switch is engaged.
		return e.finishExternally(ctx, s, r, quotes)
	}
	if ps, err := e.pauseState(ctx, e.q, s.BuyerCompanyID); err != nil {
		return err
	} else if ps.Paused() {
		return nil // kill switch: no counters, no close, no auto-accept
	}

	rounds, err := e.q.ListNegotiationRounds(ctx, s.ID)
	if err != nil {
		return err
	}
	states := map[string]*quoteState{}
	for i := range rounds {
		rd := rounds[i]
		st := states[rd.QuoteID]
		if st == nil {
			st = &quoteState{}
			states[rd.QuoteID] = st
		}
		if rd.Action == ActionCounter {
			st.counters++
			if rd.OfferPriceCents != nil {
				st.lastOfferCents = *rd.OfferPriceCents
			}
		}
		st.lastSellerCents = rd.SellerPriceCents
		st.last = &rd
	}

	m := mandateOf(s)
	live := make([]sqlc.RankQuotesByRFQRow, 0, len(quotes))
	for _, q := range quotes {
		if q.Status == "submitted" {
			live = append(live, q)
		}
	}

	unsettled := false
	for _, q := range live {
		st := states[q.ID]
		if st == nil {
			st = &quoteState{}
			states[q.ID] = st
		}
		if st.last != nil && st.last.RespondedToVersion == q.Version {
			continue // already answered this version
		}
		h := History{Counters: st.counters, LastOfferCents: st.lastOfferCents, LastSellerCents: st.lastSellerCents, Competitors: len(live) - 1}
		for _, o := range live {
			if o.ID != q.ID && (h.BestRivalCents == 0 || o.PriceCents < h.BestRivalCents) {
				h.BestRivalCents = o.PriceCents
			}
		}
		d := Decide(m, Offer{PriceCents: q.PriceCents, DeliveryDays: q.DeliveryDays, Currency: q.Currency}, h)
		params := sqlc.InsertNegotiationRoundParams{
			StrategyID: s.ID, QuoteID: q.ID, Round: int32(st.counters + 1), RespondedToVersion: q.Version,
			Action: d.Action, SellerPriceCents: q.PriceCents, SellerDeliveryDays: q.DeliveryDays,
			Utility: d.Utility, Rationale: d.Rationale, Message: d.Message,
		}
		if d.Action == ActionCounter {
			offer := d.OfferCents
			params.OfferPriceCents = &offer
			params.OfferDeliveryDays = d.OfferDeliveryDays
		}
		rd, outcome, err := e.recordRound(ctx, s, params, d)
		if err != nil {
			return err
		}
		switch outcome {
		case roundPaused:
			return nil // a kill switch was engaged mid-tick
		case roundSkipped:
			unsettled = true // someone else answered; our view is stale
			continue
		case roundFailed:
			unsettled = true
			e.logger.Error().Str("quote_id", q.ID).Msg("negotiation: counter failed; will retry next tick")
			continue
		}
		if d.Action == ActionCounter {
			st.counters++
			st.lastOfferCents = d.OfferCents
		}
		st.lastSellerCents = q.PriceCents
		st.last = &rd
	}

	if len(live) == 0 || unsettled {
		return nil
	}
	now := time.Now()
	for _, q := range live {
		st := states[q.ID]
		if st.last == nil {
			return nil
		}
		if st.last.Action == ActionCounter && now.Sub(st.last.CreatedAt) < e.sellerTimeout {
			return nil // waiting on this supplier
		}
	}
	return e.close(ctx, s, m, live)
}

// close settles the negotiation once every live quote is answered. Called only
// from tick, under guard.
func (e *Engine) close(ctx context.Context, s sqlc.NegotiationStrategy, m Mandate, live []sqlc.RankQuotesByRFQRow) error {
	cands := make([]Candidate, 0, len(live))
	byID := map[string]sqlc.RankQuotesByRFQRow{}
	for _, q := range live {
		cands = append(cands, Candidate{QuoteID: q.ID, Offer: Offer{PriceCents: q.PriceCents, DeliveryDays: q.DeliveryDays, Currency: q.Currency}})
		byID[q.ID] = q
	}
	best, _, ok := PickBest(m, cands)
	if !ok {
		summary := fmt.Sprintf("No supplier met the %s walk-away price.", money(m.MaxCents, live[0].Currency))
		if _, err := e.setStatus(ctx, s.ID, StatusCompleted, nil, summary); err != nil {
			return err
		}
		e.notify(ctx, s, "Negotiation ended without a deal", summary)
		return nil
	}
	q := byID[best.QuoteID]
	// Re-check the kill switch right before money moves: tick checked it at
	// the start, but a switch may have been engaged while it was countering.
	if ps, err := e.pauseState(ctx, e.q, s.BuyerCompanyID); err != nil {
		return err
	} else if ps.Paused() {
		return nil
	}
	opening := e.openingPrice(ctx, s.RfqID, q.ID, q.PriceCents)
	saved := ""
	if opening > q.PriceCents {
		saved = fmt.Sprintf(", saving %s vs the opening quote", money(opening-q.PriceCents, q.Currency))
	}
	if s.AutoAccept {
		// quotes.Accept commits the order in its own transaction. If we crash
		// before setStatus below, the next tick sees the RFQ closed and
		// finishExternally completes the strategy with the accepted quote.
		if _, err := e.quotes.Accept(ctx, s.BuyerCompanyID, q.ID); err != nil {
			return err
		}
		summary := fmt.Sprintf("Agreed with %s at %s%s.", q.SellerName, money(q.PriceCents, q.Currency), saved)
		if _, err := e.setStatus(ctx, s.ID, StatusCompleted, &q.ID, summary); err != nil {
			return err
		}
		e.notify(ctx, s, "Deal closed by your negotiation agent", summary)
		return nil
	}
	summary := fmt.Sprintf("Best deal: %s at %s%s. Awaiting your approval.", q.SellerName, money(q.PriceCents, q.Currency), saved)
	if _, err := e.setStatus(ctx, s.ID, StatusAwaitingApproval, &q.ID, summary); err != nil {
		return err
	}
	e.notify(ctx, s, "Deal ready for approval", summary)
	return nil
}

// finishExternally completes a negotiation whose RFQ was closed outside the
// engine (manual accept, expiry, cancellation). Called only from tick, under
// guard.
func (e *Engine) finishExternally(ctx context.Context, s sqlc.NegotiationStrategy, r sqlc.Rfq, quotes []sqlc.RankQuotesByRFQRow) error {
	for _, q := range quotes {
		if q.Status == "accepted" {
			id := q.ID
			_, err := e.setStatus(ctx, s.ID, StatusCompleted, &id, fmt.Sprintf("Accepted %s at %s.", q.SellerName, money(q.PriceCents, q.Currency)))
			return err
		}
	}
	_, err := e.setStatus(ctx, s.ID, StatusCancelled, nil, "RFQ "+r.Status+" before a deal was reached.")
	return err
}

func (e *Engine) openingPrice(ctx context.Context, rfqID, quoteID string, fallback int64) int64 {
	fv, err := e.q.ListQuoteFirstVersions(ctx, rfqID)
	if err != nil {
		return fallback
	}
	for _, v := range fv {
		if v.QuoteID == quoteID {
			return v.PriceCents
		}
	}
	return fallback
}

func (e *Engine) setStatus(ctx context.Context, id, status string, outcome *string, summary string) (sqlc.NegotiationStrategy, error) {
	return e.q.SetNegotiationStatus(ctx, sqlc.SetNegotiationStatusParams{ID: id, Status: status, OutcomeQuoteID: outcome, OutcomeSummary: &summary})
}

func (e *Engine) notify(ctx context.Context, s sqlc.NegotiationStrategy, title, body string) {
	if e.notifier == nil {
		return
	}
	e.notifier.NotifyCompany(ctx, s.BuyerCompanyID, notify.Event{
		Type: notify.TypeNegotiationUpdate, Title: title, Body: body,
		Data:     map[string]any{"rfq_id": s.RfqID},
		CTALabel: "Open negotiation", CTAPath: "/negotiations/" + s.RfqID,
	})
}

// Approve accepts the deal the engine is holding for buyer approval. A
// non-empty quoteID awards a different live quote on the same RFQ instead of
// the agent's pick.
func (e *Engine) Approve(ctx context.Context, buyerCompanyID, rfqID, quoteID string) (sqlc.Order, error) {
	release, _, err := e.guard(ctx, rfqID, true)
	if err != nil {
		return sqlc.Order{}, err
	}
	defer release()
	s, err := e.ownedStrategy(ctx, buyerCompanyID, rfqID)
	if err != nil {
		return sqlc.Order{}, err
	}
	if s.Status != StatusAwaitingApproval || s.OutcomeQuoteID == nil {
		return sqlc.Order{}, ConflictError{Msg: "negotiation has no deal awaiting approval"}
	}
	// The kill switch stops every accept that goes through the agent. The
	// buyer can still accept a quote by hand from the quote thread.
	if ps, err := e.pauseState(ctx, e.q, buyerCompanyID); err != nil {
		return sqlc.Order{}, err
	} else if ps.Paused() {
		return sqlc.Order{}, ConflictError{Msg: ps.Message()}
	}
	if quoteID == "" || quoteID == *s.OutcomeQuoteID {
		order, err := e.quotes.Accept(ctx, buyerCompanyID, *s.OutcomeQuoteID)
		if err != nil {
			return sqlc.Order{}, err
		}
		summary := strings.Replace(s.OutcomeSummary, "Awaiting your approval.", "Approved by buyer; order placed.", 1)
		_, err = e.q.SetNegotiationStatus(ctx, sqlc.SetNegotiationStatusParams{ID: s.ID, Status: StatusCompleted, OutcomeQuoteID: s.OutcomeQuoteID, OutcomeSummary: &summary})
		return order, err
	}
	quotes, err := e.q.RankQuotesByRFQ(ctx, rfqID)
	if err != nil {
		return sqlc.Order{}, err
	}
	var chosen *sqlc.RankQuotesByRFQRow
	for i := range quotes {
		if quotes[i].ID == quoteID {
			chosen = &quotes[i]
		}
	}
	if chosen == nil {
		return sqlc.Order{}, ErrNotFound
	}
	order, err := e.quotes.Accept(ctx, buyerCompanyID, chosen.ID)
	if err != nil {
		return sqlc.Order{}, err
	}
	summary := fmt.Sprintf("Buyer chose %s at %s over the agent's pick; order placed.", chosen.SellerName, money(chosen.PriceCents, chosen.Currency))
	_, err = e.q.SetNegotiationStatus(ctx, sqlc.SetNegotiationStatusParams{ID: s.ID, Status: StatusCompleted, OutcomeQuoteID: &chosen.ID, OutcomeSummary: &summary})
	return order, err
}

// Decline rejects the deal held for approval. No order is placed and the
// supplier quotes stay open, so the buyer can still accept one by hand.
func (e *Engine) Decline(ctx context.Context, buyerCompanyID, rfqID string) (sqlc.NegotiationStrategy, error) {
	release, _, err := e.guard(ctx, rfqID, true)
	if err != nil {
		return sqlc.NegotiationStrategy{}, err
	}
	defer release()
	s, err := e.ownedStrategy(ctx, buyerCompanyID, rfqID)
	if err != nil {
		return s, err
	}
	if s.Status != StatusAwaitingApproval {
		return s, ConflictError{Msg: "negotiation has no deal awaiting approval"}
	}
	return e.setStatus(ctx, s.ID, StatusCancelled, nil, "You declined the agent's deal. No order was placed; supplier quotes are still open.")
}

// SetPaused pauses or resumes an active/paused negotiation.
func (e *Engine) SetPaused(ctx context.Context, buyerCompanyID, rfqID string, paused bool) (sqlc.NegotiationStrategy, error) {
	release, _, err := e.guard(ctx, rfqID, true)
	if err != nil {
		return sqlc.NegotiationStrategy{}, err
	}
	defer release()
	s, err := e.ownedStrategy(ctx, buyerCompanyID, rfqID)
	if err != nil {
		return s, err
	}
	from, to := StatusActive, StatusPaused
	if !paused {
		from, to = StatusPaused, StatusActive
	}
	if s.Status != from {
		return s, ConflictError{Msg: "negotiation is " + s.Status}
	}
	return e.q.SetNegotiationStatus(ctx, sqlc.SetNegotiationStatusParams{ID: s.ID, Status: to})
}

func (e *Engine) ownedStrategy(ctx context.Context, buyerCompanyID, rfqID string) (sqlc.NegotiationStrategy, error) {
	s, err := e.q.GetNegotiationStrategyByRFQ(ctx, rfqID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrNotFound
	} else if err != nil {
		return s, err
	}
	if s.BuyerCompanyID != buyerCompanyID {
		return s, ErrNotFound
	}
	return s, nil
}

func mandateOf(s sqlc.NegotiationStrategy) Mandate {
	return Mandate{
		TargetCents: s.TargetPriceCents, MaxCents: s.MaxPriceCents, MaxDeliveryDays: s.MaxDeliveryDays,
		PriceWeight: s.PriceWeight, MaxRounds: int(s.MaxRounds), Tone: s.Tone,
	}
}

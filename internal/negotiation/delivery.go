package negotiation

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/quotes"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

type roundOutcome int

const (
	roundRecorded roundOutcome = iota // committed (and, for a counter, delivered or queued for replay)
	roundSkipped                      // this seller version was already answered
	roundPaused                       // a kill switch is engaged; nothing written
	roundFailed                       // counter could not be written; nothing committed, retried next tick
)

// recordRound commits one engine move. Counter delivery is crash-safe by
// construction — a transactional outbox on the round row:
//
//  1. One transaction (the "move" transaction) re-checks the kill switches,
//     inserts the round, writes the counter's durable rows through
//     quotes.RecordCounterTx (thread message + message.received agent event,
//     which the Gateway replays) and marks the round delivery_status=pending
//     with links to those rows. Either all of it commits or none of it does,
//     so there is never a recorded round without its counter, and the
//     UNIQUE(quote_id, responded_to_version) insert means a second writer can
//     never commit a second counter for the same seller version.
//  2. After commit, DeliverCounter runs the live side effects (Redis thread
//     publish, seller notification, Redis agent broadcast) — these can't join a
//     Postgres transaction, and running them before commit could announce a
//     counter that then rolls back.
//  3. The round is marked sent.
//
// A crash between 1 and 3 leaves a pending round whose counter is already in
// the seller's thread; the next tick (flushPending) replays step 2 from the
// stored rows. Replays are at-least-once for notifications only — never a
// second counter. This was chosen over a single transaction alone (which
// would silently lose the live notification on a crash) and over a
// pending-row-then-call-Counter outbox (which would need the counter's own
// writes to be idempotent).
//
// The caller holds guard(s.RfqID).
func (e *Engine) recordRound(ctx context.Context, s sqlc.NegotiationStrategy, params sqlc.InsertNegotiationRoundParams, d Decision) (sqlc.NegotiationRound, roundOutcome, error) {
	var rd sqlc.NegotiationRound
	if e.pool == nil {
		return rd, roundFailed, errors.New("negotiation: engine has no database pool")
	}
	var plan quotes.CounterPlan
	if d.Action == ActionCounter {
		// Pooled reads happen before the transaction opens: nothing below may
		// take a second connection while the move transaction holds one.
		var err error
		if plan, err = e.quotes.PrepareCounter(ctx, s.BuyerCompanyID, params.QuoteID); err != nil {
			e.logger.Error().Err(err).Str("quote_id", params.QuoteID).Msg("negotiation: prepare counter")
			return rd, roundFailed, nil
		}
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return rd, roundFailed, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	qtx := e.q.WithTx(tx)

	// Read under FOR SHARE in the move transaction itself, so a platform
	// kill switch flipped after this point waits for us, and one flipped
	// before it is seen.
	ps, err := e.pauseState(ctx, qtx, s.BuyerCompanyID)
	if err != nil {
		return rd, roundFailed, err
	}
	if ps.Paused() {
		return rd, roundPaused, nil
	}
	rd, err = qtx.InsertNegotiationRound(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return rd, roundSkipped, nil // answered by a concurrent writer
	} else if err != nil {
		return rd, roundFailed, err
	}
	if d.Action != ActionCounter {
		if err := tx.Commit(ctx); err != nil {
			return rd, roundFailed, err
		}
		return rd, roundRecorded, nil
	}

	delivery, err := e.quotes.RecordCounterTx(ctx, qtx, plan, params.OfferPriceCents, params.OfferDeliveryDays, params.OfferPaymentTermsDays, d.Message)
	if err != nil {
		e.logger.Error().Err(err).Str("quote_id", params.QuoteID).Msg("negotiation: record counter")
		return rd, roundFailed, nil
	}
	var evID *string
	if delivery.Event != nil {
		evID = &delivery.Event.ID
	}
	rd, err = qtx.SetNegotiationRoundPending(ctx, sqlc.SetNegotiationRoundPendingParams{ID: rd.ID, QuoteMessageID: &delivery.Message.ID, AgentEventID: evID})
	if err != nil {
		return rd, roundFailed, err
	}
	if err := tx.Commit(ctx); err != nil {
		return rd, roundFailed, err
	}

	e.quotes.DeliverCounter(ctx, delivery)
	if err := e.q.MarkNegotiationRoundSent(ctx, rd.ID); err != nil {
		// The counter is committed; flushPending will re-deliver and mark it.
		e.logger.Warn().Err(err).Str("round_id", rd.ID).Msg("negotiation: mark round sent")
	}
	return rd, roundRecorded, nil
}

// flushPending replays the live side effects of counters whose move
// transaction committed but whose delivery was never confirmed (the holder
// crashed between commit and MarkNegotiationRoundSent). The caller holds
// guard(rfq), and the guard covers delivery, so every pending round seen here
// belongs to a dead holder.
func (e *Engine) flushPending(ctx context.Context, strategyID string) error {
	pending, err := e.q.ListPendingNegotiationDeliveries(ctx, strategyID)
	if err != nil {
		return err
	}
	for _, rd := range pending {
		if rd.QuoteMessageID != nil {
			d, err := e.quotes.LoadCounterDelivery(ctx, *rd.QuoteMessageID, rd.AgentEventID)
			if err != nil {
				e.logger.Error().Err(err).Str("round_id", rd.ID).Msg("negotiation: load pending counter")
				continue
			}
			e.quotes.DeliverCounter(ctx, d)
		}
		if err := e.q.MarkNegotiationRoundSent(ctx, rd.ID); err != nil {
			return err
		}
		e.logger.Info().Str("round_id", rd.ID).Msg("negotiation: replayed undelivered counter")
	}
	return nil
}

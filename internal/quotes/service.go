// Package quotes normalizes seller quotes (from the agent's quote.submit
// command), ranks them for the buyer, and — on acceptance — auto-creates a
// purchase order and notifies the seller's agent with an order.created event.
package quotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/activation"
	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/invoicing"
	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/internal/thread"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// mustJSON marshals v for a best-effort thread publish. A marshal failure
// (which shouldn't happen for these concrete row types) yields nil, which
// Publisher.PublishMessage/PublishVersion treat as an empty (omitted) field
// rather than panicking.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// ErrNotFound is returned when a quote (or its RFQ) doesn't exist or isn't
// visible to the caller; the HTTP layer maps it to 404.
var ErrNotFound = errors.New("not found")

// ConflictError signals an illegal operation on the current state (→ HTTP 409).
type ConflictError struct{ Msg string }

func (e ConflictError) Error() string { return e.Msg }

// Normalized is a validated quote ready to persist.
type Normalized struct {
	PriceCents   int64
	Currency     string
	DeliveryDays *int32
	// PaymentTermsDays is the seller's payment terms in days after delivery
	// (0 = cash / on delivery). Optional: nil means not stated, and on a
	// revision it leaves the previous terms unchanged.
	PaymentTermsDays *int32
	Notes            string
	Completeness     float64
}

// MaxPaymentTermsDays bounds payment terms on quotes and counters.
const MaxPaymentTermsDays = 365

type submitPayload struct {
	Price        *float64 `json:"price"`
	Currency     string   `json:"currency"`
	DeliveryDays *int32   `json:"delivery_days"`
	Notes        string   `json:"notes"`
	// PaymentTermsDays is optional, so agents built before payment terms
	// existed keep working unchanged.
	PaymentTermsDays *int32 `json:"payment_terms_days"`
}

// Normalize validates and canonicalizes a quote.submit payload. Price is
// required and positive; money is stored as integer cents. Completeness is the
// fraction of {price, delivery_days, notes} the seller supplied.
func Normalize(payload []byte) (Normalized, error) {
	var p submitPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return Normalized{}, errors.New("quote payload is not valid JSON")
	}
	if p.Price == nil || *p.Price <= 0 {
		return Normalized{}, errors.New("quote payload requires a positive price")
	}
	if p.PaymentTermsDays != nil && (*p.PaymentTermsDays < 0 || *p.PaymentTermsDays > MaxPaymentTermsDays) {
		return Normalized{}, fmt.Errorf("payment_terms_days must be between 0 and %d", MaxPaymentTermsDays)
	}
	currency := strings.ToUpper(strings.TrimSpace(p.Currency))
	if currency == "" {
		currency = "AED"
	}
	n := Normalized{
		PriceCents:   int64(math.Round(*p.Price * 100)),
		Currency:     currency,
		DeliveryDays: p.DeliveryDays,
		Notes:        strings.TrimSpace(p.Notes),

		PaymentTermsDays: p.PaymentTermsDays,
	}
	filled := 1.0 // price is always present
	if p.DeliveryDays != nil {
		filled++
	}
	if n.Notes != "" {
		filled++
	}
	n.Completeness = filled / 3.0
	return n, nil
}

// Record upserts the seller's quote for an RFQ (one live quote per seller),
// keeping version 1 of its immutable history current (a resubmit before any
// counter-offer overwrites v1 rather than appending a new version row).
func Record(ctx context.Context, q *sqlc.Queries, pool *pgxpool.Pool, rfqID, sellerCompanyID, commandID string, n Normalized) (sqlc.Quote, error) {
	var cmd *string
	if commandID != "" {
		cmd = &commandID
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return sqlc.Quote{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)

	quote, err := qtx.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID:           rfqID,
		SellerCompanyID: sellerCompanyID,
		AgentCommandID:  cmd,
		PriceCents:      n.PriceCents,
		Currency:        n.Currency,
		DeliveryDays:    n.DeliveryDays,
		Notes:           n.Notes,
		Completeness:    n.Completeness,

		PaymentTermsDays: n.PaymentTermsDays,
	})
	if err != nil {
		return sqlc.Quote{}, err
	}
	// If a counter chain already exists (head past version 1), a fresh seller
	// submit is a NEW immutable version — never overwrite the counter's row.
	// Bump the head so the appended seller version lands above the counter.
	if quote.Version > 1 {
		quote, err = qtx.BumpQuoteHead(ctx, sqlc.BumpQuoteHeadParams{
			ID: quote.ID, PriceCents: n.PriceCents, Currency: n.Currency,
			DeliveryDays: n.DeliveryDays, Notes: n.Notes, Completeness: n.Completeness,
			PaymentTermsDays: n.PaymentTermsDays,
		})
		if err != nil {
			return sqlc.Quote{}, err
		}
	}
	// Keep this seller version current: v1 in the normal pre-counter case (a
	// resubmit overwrites v1 in place), or the freshly bumped version otherwise.
	if _, err := qtx.UpsertQuoteVersion(ctx, sqlc.UpsertQuoteVersionParams{
		QuoteID: quote.ID, Version: quote.Version, PriceCents: quote.PriceCents, Currency: quote.Currency,
		DeliveryDays: quote.DeliveryDays, Notes: quote.Notes, Completeness: quote.Completeness,
		Source: "seller", AgentCommandID: cmd, PaymentTermsDays: quote.PaymentTermsDays,
	}); err != nil {
		return sqlc.Quote{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Quote{}, err
	}
	activation.Emit(ctx, q, sellerCompanyID, "", activation.EventFirstQuote, nil)
	return quote, nil
}

// RecordCounter applies a seller-agent counter to the quote head (new version)
// and appends the immutable version row, atomically in one transaction.
func RecordCounter(ctx context.Context, q *sqlc.Queries, pool *pgxpool.Pool, quoteID, commandID string, n Normalized) (sqlc.Quote, sqlc.QuoteVersion, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return sqlc.Quote{}, sqlc.QuoteVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := q.WithTx(tx)
	head, err := qtx.BumpQuoteHead(ctx, sqlc.BumpQuoteHeadParams{
		ID: quoteID, PriceCents: n.PriceCents, Currency: n.Currency,
		DeliveryDays: n.DeliveryDays, Notes: n.Notes, Completeness: n.Completeness,
		PaymentTermsDays: n.PaymentTermsDays,
	})
	if err != nil {
		return sqlc.Quote{}, sqlc.QuoteVersion{}, err
	}
	var cmd *string
	if commandID != "" {
		cmd = &commandID
	}
	ver, err := qtx.UpsertQuoteVersion(ctx, sqlc.UpsertQuoteVersionParams{
		QuoteID: head.ID, Version: head.Version, PriceCents: head.PriceCents, Currency: head.Currency,
		DeliveryDays: head.DeliveryDays, Notes: head.Notes, Completeness: head.Completeness,
		Source: "counter", AgentCommandID: cmd, PaymentTermsDays: head.PaymentTermsDays,
	})
	if err != nil {
		return sqlc.Quote{}, sqlc.QuoteVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Quote{}, sqlc.QuoteVersion{}, err
	}
	return head, ver, nil
}

// Service serves buyer-facing ranking and acceptance.
type Service struct {
	q        *sqlc.Queries
	pool     *pgxpool.Pool
	pub      *eventbus.Publisher
	loader   *attachments.Loader
	logger   zerolog.Logger
	notifier *notify.Notifier
	inv      *invoicing.Service
	ent      *entitlements.Service
	thread   *thread.Publisher
}

// NewService builds the quotes service.
func NewService(q *sqlc.Queries, pool *pgxpool.Pool, pub *eventbus.Publisher, loader *attachments.Loader, logger zerolog.Logger, notifier *notify.Notifier, inv *invoicing.Service, ent *entitlements.Service, threadPub *thread.Publisher) *Service {
	return &Service{q: q, pool: pool, pub: pub, loader: loader, logger: logger, notifier: notifier, inv: inv, ent: ent, thread: threadPub}
}

// QuoteMessageEvent is the agent event emitted when a buyer posts in a quote
// thread, closing the clarification loop (the agent asked via
// clarification.request; this is how it hears the answer).
type QuoteMessageEvent struct {
	Type        string             `json:"type"`
	RFQID       string             `json:"rfq_id"`
	QuoteID     string             `json:"quote_id"`
	Author      string             `json:"author"`
	Body        string             `json:"body"`
	Attachments []attachments.Meta `json:"attachments,omitempty"`
}

// AddMessage appends a thread message (with optional attachments) and, when
// the author is the buyer, emits a quote.message event to the seller's agent.
// Event emission is best-effort: the message row is the source of truth.
func (s *Service) AddMessage(ctx context.Context, q sqlc.Quote, authorCompanyID, body string, attachmentIDs []string) (sqlc.QuoteMessage, error) {
	author := "buyer"
	if q.SellerCompanyID == authorCompanyID {
		author = "seller"
	}
	var metas []attachments.Meta
	if len(attachmentIDs) > 0 {
		if s.loader == nil {
			return sqlc.QuoteMessage{}, fiber.NewError(fiber.StatusBadRequest, "attachments not supported")
		}
		var err error
		metas, err = s.loader.MetasOwned(ctx, attachmentIDs, authorCompanyID)
		if err != nil {
			return sqlc.QuoteMessage{}, err
		}
	}
	var attsJSON []byte
	if len(metas) > 0 {
		attsJSON, _ = json.Marshal(metas)
	}
	msg, err := s.q.CreateQuoteMessage(ctx, sqlc.CreateQuoteMessageParams{
		QuoteID: q.ID, Author: author, Body: body, Attachments: attsJSON,
	})
	if err != nil {
		return msg, err
	}
	s.thread.PublishMessage(ctx, q.ID, mustJSON(messageView(msg)))
	if len(attachmentIDs) > 0 {
		if err := s.q.SetAttachmentsRFQ(ctx, sqlc.SetAttachmentsRFQParams{
			RfqID: &q.RfqID, Ids: attachmentIDs, CompanyID: authorCompanyID,
		}); err != nil {
			s.logger.Warn().Err(err).Str("quote_id", q.ID).Msg("failed to stamp attachments")
		}
	}
	if author == "buyer" {
		ev := QuoteMessageEvent{
			Type: "quote.message", RFQID: q.RfqID, QuoteID: q.ID,
			Author: author, Body: body, Attachments: metas,
		}
		b, _ := json.Marshal(ev)
		if err := s.pub.Publish(ctx, q.SellerCompanyID, "quote.message", b); err != nil {
			s.logger.Warn().Err(err).Str("quote_id", q.ID).Msg("quote.message publish failed")
		}
	}
	return msg, nil
}

// counterTerms is the buyer's proposed terms in a counter-offer, mirrored into
// the message.received event payload so the seller's agent can reason about
// them without re-parsing the thread body.
type counterTerms struct {
	PriceCents   *int64 `json:"price_cents,omitempty"`
	DeliveryDays *int32 `json:"delivery_days,omitempty"`
	// PaymentTermsDays is omitted when the buyer proposed no terms, so the
	// event is byte-identical for agents that predate payment terms.
	PaymentTermsDays *int32 `json:"payment_terms_days,omitempty"`
}

// threadEntry is one message in a quote's thread, as carried in the
// message.received event so the agent has full context, not just the latest
// message.
type threadEntry struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// messageReceivedEvent is the agent event emitted when a buyer counters a
// quote and the seller has agent autonomy — it carries the proposed terms
// plus the full thread so the agent can reply with a quote.counter command.
type messageReceivedEvent struct {
	Type    string        `json:"type"`
	QuoteID string        `json:"quote_id"`
	RFQID   string        `json:"rfq_id"`
	Counter counterTerms  `json:"counter"`
	Thread  []threadEntry `json:"thread"`
}

// buildThread loads a quote's message history for embedding in an agent
// event. Best-effort: a lookup failure yields an empty thread rather than
// failing the counter.
func buildThread(ctx context.Context, q *sqlc.Queries, quoteID string) []threadEntry {
	msgs, err := q.ListQuoteMessages(ctx, quoteID)
	if err != nil {
		return nil
	}
	out := make([]threadEntry, len(msgs))
	for i, m := range msgs {
		out[i] = threadEntry{Author: m.Author, Body: m.Body, CreatedAt: m.CreatedAt}
	}
	return out
}

// counterBody renders the buyer-facing thread message for a counter-offer:
// their free-text message, plus a human-readable summary of any proposed
// terms. The price is shown in the quote's currency.
func counterBody(message string, priceCents *int64, deliveryDays, paymentTermsDays *int32, currency string) string {
	var terms []string
	if priceCents != nil {
		terms = append(terms, "price: "+formatAmount(*priceCents, currency))
	}
	if deliveryDays != nil {
		terms = append(terms, fmt.Sprintf("delivery: %d days", *deliveryDays))
	}
	if paymentTermsDays != nil {
		terms = append(terms, "payment: "+PaymentTermsLabel(*paymentTermsDays))
	}
	if len(terms) == 0 {
		return message
	}
	summary := "Proposed terms — " + strings.Join(terms, ", ")
	if message == "" {
		return summary
	}
	return message + "\n\n" + summary
}

// Counter records a buyer's counter-offer message on the thread and, only
// when the seller has agent autonomy, emits a message.received event to the
// seller's agent carrying the proposed terms + thread. Returns the created
// message. Only the buyer on the quote's RFQ may counter (ErrNotFound
// otherwise, so as not to leak existence across tenants).
//
// The durable writes (thread message + agent event) commit in one
// transaction; the live side effects run after commit (see DeliverCounter).
func (s *Service) Counter(ctx context.Context, buyerCompanyID, quoteID string, priceCents *int64, deliveryDays, paymentTermsDays *int32, message string) (sqlc.QuoteMessage, error) {
	if err := validPaymentTerms(paymentTermsDays); err != nil {
		return sqlc.QuoteMessage{}, err
	}
	plan, err := s.PrepareCounter(ctx, buyerCompanyID, quoteID)
	if err != nil {
		return sqlc.QuoteMessage{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sqlc.QuoteMessage{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := s.RecordCounterTx(ctx, s.q.WithTx(tx), plan, priceCents, deliveryDays, paymentTermsDays, message)
	if err != nil {
		return sqlc.QuoteMessage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.QuoteMessage{}, err
	}
	s.DeliverCounter(ctx, d)
	return d.Message, nil
}

// CounterPlan is a validated counter target: the quote, its RFQ and whether
// the seller's agent answers autonomously. PrepareCounter resolves it with
// ordinary pooled reads BEFORE the caller opens its transaction, so
// RecordCounterTx never needs a second connection while holding one (nested
// pool acquisition can exhaust the pool under load and deadlock).
type CounterPlan struct {
	Quote      sqlc.Quote
	RFQ        sqlc.Rfq
	Autonomous bool
}

// PrepareCounter checks that buyerCompanyID owns the quote's RFQ (ErrNotFound
// otherwise, so as not to leak existence) and resolves the seller's autonomy.
func (s *Service) PrepareCounter(ctx context.Context, buyerCompanyID, quoteID string) (CounterPlan, error) {
	quote, err := s.q.GetQuote(ctx, quoteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CounterPlan{}, ErrNotFound
	} else if err != nil {
		return CounterPlan{}, err
	}
	r, err := s.q.GetRFQ(ctx, quote.RfqID)
	if err != nil {
		return CounterPlan{}, err
	}
	if r.BuyerCompanyID != buyerCompanyID {
		return CounterPlan{}, ErrNotFound // party-only, no leak
	}
	return CounterPlan{Quote: quote, RFQ: r, Autonomous: s.ent != nil && s.ent.For(ctx, quote.SellerCompanyID).AgentAutonomy}, nil
}

// CounterDelivery is a committed counter-offer whose live side effects are
// still owed: the Redis thread publish, the seller notification (no agent
// autonomy) or the Redis broadcast of the durable message.received event.
type CounterDelivery struct {
	Message         sqlc.QuoteMessage
	Event           *sqlc.AgentEvent // durable message.received; nil without seller agent autonomy
	Autonomous      bool
	SellerCompanyID string
	RFQID           string
	RFQTitle        string
}

// RecordCounterTx performs every durable write of a counter-offer inside the
// caller's transaction (qtx), using only qtx: the buyer thread message and,
// when the seller has agent autonomy, the message.received agent event (the
// Gateway replays agent_events, so the seller's agent receives it even if the
// live publish is lost). Nothing leaves Postgres here: the caller commits,
// then calls DeliverCounter. That split lets the negotiation engine commit
// its round row and the counter in the same transaction.
func (s *Service) RecordCounterTx(ctx context.Context, qtx *sqlc.Queries, plan CounterPlan, priceCents *int64, deliveryDays, paymentTermsDays *int32, message string) (CounterDelivery, error) {
	if err := validPaymentTerms(paymentTermsDays); err != nil {
		return CounterDelivery{}, err
	}
	quote, r := plan.Quote, plan.RFQ
	body := counterBody(message, priceCents, deliveryDays, paymentTermsDays, quote.Currency)
	msg, err := qtx.CreateQuoteMessage(ctx, sqlc.CreateQuoteMessageParams{
		QuoteID: quote.ID, Author: "buyer", Body: body, Attachments: nil,
	})
	if err != nil {
		return CounterDelivery{}, err
	}
	d := CounterDelivery{
		Message: msg, SellerCompanyID: quote.SellerCompanyID, RFQID: r.ID, RFQTitle: r.Title,
		Autonomous: plan.Autonomous,
	}
	// Round-trip to the agent ONLY if the seller has agent autonomy.
	if d.Autonomous && s.pub != nil {
		payload, err := json.Marshal(messageReceivedEvent{
			Type: "message.received", QuoteID: quote.ID, RFQID: quote.RfqID,
			Counter: counterTerms{PriceCents: priceCents, DeliveryDays: deliveryDays, PaymentTermsDays: paymentTermsDays},
			Thread:  buildThread(ctx, qtx, quote.ID),
		})
		if err != nil {
			return CounterDelivery{}, err
		}
		ev, err := qtx.CreateAgentEvent(ctx, sqlc.CreateAgentEventParams{
			CompanyID: quote.SellerCompanyID, Type: "message.received", Payload: payload,
		})
		if err != nil {
			return CounterDelivery{}, err
		}
		d.Event = &ev
	}
	return d, nil
}

// validPaymentTerms rejects payment terms outside 0..MaxPaymentTermsDays.
func validPaymentTerms(days *int32) error {
	if days != nil && (*days < 0 || *days > MaxPaymentTermsDays) {
		return InvalidError{Msg: fmt.Sprintf("payment_terms_days must be between 0 and %d", MaxPaymentTermsDays)}
	}
	return nil
}

// DeliverCounter runs a committed counter's live side effects. Each is
// best-effort and safe to repeat: the thread stream and agent broadcast are
// fast paths over durable rows (clients and the Gateway dedupe by id), and a
// repeated notification is the worst case of a replay.
func (s *Service) DeliverCounter(ctx context.Context, d CounterDelivery) {
	s.thread.PublishMessage(ctx, d.Message.QuoteID, mustJSON(messageView(d.Message)))
	// Without an autonomous agent a person has to answer, so tell the seller.
	if !d.Autonomous && s.notifier != nil {
		s.notifier.NotifyCompany(ctx, d.SellerCompanyID, notify.Event{
			Type:     notify.TypeQuoteCountered,
			Title:    "Buyer sent a counter-offer: " + d.RFQTitle,
			Body:     "Review the buyer's terms and revise your quote or reply.",
			Data:     map[string]any{"rfq_id": d.RFQID, "quote_id": d.Message.QuoteID},
			CTALabel: "View RFQ",
			CTAPath:  "/seller/rfqs/" + d.RFQID,
		})
	}
	if d.Event != nil && s.pub != nil {
		s.pub.PublishExisting(ctx, *d.Event)
	}
}

// LoadCounterDelivery rebuilds a committed counter's pending delivery from
// its durable rows, so a crash between commit and DeliverCounter can be
// replayed.
func (s *Service) LoadCounterDelivery(ctx context.Context, quoteMessageID string, agentEventID *string) (CounterDelivery, error) {
	msg, err := s.q.GetQuoteMessage(ctx, quoteMessageID)
	if err != nil {
		return CounterDelivery{}, err
	}
	quote, err := s.q.GetQuote(ctx, msg.QuoteID)
	if err != nil {
		return CounterDelivery{}, err
	}
	r, err := s.q.GetRFQ(ctx, quote.RfqID)
	if err != nil {
		return CounterDelivery{}, err
	}
	d := CounterDelivery{Message: msg, SellerCompanyID: quote.SellerCompanyID, RFQID: r.ID, RFQTitle: r.Title}
	if agentEventID != nil {
		ev, err := s.q.GetAgentEventByID(ctx, *agentEventID)
		if err != nil {
			return CounterDelivery{}, err
		}
		d.Event, d.Autonomous = &ev, true
	} else {
		d.Autonomous = s.ent != nil && s.ent.For(ctx, quote.SellerCompanyID).AgentAutonomy && s.pub != nil
	}
	return d, nil
}

type orderCreatedEvent struct {
	Type            string `json:"type"`
	OrderID         string `json:"order_id"`
	RFQID           string `json:"rfq_id"`
	QuoteID         string `json:"quote_id"`
	BuyerCompanyID  string `json:"buyer_company_id"`
	SellerCompanyID string `json:"seller_company_id"`
	TotalCents      int64  `json:"total_cents"`
	Currency        string `json:"currency"`
}

type quoteRejectedEvent struct {
	Type            string `json:"type"`
	RFQID           string `json:"rfq_id"`
	QuoteID         string `json:"quote_id"`
	SellerCompanyID string `json:"seller_company_id"`
	Reason          string `json:"reason"`
}

// Accept accepts a quote on behalf of the buyer: it marks the quote accepted,
// rejects the other quotes on the RFQ, creates the purchase order, closes the
// RFQ, and notifies the seller's agent — all in one transaction, with the live
// order.created publish deferred until after commit.
func (s *Service) Accept(ctx context.Context, buyerCompanyID, quoteID string) (sqlc.Order, error) {
	quote, err := s.q.GetQuote(ctx, quoteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Order{}, ErrNotFound
	} else if err != nil {
		return sqlc.Order{}, err
	}
	r, err := s.q.GetRFQ(ctx, quote.RfqID)
	if err != nil {
		return sqlc.Order{}, err
	}
	if r.BuyerCompanyID != buyerCompanyID {
		return sqlc.Order{}, ErrNotFound // don't leak existence across tenants
	}
	if quote.Status != "submitted" {
		return sqlc.Order{}, ConflictError{Msg: "quote is not in a submitted state"}
	}
	if !rfq.CanTransition(r.Status, rfq.StatusClosed) {
		return sqlc.Order{}, ConflictError{Msg: "rfq cannot be closed from status " + r.Status}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sqlc.Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	// Serialise accepts per RFQ (row lock) and re-check under the lock: the
	// pre-checks above ran outside the transaction, so a concurrent accept on
	// another quote of the same RFQ may have closed it since.
	locked, err := qtx.LockRFQForAccept(ctx, quote.RfqID)
	if err != nil {
		return sqlc.Order{}, err
	}
	if !rfq.CanTransition(locked.Status, rfq.StatusClosed) {
		return sqlc.Order{}, ConflictError{Msg: "rfq cannot be closed from status " + locked.Status}
	}

	// Capture the still-submitted losing sellers before RejectOtherQuotes flips
	// their status, so we know exactly who to notify.
	losers, err := qtx.ListSubmittedQuoteSellersExcept(ctx, sqlc.ListSubmittedQuoteSellersExceptParams{
		RfqID: quote.RfqID, ID: quoteID,
	})
	if err != nil {
		return sqlc.Order{}, err
	}

	if _, err := qtx.AcceptQuote(ctx, quoteID); errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Order{}, ConflictError{Msg: "quote is not in a submitted state"}
	} else if err != nil {
		return sqlc.Order{}, err
	}
	if err := qtx.RejectOtherQuotes(ctx, sqlc.RejectOtherQuotesParams{RfqID: quote.RfqID, ID: quoteID}); err != nil {
		return sqlc.Order{}, err
	}
	order, err := qtx.CreateOrder(ctx, sqlc.CreateOrderParams{
		RfqID:           quote.RfqID,
		QuoteID:         quote.ID,
		BuyerCompanyID:  buyerCompanyID,
		SellerCompanyID: quote.SellerCompanyID,
		TotalCents:      quote.PriceCents,
		Currency:        quote.Currency,
	})
	if err != nil {
		return sqlc.Order{}, err
	}
	if _, err := qtx.SetRFQStatus(ctx, sqlc.SetRFQStatusParams{ID: quote.RfqID, Status: rfq.StatusClosed}); err != nil {
		return sqlc.Order{}, err
	}

	// The invoice is created in the same transaction as the order so an
	// order always has exactly one invoice: a rollback leaves none.
	invoice, err := s.inv.CreateForOrder(ctx, qtx, order, r)
	if err != nil {
		return sqlc.Order{}, err
	}

	body, err := json.Marshal(orderCreatedEvent{
		Type:            "order.created",
		OrderID:         order.ID,
		RFQID:           order.RfqID,
		QuoteID:         order.QuoteID,
		BuyerCompanyID:  order.BuyerCompanyID,
		SellerCompanyID: order.SellerCompanyID,
		TotalCents:      order.TotalCents,
		Currency:        order.Currency,
	})
	if err != nil {
		return sqlc.Order{}, err
	}
	// Write-always: the durable event is written inside the transaction so it
	// can never be lost if the commit succeeds.
	ev, err := qtx.CreateAgentEvent(ctx, sqlc.CreateAgentEventParams{
		CompanyID: order.SellerCompanyID,
		Type:      "order.created",
		Payload:   body,
	})
	if err != nil {
		return sqlc.Order{}, err
	}

	// Write-always: durable quote.rejected events for each losing seller, same
	// transaction as order.created above.
	rejectedEvs := make([]sqlc.AgentEvent, 0, len(losers))
	for _, l := range losers {
		rb, err := json.Marshal(quoteRejectedEvent{
			Type:            "quote.rejected",
			RFQID:           quote.RfqID,
			QuoteID:         l.ID,
			SellerCompanyID: l.SellerCompanyID,
			Reason:          "not_selected",
		})
		if err != nil {
			return sqlc.Order{}, err
		}
		rev, err := qtx.CreateAgentEvent(ctx, sqlc.CreateAgentEventParams{
			CompanyID: l.SellerCompanyID,
			Type:      "quote.rejected",
			Payload:   rb,
		})
		if err != nil {
			return sqlc.Order{}, err
		}
		rejectedEvs = append(rejectedEvs, rev)
	}

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Order{}, err
	}
	activation.Emit(ctx, s.q, order.BuyerCompanyID, "", activation.EventFirstOrder, nil)

	// Live publish only after the durable state is committed.
	s.pub.PublishExisting(ctx, ev)
	for _, rev := range rejectedEvs {
		s.pub.PublishExisting(ctx, rev)
	}

	if s.notifier != nil {
		nev := notify.Event{
			Type:     notify.TypeOrderCreated,
			Title:    "Order placed",
			Body:     "A purchase order was created from the accepted quote.",
			Data:     map[string]any{"order_id": order.ID, "rfq_id": order.RfqID},
			CTALabel: "View order",
			CTAPath:  "/orders/" + order.ID,
		}
		s.notifier.NotifyCompany(ctx, order.BuyerCompanyID, nev)
		s.notifier.NotifyCompany(ctx, order.SellerCompanyID, nev)

		s.notifier.NotifyCompany(ctx, order.BuyerCompanyID, notify.Event{
			Type:     notify.TypeInvoiceIssued,
			Title:    "Invoice issued",
			Body:     "Invoice " + invoice.InvoiceNumber + " is ready for your order.",
			Data:     map[string]any{"order_id": order.ID, "invoice_number": invoice.InvoiceNumber},
			CTALabel: "View invoice",
			CTAPath:  "/orders/" + order.ID,
		})
	}
	return order, nil
}

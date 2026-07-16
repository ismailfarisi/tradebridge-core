// Package quotes normalizes seller quotes (from the agent's quote.submit
// command), ranks them for the buyer, and — on acceptance — auto-creates a
// purchase order and notifies the seller's agent with an order.created event.
package quotes

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

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
	Notes        string
	Completeness float64
}

type submitPayload struct {
	Price        *float64 `json:"price"`
	Currency     string   `json:"currency"`
	DeliveryDays *int32   `json:"delivery_days"`
	Notes        string   `json:"notes"`
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
	currency := strings.ToUpper(strings.TrimSpace(p.Currency))
	if currency == "" {
		currency = "USD"
	}
	n := Normalized{
		PriceCents:   int64(math.Round(*p.Price * 100)),
		Currency:     currency,
		DeliveryDays: p.DeliveryDays,
		Notes:        strings.TrimSpace(p.Notes),
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

// Record upserts the seller's quote for an RFQ (one live quote per seller).
func Record(ctx context.Context, q *sqlc.Queries, rfqID, sellerCompanyID, commandID string, n Normalized) (sqlc.Quote, error) {
	var cmd *string
	if commandID != "" {
		cmd = &commandID
	}
	return q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID:           rfqID,
		SellerCompanyID: sellerCompanyID,
		AgentCommandID:  cmd,
		PriceCents:      n.PriceCents,
		Currency:        n.Currency,
		DeliveryDays:    n.DeliveryDays,
		Notes:           n.Notes,
		Completeness:    n.Completeness,
	})
}

// Service serves buyer-facing ranking and acceptance.
type Service struct {
	q        *sqlc.Queries
	pool     *pgxpool.Pool
	pub      *eventbus.Publisher
	loader   *attachments.Loader
	logger   zerolog.Logger
	notifier *notify.Notifier
}

// NewService builds the quotes service.
func NewService(q *sqlc.Queries, pool *pgxpool.Pool, pub *eventbus.Publisher, loader *attachments.Loader, logger zerolog.Logger, notifier *notify.Notifier) *Service {
	return &Service{q: q, pool: pool, pub: pub, loader: loader, logger: logger, notifier: notifier}
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

	// Capture the still-submitted losing sellers before RejectOtherQuotes flips
	// their status, so we know exactly who to notify.
	losers, err := qtx.ListSubmittedQuoteSellersExcept(ctx, sqlc.ListSubmittedQuoteSellersExceptParams{
		RfqID: quote.RfqID, ID: quoteID,
	})
	if err != nil {
		return sqlc.Order{}, err
	}

	if _, err := qtx.AcceptQuote(ctx, quoteID); err != nil {
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

	// Live publish only after the durable state is committed.
	s.pub.PublishExisting(ctx, ev)
	for _, rev := range rejectedEvs {
		s.pub.PublishExisting(ctx, rev)
	}

	if s.notifier != nil {
		nev := notify.Event{
			Type:     notify.TypeOrderCreated,
			Title:    "Order confirmed",
			Body:     "A purchase order was created from the accepted quote.",
			Data:     map[string]any{"order_id": order.ID, "rfq_id": order.RfqID},
			CTALabel: "View order",
			CTAPath:  "/orders/" + order.ID,
		}
		s.notifier.NotifyCompany(ctx, order.BuyerCompanyID, nev)
		s.notifier.NotifyCompany(ctx, order.SellerCompanyID, nev)
	}
	return order, nil
}

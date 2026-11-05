package quotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Limits on a quote a seller types into the web app. The agent path keeps its
// own (looser) contract in Normalize.
const (
	maxSellerNotes        = 2000
	maxSellerDeliveryDays = 3650
)

// InvalidError signals a quote the seller must fix (→ HTTP 400).
type InvalidError struct{ Msg string }

func (e InvalidError) Error() string { return e.Msg }

// SellerQuoteInput is a quote entered by a seller in the web app. Price is in
// major currency units (e.g. 12500.50 AED), like the agent quote.submit payload.
type SellerQuoteInput struct {
	Price        *float64 `json:"price"`
	Currency     string   `json:"currency"`
	DeliveryDays *int32   `json:"delivery_days"`
	Notes        string   `json:"notes"`
	// PaymentTermsDays is optional: days after delivery the buyer has to pay
	// (0 = cash / on delivery). Omitted on a revision keeps the previous terms.
	PaymentTermsDays *int32 `json:"payment_terms_days,omitempty"`
}

// SellerQuoteResult is the outcome of SubmitAsSeller.
type SellerQuoteResult struct {
	Quote   sqlc.Quote
	Revised bool // false for a first quote, true for a revised offer
}

// SubmitAsSeller records a quote a seller entered by hand, the web
// counterpart of the agent's quote.submit and quote.counter commands.
//
// The seller must be matched to the RFQ (ErrNotFound otherwise, so existence
// never leaks) and the RFQ must still accept quotes. The first submission
// creates the quote exactly like quote.submit. Any later submission is a
// revised offer: a new quote version, a seller message in the quote thread
// and a "counter-offer" notification to the buyer, exactly like
// quote.counter. A quote that is no longer live (accepted or rejected)
// cannot be revised.
func (s *Service) SubmitAsSeller(ctx context.Context, sellerCompanyID, rfqID string, in SellerQuoteInput) (SellerQuoteResult, error) {
	matched, err := s.q.RFQMatchExists(ctx, sqlc.RFQMatchExistsParams{RfqID: rfqID, SellerCompanyID: sellerCompanyID})
	if err != nil {
		return SellerQuoteResult{}, err
	}
	if !matched {
		return SellerQuoteResult{}, ErrNotFound
	}
	r, err := s.q.GetRFQ(ctx, rfqID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SellerQuoteResult{}, ErrNotFound
	} else if err != nil {
		return SellerQuoteResult{}, err
	}
	if r.Status != rfq.StatusOpen && r.Status != rfq.StatusQuoted {
		return SellerQuoteResult{}, ConflictError{Msg: "this RFQ is no longer accepting quotes"}
	}

	n, err := normalizeSellerInput(in)
	if err != nil {
		return SellerQuoteResult{}, err
	}

	existing, err := s.q.GetQuoteBySeller(ctx, sqlc.GetQuoteBySellerParams{RfqID: rfqID, SellerCompanyID: sellerCompanyID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return s.firstSellerQuote(ctx, r, sellerCompanyID, n)
	case err != nil:
		return SellerQuoteResult{}, err
	}
	if existing.Status != "submitted" {
		return SellerQuoteResult{}, ConflictError{Msg: "your quote is " + existing.Status + " and can no longer be changed"}
	}
	return s.reviseSellerQuote(ctx, r, existing, n)
}

func normalizeSellerInput(in SellerQuoteInput) (Normalized, error) {
	if in.DeliveryDays != nil && (*in.DeliveryDays < 0 || *in.DeliveryDays > maxSellerDeliveryDays) {
		return Normalized{}, InvalidError{Msg: fmt.Sprintf("delivery_days must be between 0 and %d", maxSellerDeliveryDays)}
	}
	if len([]rune(in.Notes)) > maxSellerNotes {
		return Normalized{}, InvalidError{Msg: fmt.Sprintf("notes must be at most %d characters", maxSellerNotes)}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return Normalized{}, err
	}
	n, err := Normalize(raw)
	if err != nil {
		return Normalized{}, InvalidError{Msg: err.Error()}
	}
	if len(n.Currency) != 3 {
		return Normalized{}, InvalidError{Msg: "currency must be a 3-letter code such as AED"}
	}
	return n, nil
}

func (s *Service) firstSellerQuote(ctx context.Context, r sqlc.Rfq, sellerCompanyID string, n Normalized) (SellerQuoteResult, error) {
	quote, err := Record(ctx, s.q, s.pool, r.ID, sellerCompanyID, "", n)
	if err != nil {
		return SellerQuoteResult{}, err
	}
	if rfq.CanTransition(r.Status, rfq.StatusQuoted) {
		if _, err := s.q.SetRFQStatus(ctx, sqlc.SetRFQStatusParams{ID: r.ID, Status: rfq.StatusQuoted}); err != nil {
			s.logger.Warn().Err(err).Str("rfq_id", r.ID).Msg("failed to advance rfq to quoted")
		}
	}
	if s.notifier != nil {
		count, _ := s.q.CountSubmittedQuotesByRFQ(ctx, r.ID)
		title := "A supplier quoted your RFQ"
		if count > 1 {
			title = fmt.Sprintf("%d suppliers quoted your RFQ", count)
		}
		s.notifier.NotifyCompany(ctx, r.BuyerCompanyID, notify.Event{
			Type:     notify.TypeQuoteSubmitted,
			Title:    title,
			Body:     "Open your RFQ to compare quotes.",
			Data:     map[string]any{"rfq_id": r.ID},
			CTALabel: "Compare quotes",
			CTAPath:  "/rfqs/" + r.ID,
		})
	}
	return SellerQuoteResult{Quote: quote}, nil
}

func (s *Service) reviseSellerQuote(ctx context.Context, r sqlc.Rfq, existing sqlc.Quote, n Normalized) (SellerQuoteResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return SellerQuoteResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)
	head, err := qtx.ReviseSubmittedQuote(ctx, sqlc.ReviseSubmittedQuoteParams{
		ID: existing.ID, SellerCompanyID: existing.SellerCompanyID,
		PriceCents: n.PriceCents, Currency: n.Currency, DeliveryDays: n.DeliveryDays,
		Notes: n.Notes, Completeness: n.Completeness, PaymentTermsDays: n.PaymentTermsDays,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Accepted or rejected between the read above and this write.
		return SellerQuoteResult{}, ConflictError{Msg: "your quote can no longer be changed"}
	} else if err != nil {
		return SellerQuoteResult{}, err
	}
	ver, err := qtx.UpsertQuoteVersion(ctx, sqlc.UpsertQuoteVersionParams{
		QuoteID: head.ID, Version: head.Version, PriceCents: head.PriceCents, Currency: head.Currency,
		DeliveryDays: head.DeliveryDays, Notes: head.Notes, Completeness: head.Completeness,
		Source: "counter", PaymentTermsDays: head.PaymentTermsDays,
	})
	if err != nil {
		return SellerQuoteResult{}, err
	}
	msg, err := qtx.CreateQuoteMessage(ctx, sqlc.CreateQuoteMessageParams{
		QuoteID: head.ID, Author: "seller", Body: revisionMessage(head), Attachments: nil,
	})
	if err != nil {
		return SellerQuoteResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SellerQuoteResult{}, err
	}

	s.thread.PublishVersion(ctx, head.ID, mustJSON(ver))
	s.thread.PublishMessage(ctx, head.ID, mustJSON(messageView(msg)))
	if s.notifier != nil {
		s.notifier.NotifyCompany(ctx, r.BuyerCompanyID, notify.Event{
			Type:     notify.TypeQuoteCountered,
			Title:    "Seller sent a counter-offer",
			Body:     "The seller revised their quote.",
			Data:     map[string]any{"rfq_id": r.ID, "quote_id": head.ID},
			CTALabel: "View thread",
			CTAPath:  "/rfqs/" + r.ID,
		})
	}
	return SellerQuoteResult{Quote: head, Revised: true}, nil
}

// formatAmount renders integer cents as a grouped amount with its currency
// code, e.g. 1160050, "AED" → "11,600.50 AED". An empty currency means AED.
func formatAmount(cents int64, currency string) string {
	if currency == "" {
		currency = "AED"
	}
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := strconv.FormatInt(cents/100, 10)
	var b strings.Builder
	for i, d := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return fmt.Sprintf("%s%s.%02d %s", sign, b.String(), cents%100, currency)
}

// revisionMessage is the thread line the buyer sees for a revised offer: the
// new terms, then the seller's notes if any.
func revisionMessage(q sqlc.Quote) string {
	body := "Revised offer: " + formatAmount(q.PriceCents, q.Currency)
	if q.DeliveryDays != nil {
		body += fmt.Sprintf(", delivery in %d days", *q.DeliveryDays)
	}
	if q.PaymentTermsDays != nil {
		body += ", payment " + PaymentTermsLabel(*q.PaymentTermsDays)
	}
	if q.Notes != "" {
		body += "\n\n" + q.Notes
	}
	return body
}

// PaymentTermsLabel renders payment terms for people: 0 → "cash on
// delivery", 30 → "net 30 days".
func PaymentTermsLabel(days int32) string {
	if days == 0 {
		return "cash on delivery"
	}
	return fmt.Sprintf("net %d days", days)
}

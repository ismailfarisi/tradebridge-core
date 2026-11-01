package negotiation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// itemOf reads the comparable-item description out of an RFQ.
func itemOf(title, category string, requirements []byte) Item {
	it := Item{Title: title, Category: category}
	var req struct {
		Category string          `json:"category"`
		Quantity json.RawMessage `json:"quantity"`
		Keywords []string        `json:"keywords"`
		Location string          `json:"location"`
	}
	if len(requirements) > 0 && json.Unmarshal(requirements, &req) == nil {
		if it.Category == "" {
			it.Category = req.Category
		}
		it.Keywords, it.Location = req.Keywords, req.Location
		var s string
		if json.Unmarshal(req.Quantity, &s) == nil {
			it.Quantity = s
		} else if len(req.Quantity) > 0 && string(req.Quantity) != "null" {
			it.Quantity = string(req.Quantity) // a bare JSON number
		}
	}
	it.Quantity = strings.TrimSpace(it.Quantity)
	return it
}

// baselineInput gathers the openings and purchase history for one deal.
func baselineInput(ctx context.Context, q *sqlc.Queries, r sqlc.Rfq, quoteID string, finalCents int64, currency string, now time.Time) (BaselineInput, error) {
	in := BaselineInput{QuoteID: quoteID, FinalCents: finalCents, Currency: currency, Now: now,
		Item: itemOf(r.Title, r.Category, r.Requirements)}
	ops, err := q.ListQuoteOpenings(ctx, r.ID)
	if err != nil {
		return in, err
	}
	for _, o := range ops {
		in.Openings = append(in.Openings, OpeningQuote{QuoteID: o.QuoteID, PriceCents: o.PriceCents, Currency: o.Currency})
	}
	if strings.TrimSpace(in.Item.Category) == "" {
		return in, nil
	}
	hist, err := q.ListBuyerPurchaseHistory(ctx, sqlc.ListBuyerPurchaseHistoryParams{
		BuyerCompanyID: r.BuyerCompanyID, Currency: currency, Category: in.Item.Category,
		ExcludeRfqID: r.ID, Since: now.Add(-HistoricalMaxAge),
	})
	if err != nil {
		return in, err
	}
	for _, h := range hist {
		in.History = append(in.History, PastPurchase{
			OrderID: h.OrderID, RFQID: h.RfqID, TotalCents: h.TotalCents, Currency: h.Currency, At: h.CreatedAt,
			Item: itemOf(h.RfqTitle, h.Category, h.Requirements),
		})
	}
	return in, nil
}

// snapshotBaselines measures the deal a negotiation closed on and stores the
// result, so reported savings never drift. Failures are logged, not returned:
// reporting must never block a deal.
func (e *Engine) snapshotBaselines(ctx context.Context, s sqlc.NegotiationStrategy, quoteID string, finalCents int64, currency string) Baselines {
	r, err := e.q.GetRFQ(ctx, s.RfqID)
	if err != nil {
		e.logger.Error().Err(err).Str("rfq_id", s.RfqID).Msg("negotiation: baseline rfq")
		return Baselines{}
	}
	in, err := baselineInput(ctx, e.q, r, quoteID, finalCents, currency, time.Now())
	if err != nil {
		e.logger.Error().Err(err).Str("rfq_id", s.RfqID).Msg("negotiation: baseline input")
		return Baselines{}
	}
	b := ComputeBaselines(in)
	if err := saveBaselines(ctx, e.q, s, quoteID, b); err != nil {
		e.logger.Error().Err(err).Str("rfq_id", s.RfqID).Msg("negotiation: baseline snapshot")
	}
	return b
}

func saveBaselines(ctx context.Context, q *sqlc.Queries, s sqlc.NegotiationStrategy, quoteID string, b Baselines) error {
	p := sqlc.UpsertNegotiationBaselineParams{
		StrategyID: s.ID, RfqID: s.RfqID, QuoteID: quoteID, Currency: b.Currency, FinalCents: b.FinalCents,
		OpeningCents: b.OpeningCents, MarketCents: b.MarketCents, MarketQuoteCount: int32(b.MarketQuoteCount),
		HistoricalCents: b.HistoricalCents, Anchored: b.Anchor.Flagged,
		VerifiedBaselineCents: b.VerifiedBaselineCents, VerifiedBasis: b.VerifiedBasis, Reason: b.Reason, Source: "close",
	}
	if b.Historical != nil {
		id := b.Historical.OrderID
		p.HistoricalOrderID = &id
		if raw, err := json.Marshal(b.Historical); err == nil {
			p.HistoricalNote = string(raw)
		}
	}
	if b.Anchor.Flagged {
		pct, basis := b.Anchor.Pct, b.Anchor.Basis
		p.AnchorPct, p.AnchorBasis = &pct, &basis
	}
	_, err := q.UpsertNegotiationBaseline(ctx, p)
	return err
}

// baselinesFromSnapshot rebuilds the view of a stored snapshot.
func baselinesFromSnapshot(nb sqlc.NegotiationBaseline) Baselines {
	b := Baselines{
		Currency: nb.Currency, FinalCents: nb.FinalCents, OpeningCents: nb.OpeningCents,
		MarketCents: nb.MarketCents, MarketQuoteCount: int(nb.MarketQuoteCount), HistoricalCents: nb.HistoricalCents,
		VerifiedBaselineCents: nb.VerifiedBaselineCents, VerifiedBasis: nb.VerifiedBasis, Reason: nb.Reason,
	}
	if nb.HistoricalNote != "" {
		var m HistoricalMatch
		if json.Unmarshal([]byte(nb.HistoricalNote), &m) == nil {
			b.Historical = &m
		}
	}
	if nb.Anchored {
		b.Anchor.Flagged = true
		if nb.AnchorPct != nil {
			b.Anchor.Pct = *nb.AnchorPct
		}
		if nb.AnchorBasis != nil {
			b.Anchor.Basis = *nb.AnchorBasis
		}
		b.Anchor.Note = anchorNote(b.Anchor)
	}
	if b.OpeningCents != nil && *b.OpeningCents > b.FinalCents {
		b.OpeningSavingsCents = *b.OpeningCents - b.FinalCents
	}
	if b.VerifiedBaselineCents > b.FinalCents {
		b.VerifiedSavingsCents = b.VerifiedBaselineCents - b.FinalCents
		b.VerifiedSavingsPct = pctOf(b.VerifiedSavingsCents, b.VerifiedBaselineCents)
	}
	return b
}

func getSnapshot(ctx context.Context, q *sqlc.Queries, strategyID string) (*sqlc.NegotiationBaseline, error) {
	nb, err := q.GetNegotiationBaseline(ctx, strategyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &nb, nil
}

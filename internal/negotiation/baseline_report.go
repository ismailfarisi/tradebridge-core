package negotiation

import (
	"fmt"
	"sort"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// DealMeasure is one order measured for the savings report.
type DealMeasure struct {
	OrderID string
	Month   string // YYYY-MM
	Status  string
	Baselines
	Source string // close | backfill | live
}

// measureDeal reads a stored snapshot when one exists and otherwise measures
// the order live from the RFQ's immutable opening quotes (no history: past
// purchases are only matched when a negotiation closes).
func measureDeal(r sqlc.BuyerDealBaselinesRow) DealMeasure {
	d := DealMeasure{OrderID: r.OrderID, Month: r.CreatedAt.UTC().Format("2006-01"), Status: r.Status}
	if r.HasSnapshot && r.VerifiedBaselineCents != nil && r.VerifiedBasis != nil {
		nb := sqlc.NegotiationBaseline{
			Currency: r.Currency, FinalCents: r.TotalCents, MarketCents: r.MarketCents,
			HistoricalCents: r.HistoricalCents, VerifiedBaselineCents: *r.VerifiedBaselineCents,
			VerifiedBasis: *r.VerifiedBasis, AnchorPct: r.AnchorPct, AnchorBasis: r.AnchorBasis,
			MarketQuoteCount: int32(len(r.RfqOpenings)),
		}
		if r.OpeningCents > 0 {
			v := r.OpeningCents
			nb.OpeningCents = &v
		}
		if r.Anchored != nil {
			nb.Anchored = *r.Anchored
		}
		if r.Reason != nil {
			nb.Reason = *r.Reason
		}
		d.Baselines = baselinesFromSnapshot(nb)
		d.Source = "close"
		if r.Source != nil {
			d.Source = *r.Source
		}
		return d
	}
	in := BaselineInput{QuoteID: "deal", FinalCents: r.TotalCents, Currency: r.Currency}
	own := r.OpeningCents > 0
	for i, p := range r.RfqOpenings {
		id := fmt.Sprintf("q%d", i)
		if own && p == r.OpeningCents {
			id, own = "deal", false
		}
		in.Openings = append(in.Openings, OpeningQuote{QuoteID: id, PriceCents: p, Currency: r.Currency})
	}
	d.Baselines = ComputeBaselines(in)
	d.Source = "live"
	return d
}

// VerifiedReport is the headline savings block of GET /v1/negotiations/savings.
type VerifiedReport struct {
	Currency            string         `json:"currency"`
	Deals               int            `json:"deals"`
	SpendCents          int64          `json:"spend_cents"`
	BaselineCents       int64          `json:"baseline_cents"`
	SavingsCents        int64          `json:"savings_cents"`
	SavingsPct          float64        `json:"savings_pct"`
	OpeningSavingsCents int64          `json:"opening_quote_savings_cents"`
	ByBasis             map[string]int `json:"by_basis"`
	AnchoredDeals       int            `json:"anchored_deals"`
	SnapshotDeals       int            `json:"snapshot_deals"`
	OtherCurrencyDeals  int            `json:"other_currency_deals"`
	Explanation         string         `json:"explanation"`
}

// primaryCurrency is the currency most of the buyer's deals are in (ties go
// to the default AED, then alphabetical). Totals never mix currencies.
func primaryCurrency(ds []DealMeasure) string {
	n := map[string]int{}
	for _, d := range ds {
		n[d.Currency]++
	}
	keys := make([]string, 0, len(n))
	for k := range n {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if n[keys[i]] != n[keys[j]] {
			return n[keys[i]] > n[keys[j]]
		}
		if (keys[i] == "AED") != (keys[j] == "AED") {
			return keys[i] == "AED"
		}
		return keys[i] < keys[j]
	})
	if len(keys) == 0 {
		return "AED"
	}
	return keys[0]
}

// buildVerifiedReport totals the non-cancelled deals in the primary currency.
func buildVerifiedReport(all []DealMeasure) VerifiedReport {
	var live []DealMeasure
	for _, d := range all {
		if d.Status != "cancelled" {
			live = append(live, d)
		}
	}
	rep := VerifiedReport{Currency: primaryCurrency(live), ByBasis: map[string]int{
		BasisMarket: 0, BasisHistorical: 0, BasisOpening: 0, BasisNone: 0,
	}}
	for _, d := range live {
		if d.Currency != rep.Currency {
			rep.OtherCurrencyDeals++
			continue
		}
		rep.Deals++
		rep.SpendCents += d.FinalCents
		rep.BaselineCents += d.VerifiedBaselineCents
		rep.SavingsCents += d.VerifiedSavingsCents
		rep.OpeningSavingsCents += d.OpeningSavingsCents
		rep.ByBasis[d.VerifiedBasis]++
		if d.Anchor.Flagged {
			rep.AnchoredDeals++
		}
		if d.Source != "live" {
			rep.SnapshotDeals++
		}
	}
	rep.SavingsPct = pctOf(rep.SavingsCents, rep.BaselineCents)
	rep.Explanation = "Verified savings compare each deal with the lowest fair reference: the typical opening price across suppliers, " +
		"what you last paid for the same item, or the supplier's own opening quote when it doesn't look inflated."
	if rep.AnchoredDeals > 0 {
		rep.Explanation += fmt.Sprintf(" %d opening quote%s looked inflated and %s not used.", rep.AnchoredDeals, plural(rep.AnchoredDeals), map[bool]string{true: "was", false: "were"}[rep.AnchoredDeals == 1])
	}
	if rep.OtherCurrencyDeals > 0 {
		rep.Explanation += fmt.Sprintf(" %d deal%s in other currencies %s not included.", rep.OtherCurrencyDeals, plural(rep.OtherCurrencyDeals), map[bool]string{true: "is", false: "are"}[rep.OtherCurrencyDeals == 1])
	}
	return rep
}

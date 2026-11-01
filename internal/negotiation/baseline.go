package negotiation

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Savings baselines.
//
// A supplier's opening quote is the easiest baseline to game: inflate the
// opening, concede, and the "savings" look large. So every deal is measured
// against up to three references, and the headline ("verified savings") uses
// the LOWEST one that applies:
//
//   - Opening quote: the winning supplier's own first (v1) price. Kept for
//     transparency, but dropped from the verified figure when it is flagged
//     as an anchor.
//   - Market: the lower median of every supplier's opening price on the same
//     RFQ, in the deal's currency, when at least MinMarketQuotes quoted. The
//     lower median (for an even count, the lower of the two middle values)
//     ignores up to half the quotes being inflated, and with two quotes it is
//     simply the lower one, so a single anchored quote cannot raise it.
//   - Historical: the buyer's most recent confirmed purchase of a comparable
//     item (same category and currency, same quantity unit, quantity within
//     HistoricalMinQtyRatio..HistoricalMaxQtyRatio, item similarity at least
//     HistoricalMinSimilarity, within HistoricalMaxAge), normalised per unit
//     and scaled to this RFQ's quantity. Used only when such a match exists.
//
// Quotes and orders in different currencies are never compared.
const (
	// MinMarketQuotes is how many same-currency opening quotes the market
	// baseline needs.
	MinMarketQuotes = 2

	// AnchorMarketThreshold flags an opening quote as a likely anchor when it
	// is more than 25% above the lower median of the OTHER suppliers' openings.
	// Honest quotes on one RFQ usually sit within a 10-20% band; 25% leaves
	// room for real differences in spec or service before calling it anchoring.
	AnchorMarketThreshold = 0.25

	// AnchorHistoricalThreshold flags an opening quote more than 35% above the
	// buyer's own comparable past price. Wider than the market threshold
	// because past prices age (inflation, freight, FX) by up to a year.
	AnchorHistoricalThreshold = 0.35

	// HistoricalMinQtyRatio and HistoricalMaxQtyRatio bound how different the
	// past quantity may be. Unit prices shift with volume, so a per-unit price
	// from an order less than half or more than twice the size is not compared.
	HistoricalMinQtyRatio = 0.5
	HistoricalMaxQtyRatio = 2.0

	// HistoricalMinSimilarity is the minimum Jaccard similarity between the
	// two items' descriptive terms (title + extracted keywords, location
	// removed) for them to count as the same item.
	HistoricalMinSimilarity = 0.5

	// HistoricalMaxAge is how far back a past purchase may be.
	HistoricalMaxAge = 365 * 24 * time.Hour
)

// Basis names the reference a savings figure is measured against.
const (
	BasisOpening    = "opening_quote"
	BasisMarket     = "market"
	BasisHistorical = "historical"
	BasisNone       = "none"
)

// OpeningQuote is one supplier's first (v1) price on an RFQ.
type OpeningQuote struct {
	QuoteID    string
	PriceCents int64
	Currency   string
}

// Item describes what was bought, as far as the RFQ records it.
type Item struct {
	Title    string
	Category string
	Keywords []string
	Quantity string
	Location string
}

// PastPurchase is one of the buyer's own prior confirmed orders.
type PastPurchase struct {
	OrderID    string
	RFQID      string
	Item       Item
	TotalCents int64
	Currency   string
	At         time.Time
}

// HistoricalMatch is the past purchase the historical baseline came from.
type HistoricalMatch struct {
	OrderID    string    `json:"order_id"`
	RFQID      string    `json:"rfq_id"`
	Title      string    `json:"rfq_title"`
	TotalCents int64     `json:"total_cents"`
	Quantity   string    `json:"quantity"`
	OrderedAt  time.Time `json:"ordered_at"`
	Similarity float64   `json:"similarity"`
}

// BaselineInput is everything needed to measure one deal.
type BaselineInput struct {
	QuoteID    string // the deal's quote
	FinalCents int64  // what the buyer pays
	Currency   string
	Openings   []OpeningQuote // every supplier's opening on the RFQ
	Item       Item
	History    []PastPurchase // buyer's prior purchases, any order
	Now        time.Time
}

// Baselines is the measured deal.
type Baselines struct {
	Currency              string           `json:"currency"`
	FinalCents            int64            `json:"final_cents"`
	OpeningCents          *int64           `json:"opening_cents"`
	MarketCents           *int64           `json:"market_cents"`
	MarketQuoteCount      int              `json:"market_quote_count"`
	HistoricalCents       *int64           `json:"historical_cents"`
	Historical            *HistoricalMatch `json:"historical"`
	Anchor                Anchor           `json:"anchor"`
	VerifiedBaselineCents int64            `json:"verified_baseline_cents"`
	VerifiedBasis         string           `json:"verified_basis"`
	VerifiedSavingsCents  int64            `json:"verified_savings_cents"`
	VerifiedSavingsPct    float64          `json:"verified_savings_pct"`
	OpeningSavingsCents   int64            `json:"opening_savings_cents"`
	Reason                string           `json:"reason"`
}

// Anchor reports whether an opening quote looks inflated.
type Anchor struct {
	Flagged bool    `json:"flagged"`
	Pct     float64 `json:"pct"`   // how far above the reference, in percent
	Basis   string  `json:"basis"` // market | historical
	Note    string  `json:"note"`
}

// LowerMedian returns the lower median of vs (for an even count, the lower of
// the two middle values), matching Postgres percentile_disc(0.5). It returns
// 0 for an empty slice.
func LowerMedian(vs []int64) int64 {
	if len(vs) == 0 {
		return 0
	}
	s := append([]int64(nil), vs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[(len(s)-1)/2]
}

// DetectAnchor flags opening as an anchor when it is far above the other
// suppliers' openings (others, same currency) or the historical reference.
// The market comparison wins when both trigger, since it is the fresher signal.
func DetectAnchor(opening int64, others []int64, historical *int64) Anchor {
	if opening <= 0 {
		return Anchor{}
	}
	if len(others) > 0 {
		ref := LowerMedian(others)
		if ref > 0 && float64(opening) > float64(ref)*(1+AnchorMarketThreshold) {
			a := Anchor{Flagged: true, Pct: pctAbove(opening, ref), Basis: BasisMarket}
			a.Note = anchorNote(a)
			return a
		}
	}
	if historical != nil && *historical > 0 && float64(opening) > float64(*historical)*(1+AnchorHistoricalThreshold) {
		a := Anchor{Flagged: true, Pct: pctAbove(opening, *historical), Basis: BasisHistorical}
		a.Note = anchorNote(a)
		return a
	}
	return Anchor{}
}

// anchorNote is the plain-language flag shown in the negotiation room and log.
func anchorNote(a Anchor) string {
	if !a.Flagged {
		return ""
	}
	if a.Basis == BasisHistorical {
		return fmt.Sprintf("Opening quote was %s above what you last paid for this item; savings measured against your past price instead.", fmtPct(a.Pct))
	}
	return fmt.Sprintf("Opening quote was %s above other suppliers; savings measured against market price instead.", fmtPct(a.Pct))
}

// ComputeBaselines measures one deal against every applicable reference.
func ComputeBaselines(in BaselineInput) Baselines {
	b := Baselines{Currency: in.Currency, FinalCents: in.FinalCents}

	var all, others []int64
	for _, o := range in.Openings {
		if o.Currency != in.Currency || o.PriceCents <= 0 {
			continue // never compare across currencies
		}
		all = append(all, o.PriceCents)
		if o.QuoteID == in.QuoteID {
			v := o.PriceCents
			b.OpeningCents = &v
		} else {
			others = append(others, o.PriceCents)
		}
	}
	b.MarketQuoteCount = len(all)
	if len(all) >= MinMarketQuotes {
		v := LowerMedian(all)
		b.MarketCents = &v
	}
	if m := MatchHistorical(in.Item, in.Currency, in.History, in.Now); m != nil {
		b.Historical = &m.HistoricalMatch
		v := m.ScaledCents
		b.HistoricalCents = &v
	}
	if b.OpeningCents != nil {
		b.Anchor = DetectAnchor(*b.OpeningCents, others, b.HistoricalCents)
		if *b.OpeningCents > in.FinalCents {
			b.OpeningSavingsCents = *b.OpeningCents - in.FinalCents
		}
	}

	// Lowest applicable reference wins. Ties prefer the harder-to-game one.
	type cand struct {
		basis string
		cents int64
	}
	var cands []cand
	if b.MarketCents != nil {
		cands = append(cands, cand{BasisMarket, *b.MarketCents})
	}
	if b.HistoricalCents != nil {
		cands = append(cands, cand{BasisHistorical, *b.HistoricalCents})
	}
	if b.OpeningCents != nil && !b.Anchor.Flagged {
		cands = append(cands, cand{BasisOpening, *b.OpeningCents})
	}
	if len(cands) == 0 {
		b.VerifiedBasis = BasisNone
		b.VerifiedBaselineCents = in.FinalCents
		b.Reason = "No opening quote or past purchase in this currency to compare against, so no savings are claimed."
		return b
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.cents < best.cents {
			best = c
		}
	}
	b.VerifiedBasis = best.basis
	b.VerifiedBaselineCents = best.cents
	if best.cents > in.FinalCents {
		b.VerifiedSavingsCents = best.cents - in.FinalCents
		b.VerifiedSavingsPct = pctOf(b.VerifiedSavingsCents, best.cents)
	}
	b.Reason = explainBasis(b)
	return b
}

func explainBasis(b Baselines) string {
	var s string
	switch b.VerifiedBasis {
	case BasisMarket:
		s = fmt.Sprintf("Measured against the typical opening price across %d suppliers (%s), the lowest fair reference for this deal.",
			b.MarketQuoteCount, money(*b.MarketCents, b.Currency))
	case BasisHistorical:
		s = fmt.Sprintf("Measured against what you last paid for a comparable item (%s, scaled to this quantity), which is lower than this RFQ's quotes.",
			money(*b.HistoricalCents, b.Currency))
	case BasisOpening:
		if b.MarketCents == nil && b.HistoricalCents == nil {
			s = "Only one supplier quoted and there is no comparable past purchase, so savings are measured against this supplier's opening quote."
		} else {
			s = "Measured against the supplier's opening quote, which was already the lowest reference."
		}
	}
	if b.VerifiedSavingsCents == 0 && b.VerifiedBasis != BasisNone {
		s += " The final price is not below that reference, so no savings are claimed."
	}
	if b.Anchor.Flagged {
		s = b.Anchor.Note + " " + s
	}
	return s
}

// historicalCandidate is a matched past purchase with its scaled price.
type historicalCandidate struct {
	HistoricalMatch
	ScaledCents int64
}

// MatchHistorical returns the most recent past purchase that is confidently
// the same item, or nil. "Confidently" means: same category (non-empty), same
// currency, both quantities parse with the same unit, quantity ratio within
// bounds, item similarity at least HistoricalMinSimilarity, and not older than
// HistoricalMaxAge.
func MatchHistorical(item Item, currency string, history []PastPurchase, now time.Time) *historicalCandidate {
	cat := normCategory(item.Category)
	if cat == "" {
		return nil
	}
	qty, unit, ok := ParseQuantity(item.Quantity)
	if !ok {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	sorted := append([]PastPurchase(nil), history...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.After(sorted[j].At) })
	for _, p := range sorted {
		if p.Currency != currency || p.TotalCents <= 0 || normCategory(p.Item.Category) != cat {
			continue
		}
		if now.Sub(p.At) > HistoricalMaxAge {
			continue
		}
		pq, pu, ok := ParseQuantity(p.Item.Quantity)
		if !ok || pu != unit {
			continue
		}
		ratio := qty / pq
		if ratio < HistoricalMinQtyRatio || ratio > HistoricalMaxQtyRatio {
			continue
		}
		sim := ItemSimilarity(item, p.Item)
		if sim < HistoricalMinSimilarity {
			continue
		}
		scaled := int64(math.Round(float64(p.TotalCents) / pq * qty))
		return &historicalCandidate{
			HistoricalMatch: HistoricalMatch{
				OrderID: p.OrderID, RFQID: p.RFQID, Title: p.Item.Title, TotalCents: p.TotalCents,
				Quantity: p.Item.Quantity, OrderedAt: p.At, Similarity: math.Round(sim*1000) / 1000,
			},
			ScaledCents: scaled,
		}
	}
	return nil
}

var reQty = regexp.MustCompile(`(?i)^\s*(\d[\d,]*(?:\.\d+)?)\s*([a-z][a-z ]*)?\s*$`)

// unitAliases maps quantity units to a canonical unit and a multiplier into
// it, so "2 tons" and "2000 kg" compare.
var unitAliases = map[string]struct {
	unit string
	mult float64
}{
	"": {"each", 1}, "unit": {"each", 1}, "units": {"each", 1}, "pc": {"each", 1}, "pcs": {"each", 1},
	"piece": {"each", 1}, "pieces": {"each", 1},
	"kg": {"kg", 1}, "kgs": {"kg", 1}, "kilogram": {"kg", 1}, "kilograms": {"kg", 1},
	"ton": {"kg", 1000}, "tons": {"kg", 1000}, "tonne": {"kg", 1000}, "tonnes": {"kg", 1000},
	"mt": {"kg", 1000}, "metric ton": {"kg", 1000}, "metric tons": {"kg", 1000},
	"box": {"box", 1}, "boxes": {"box", 1}, "carton": {"carton", 1}, "cartons": {"carton", 1},
	"pallet": {"pallet", 1}, "pallets": {"pallet", 1},
	"liter": {"l", 1}, "liters": {"l", 1}, "litre": {"l", 1}, "litres": {"l", 1}, "l": {"l", 1},
}

// ParseQuantity reads an RFQ quantity like "500 cartons" or "2 tons" into an
// amount in a canonical unit. A bare number counts as "each". Anything else
// (ranges, unknown units, prose) fails, so it is never compared.
func ParseQuantity(s string) (float64, string, bool) {
	m := reQty.FindStringSubmatch(s)
	if m == nil {
		return 0, "", false
	}
	n, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
	if err != nil || n <= 0 {
		return 0, "", false
	}
	u, ok := unitAliases[strings.Join(strings.Fields(strings.ToLower(m[2])), " ")]
	if !ok {
		return 0, "", false
	}
	return n * u.mult, u.unit, true
}

var reTerm = regexp.MustCompile(`[a-z][a-z-]{2,}`)

var termStop = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "need": {}, "want": {}, "looking": {}, "please": {},
	"quote": {}, "supplier": {}, "suppliers": {}, "our": {}, "your": {}, "from": {}, "that": {},
	"this": {}, "have": {}, "will": {}, "deliver": {}, "delivery": {}, "ship": {}, "order": {},
	"unit": {}, "units": {}, "pcs": {}, "pieces": {},
}

func itemTerms(it Item) map[string]struct{} {
	loc := map[string]struct{}{}
	for _, w := range reTerm.FindAllString(strings.ToLower(it.Location), -1) {
		loc[w] = struct{}{}
	}
	out := map[string]struct{}{}
	text := strings.ToLower(it.Title + " " + strings.Join(it.Keywords, " "))
	for _, w := range reTerm.FindAllString(text, -1) {
		if _, stop := termStop[w]; stop {
			continue
		}
		if _, isLoc := loc[w]; isLoc {
			continue
		}
		out[singular(w)] = struct{}{}
	}
	return out
}

func singular(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "xes"), strings.HasSuffix(w, "ches"), strings.HasSuffix(w, "shes"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 3:
		return w[:len(w)-1]
	}
	return w
}

// ItemSimilarity is the Jaccard similarity of two items' descriptive terms.
func ItemSimilarity(a, b Item) float64 {
	ta, tb := itemTerms(a), itemTerms(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for w := range ta {
		if _, ok := tb[w]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	return float64(inter) / float64(union)
}

func normCategory(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func pctAbove(v, ref int64) float64 {
	return math.Round((float64(v)/float64(ref)-1)*1000) / 10
}

// pctOf is part/whole in percent, to three decimals.
func pctOf(part, whole int64) float64 {
	if whole <= 0 {
		return 0
	}
	return math.Round(float64(part)/float64(whole)*100000) / 1000
}

func fmtPct(p float64) string {
	if p == math.Trunc(p) {
		return fmt.Sprintf("%.0f%%", p)
	}
	return fmt.Sprintf("%.1f%%", p)
}

// SavingsPhrase is the clause the engine appends to an outcome summary.
func SavingsPhrase(b Baselines) string {
	if b.VerifiedSavingsCents <= 0 {
		return ""
	}
	ref := map[string]string{
		BasisMarket:     "the typical market quote",
		BasisHistorical: "your last comparable purchase",
		BasisOpening:    "the opening quote",
	}[b.VerifiedBasis]
	return fmt.Sprintf(", saving %s vs %s", money(b.VerifiedSavingsCents, b.Currency), ref)
}

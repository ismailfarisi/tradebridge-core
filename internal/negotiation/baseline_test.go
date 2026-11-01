package negotiation

import (
	"strings"
	"testing"
	"time"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func cartons(qty string) Item {
	return Item{Title: "Corrugated cartons", Category: "Packaging", Keywords: []string{"cartons", "corrugated", "packaging", "dubai"}, Quantity: qty, Location: "Dubai"}
}

func openings(cur string, prices ...int64) []OpeningQuote {
	out := make([]OpeningQuote, len(prices))
	for i, p := range prices {
		out[i] = OpeningQuote{QuoteID: string(rune('a' + i)), PriceCents: p, Currency: cur}
	}
	return out
}

func TestLowerMedian(t *testing.T) {
	cases := []struct {
		in   []int64
		want int64
	}{
		{nil, 0},
		{[]int64{500}, 500},
		{[]int64{900, 100}, 100},           // even count: lower middle
		{[]int64{300, 100, 200}, 200},      // odd count: middle
		{[]int64{100, 400, 200, 300}, 200}, // unsorted, even
	}
	for _, c := range cases {
		if got := LowerMedian(c.in); got != c.want {
			t.Errorf("LowerMedian(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSingleQuoteUsesOpening(t *testing.T) {
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 90_000, Currency: "AED", Openings: openings("AED", 100_000), Now: now})
	if b.MarketCents != nil {
		t.Fatalf("market baseline needs %d quotes, got %v", MinMarketQuotes, *b.MarketCents)
	}
	if b.VerifiedBasis != BasisOpening || b.VerifiedSavingsCents != 10_000 || b.OpeningSavingsCents != 10_000 {
		t.Fatalf("got basis=%s verified=%d opening=%d", b.VerifiedBasis, b.VerifiedSavingsCents, b.OpeningSavingsCents)
	}
	if b.VerifiedSavingsPct != 10 {
		t.Fatalf("pct = %v", b.VerifiedSavingsPct)
	}
	if !strings.Contains(b.Reason, "Only one supplier quoted") {
		t.Fatalf("reason = %q", b.Reason)
	}
}

func TestInflatedOpeningDoesNotInflateVerified(t *testing.T) {
	// Supplier a opens at 2x the others, then "concedes" to 95k.
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 95_000, Currency: "AED",
		Openings: openings("AED", 200_000, 100_000, 104_000), Now: now})
	if !b.Anchor.Flagged || b.Anchor.Basis != BasisMarket {
		t.Fatalf("anchor = %+v", b.Anchor)
	}
	if b.Anchor.Pct != 100 {
		t.Fatalf("anchor pct = %v, want 100 (vs lower median 100k of others)", b.Anchor.Pct)
	}
	if b.OpeningSavingsCents != 105_000 {
		t.Fatalf("opening savings = %d", b.OpeningSavingsCents)
	}
	if b.VerifiedBasis != BasisMarket || *b.MarketCents != 104_000 || b.VerifiedSavingsCents != 9_000 {
		t.Fatalf("basis=%s market=%d verified=%d", b.VerifiedBasis, *b.MarketCents, b.VerifiedSavingsCents)
	}
	if !strings.HasPrefix(b.Reason, "Opening quote was 100% above other suppliers; savings measured against market price instead.") {
		t.Fatalf("reason = %q", b.Reason)
	}
}

func TestTwoQuotesOneInflated(t *testing.T) {
	// With two quotes the lower median is the lower quote, so one inflated
	// quote cannot move the market baseline.
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 120_000, Currency: "AED",
		Openings: openings("AED", 300_000, 110_000), Now: now})
	if !b.Anchor.Flagged || *b.MarketCents != 110_000 {
		t.Fatalf("anchor=%+v market=%v", b.Anchor, b.MarketCents)
	}
	if b.VerifiedSavingsCents != 0 {
		t.Fatalf("paid above market, verified savings must be 0, got %d", b.VerifiedSavingsCents)
	}
	if !strings.Contains(b.Reason, "no savings are claimed") {
		t.Fatalf("reason = %q", b.Reason)
	}
}

func TestOpeningBelowMarketStaysHonest(t *testing.T) {
	// The winner opened lowest: opening is the lowest reference.
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 85_000, Currency: "AED",
		Openings: openings("AED", 90_000, 100_000, 110_000), Now: now})
	if b.Anchor.Flagged || b.VerifiedBasis != BasisOpening || b.VerifiedSavingsCents != 5_000 {
		t.Fatalf("got %+v", b)
	}
}

func TestCurrencyMismatchNotCompared(t *testing.T) {
	ops := []OpeningQuote{
		{QuoteID: "a", PriceCents: 100_000, Currency: "AED"},
		{QuoteID: "b", PriceCents: 10_000, Currency: "USD"}, // would look like an anchor if compared
		{QuoteID: "c", PriceCents: 12_000, Currency: "USD"},
	}
	hist := []PastPurchase{{OrderID: "o1", Item: cartons("500 cartons"), TotalCents: 20_000, Currency: "USD", At: now.AddDate(0, -1, 0)}}
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 95_000, Currency: "AED", Openings: ops,
		Item: cartons("500 cartons"), History: hist, Now: now})
	if b.MarketCents != nil || b.HistoricalCents != nil || b.Anchor.Flagged {
		t.Fatalf("cross-currency data leaked: %+v", b)
	}
	if b.VerifiedBasis != BasisOpening || b.MarketQuoteCount != 1 {
		t.Fatalf("got %+v", b)
	}
	// Deal currency differs from its own opening: no opening reference at all.
	b = ComputeBaselines(BaselineInput{QuoteID: "b", FinalCents: 9_000, Currency: "AED", Openings: ops[1:2], Now: now})
	if b.OpeningCents != nil || b.VerifiedBasis != BasisNone || b.VerifiedSavingsCents != 0 {
		t.Fatalf("got %+v", b)
	}
}

func TestHistoricalBaseline(t *testing.T) {
	hist := []PastPurchase{
		{OrderID: "old", Item: cartons("500 cartons"), TotalCents: 80_000, Currency: "AED", At: now.AddDate(0, -6, 0)},
		{OrderID: "recent", Item: cartons("400 cartons"), TotalCents: 72_000, Currency: "AED", At: now.AddDate(0, -1, 0)},
	}
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 88_000, Currency: "AED",
		Openings: openings("AED", 100_000), Item: cartons("500 cartons"), History: hist, Now: now})
	// Most recent match: 72,000 / 400 * 500 = 90,000.
	if b.HistoricalCents == nil || *b.HistoricalCents != 90_000 || b.Historical.OrderID != "recent" {
		t.Fatalf("historical = %+v %+v", b.HistoricalCents, b.Historical)
	}
	if b.VerifiedBasis != BasisHistorical || b.VerifiedSavingsCents != 2_000 {
		t.Fatalf("basis=%s savings=%d", b.VerifiedBasis, b.VerifiedSavingsCents)
	}
}

func TestNoHistory(t *testing.T) {
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 88_000, Currency: "AED",
		Openings: openings("AED", 100_000, 98_000), Item: cartons("500 cartons"), Now: now})
	if b.HistoricalCents != nil || b.VerifiedBasis != BasisMarket {
		t.Fatalf("got %+v", b)
	}
}

func TestHistoricalRequiresConfidentMatch(t *testing.T) {
	base := PastPurchase{OrderID: "o", Item: cartons("500 cartons"), TotalCents: 50_000, Currency: "AED", At: now.AddDate(0, -1, 0)}
	cases := map[string]func(p *PastPurchase, item *Item){
		"quantity far larger":  func(p *PastPurchase, _ *Item) { p.Item.Quantity = "5000 cartons" },
		"quantity far smaller": func(p *PastPurchase, _ *Item) { p.Item.Quantity = "100 cartons" },
		"different unit":       func(p *PastPurchase, _ *Item) { p.Item.Quantity = "500 pallets" },
		"unparseable quantity": func(p *PastPurchase, _ *Item) { p.Item.Quantity = "about 500-600" },
		"current qty missing":  func(_ *PastPurchase, it *Item) { it.Quantity = "" },
		"different category":   func(p *PastPurchase, _ *Item) { p.Item.Category = "Chemicals" },
		"no category":          func(_ *PastPurchase, it *Item) { it.Category = "" },
		"different item": func(p *PastPurchase, _ *Item) {
			p.Item.Title, p.Item.Keywords = "Stretch wrap film", []string{"stretch", "wrap", "film"}
		},
		"only location in common": func(p *PastPurchase, _ *Item) {
			p.Item.Title, p.Item.Keywords = "Pallet labels", []string{"labels", "dubai"}
		},
		"too old":            func(p *PastPurchase, _ *Item) { p.At = now.Add(-HistoricalMaxAge - time.Hour) },
		"different currency": func(p *PastPurchase, _ *Item) { p.Currency = "USD" },
	}
	for name, mut := range cases {
		p, item := base, cartons("500 cartons")
		p.Item.Keywords = append([]string(nil), base.Item.Keywords...)
		mut(&p, &item)
		if m := MatchHistorical(item, "AED", []PastPurchase{p}, now); m != nil {
			t.Errorf("%s: expected no match, got %+v", name, m)
		}
	}
	if m := MatchHistorical(cartons("500 cartons"), "AED", []PastPurchase{base}, now); m == nil {
		t.Fatal("baseline case should match")
	}
}

func TestQuantityMismatchNormalisedPerUnit(t *testing.T) {
	// 2 tons vs a past 1,500 kg order: same unit family, ratio 1.33.
	item := Item{Title: "Cement bags", Category: "construction", Keywords: []string{"cement"}, Quantity: "2 tons"}
	past := PastPurchase{OrderID: "o", Item: Item{Title: "Cement", Category: "Construction", Keywords: []string{"cement", "bags"}, Quantity: "1,500 kg"},
		TotalCents: 150_000, Currency: "AED", At: now.AddDate(0, -2, 0)}
	m := MatchHistorical(item, "AED", []PastPurchase{past}, now)
	if m == nil || m.ScaledCents != 200_000 {
		t.Fatalf("got %+v", m)
	}
}

func TestAllQuotesAnchoredAgainstHistory(t *testing.T) {
	// Every supplier inflates equally: the market check cannot see it, but the
	// buyer's own past price can.
	hist := []PastPurchase{{OrderID: "o", Item: cartons("500 cartons"), TotalCents: 50_000, Currency: "AED", At: now.AddDate(0, -2, 0)}}
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 70_000, Currency: "AED",
		Openings: openings("AED", 100_000, 101_000, 102_000), Item: cartons("500 cartons"), History: hist, Now: now})
	if !b.Anchor.Flagged || b.Anchor.Basis != BasisHistorical || b.Anchor.Pct != 100 {
		t.Fatalf("anchor = %+v", b.Anchor)
	}
	if b.VerifiedBasis != BasisHistorical || b.VerifiedSavingsCents != 0 || b.OpeningSavingsCents != 30_000 {
		t.Fatalf("basis=%s verified=%d opening=%d", b.VerifiedBasis, b.VerifiedSavingsCents, b.OpeningSavingsCents)
	}
	if !strings.Contains(b.Reason, "above what you last paid") {
		t.Fatalf("reason = %q", b.Reason)
	}
}

func TestDetectAnchorThresholds(t *testing.T) {
	if a := DetectAnchor(125_000, []int64{100_000}, nil); a.Flagged {
		t.Fatal("exactly at the market threshold is not an anchor")
	}
	if a := DetectAnchor(125_100, []int64{100_000}, nil); !a.Flagged || a.Pct != 25.1 {
		t.Fatalf("just above threshold: %+v", a)
	}
	h := int64(100_000)
	if a := DetectAnchor(135_000, nil, &h); a.Flagged {
		t.Fatal("exactly at the historical threshold is not an anchor")
	}
	if a := DetectAnchor(140_000, nil, &h); !a.Flagged || a.Basis != BasisHistorical {
		t.Fatalf("above historical threshold: %+v", a)
	}
	if a := DetectAnchor(500_000, nil, nil); a.Flagged {
		t.Fatal("no reference, no flag")
	}
}

func TestParseQuantity(t *testing.T) {
	cases := []struct {
		in   string
		n    float64
		unit string
		ok   bool
	}{
		{"500 cartons", 500, "carton", true},
		{"1,200 pcs", 1200, "each", true},
		{"2 metric tons", 2000, "kg", true},
		{"750", 750, "each", true},
		{"10 Litres", 10, "l", true},
		{"500-600 boxes", 0, "", false},
		{"lots", 0, "", false},
		{"5 widgets", 0, "", false},
		{"0 units", 0, "", false},
	}
	for _, c := range cases {
		n, u, ok := ParseQuantity(c.in)
		if ok != c.ok || n != c.n || u != c.unit {
			t.Errorf("ParseQuantity(%q) = %v %q %v", c.in, n, u, ok)
		}
	}
}

func TestSavingsPhrase(t *testing.T) {
	b := ComputeBaselines(BaselineInput{QuoteID: "a", FinalCents: 95_000, Currency: "AED",
		Openings: openings("AED", 200_000, 100_000, 104_000), Now: now})
	if got := SavingsPhrase(b); got != ", saving AED 90 vs the typical market quote" {
		t.Fatalf("phrase = %q", got)
	}
	if SavingsPhrase(Baselines{}) != "" {
		t.Fatal("no savings, no phrase")
	}
}

func TestMeasureDealLiveAndSnapshot(t *testing.T) {
	live := measureDeal(sqlc.BuyerDealBaselinesRow{OrderID: "o", CreatedAt: now, TotalCents: 95_000, Currency: "AED",
		Status: "created", OpeningCents: 200_000, RfqOpenings: []int64{100_000, 104_000, 200_000}})
	if live.Source != "live" || live.VerifiedSavingsCents != 9_000 || !live.Anchor.Flagged || live.OpeningSavingsCents != 105_000 {
		t.Fatalf("live = %+v", live)
	}
	basis, base, src, anchored := BasisHistorical, int64(90_000), "close", false
	snap := measureDeal(sqlc.BuyerDealBaselinesRow{OrderID: "o2", CreatedAt: now, TotalCents: 88_000, Currency: "AED",
		Status: "confirmed", OpeningCents: 100_000, RfqOpenings: []int64{100_000}, HasSnapshot: true,
		VerifiedBasis: &basis, VerifiedBaselineCents: &base, Source: &src, Anchored: &anchored})
	if snap.Source != "close" || snap.VerifiedSavingsCents != 2_000 || snap.VerifiedBasis != BasisHistorical {
		t.Fatalf("snap = %+v", snap)
	}
	rep := buildVerifiedReport([]DealMeasure{live, snap,
		{OrderID: "usd", Status: "created", Baselines: Baselines{Currency: "USD", FinalCents: 1, VerifiedBasis: BasisNone}},
		{OrderID: "x", Status: "cancelled", Baselines: Baselines{Currency: "AED", FinalCents: 5, VerifiedSavingsCents: 999}}})
	if rep.Currency != "AED" || rep.Deals != 2 || rep.SavingsCents != 11_000 || rep.OpeningSavingsCents != 117_000 ||
		rep.AnchoredDeals != 1 || rep.SnapshotDeals != 1 || rep.OtherCurrencyDeals != 1 || rep.ByBasis[BasisMarket] != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestItemOfReadsRequirements(t *testing.T) {
	it := itemOf("Cartons", "", []byte(`{"category":"packaging","quantity":"500 cartons","keywords":["cartons"],"location":"Dubai"}`))
	if it.Category != "packaging" || it.Quantity != "500 cartons" || it.Location != "Dubai" {
		t.Fatalf("got %+v", it)
	}
	if it := itemOf("x", "c", []byte(`{"quantity":250}`)); it.Quantity != "250" {
		t.Fatalf("numeric quantity: %+v", it)
	}
}

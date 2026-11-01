package negotiation

import (
	"math"
	"math/rand"
	"strings"
	"testing"
)

func i32(v int32) *int32 { return &v }

func baseMandate() Mandate {
	return Mandate{TargetCents: 90_000, MaxCents: 100_000, MaxDeliveryDays: i32(14), PriceWeight: 0.7, MaxRounds: 4, Tone: "collaborative"}
}

func TestDecide_AcceptsAtOrBelowTarget(t *testing.T) {
	d := Decide(baseMandate(), Offer{PriceCents: 89_000, DeliveryDays: i32(10), Currency: "USD"}, History{})
	if d.Action != ActionAcceptReady {
		t.Fatalf("want accept_ready, got %s", d.Action)
	}
}

func TestDecide_FirstCounterAnchorsAtTarget(t *testing.T) {
	d := Decide(baseMandate(), Offer{PriceCents: 120_000, DeliveryDays: i32(10), Currency: "USD"}, History{})
	if d.Action != ActionCounter || d.OfferCents != 90_000 {
		t.Fatalf("want counter at target, got %s %d", d.Action, d.OfferCents)
	}
	if d.OfferDeliveryDays == nil || *d.OfferDeliveryDays != 10 {
		t.Fatalf("delivery within limit should be kept, got %v", d.OfferDeliveryDays)
	}
	if !strings.Contains(d.Message, "USD 900") {
		t.Fatalf("message should state the offer: %q", d.Message)
	}
}

func TestDecide_ConcessionIsMonotonicAndBounded(t *testing.T) {
	for _, tone := range []string{"collaborative", "firm"} {
		m := baseMandate()
		m.Tone = tone
		prev := int64(0)
		for n := 0; n < m.MaxRounds; n++ {
			d := Decide(m, Offer{PriceCents: 150_000, DeliveryDays: i32(10)}, History{Counters: n, LastOfferCents: prev, LastSellerCents: 150_000 + int64(m.MaxRounds-n)})
			if n > 0 && d.Action != ActionCounter {
				// LastSellerCents decreasing keeps the seller "moving".
				t.Fatalf("%s round %d: want counter, got %s", tone, n+1, d.Action)
			}
			if d.OfferCents < prev || d.OfferCents > m.MaxCents {
				t.Fatalf("%s round %d: offer %d not in [%d,%d]", tone, n+1, d.OfferCents, prev, m.MaxCents)
			}
			prev = d.OfferCents
		}
		if prev != m.MaxCents {
			t.Fatalf("%s: final counter should reach walk-away, got %d", tone, prev)
		}
	}
}

func TestDecide_FirmConcedesSlowerThanCollaborative(t *testing.T) {
	m := baseMandate()
	h := History{Counters: 1, LastOfferCents: 90_000, LastSellerCents: 160_000}
	collab := Decide(m, Offer{PriceCents: 150_000}, h)
	m.Tone = "firm"
	firm := Decide(m, Offer{PriceCents: 150_000}, h)
	if firm.OfferCents >= collab.OfferCents {
		t.Fatalf("firm %d should be below collaborative %d", firm.OfferCents, collab.OfferCents)
	}
}

func TestDecide_RoundBudgetExhausted(t *testing.T) {
	m := baseMandate()
	within := Decide(m, Offer{PriceCents: 98_000, DeliveryDays: i32(10)}, History{Counters: 4, LastSellerCents: 99_000})
	if within.Action != ActionAcceptReady {
		t.Fatalf("within walk-away should be accept_ready, got %s", within.Action)
	}
	above := Decide(m, Offer{PriceCents: 105_000, DeliveryDays: i32(10)}, History{Counters: 4, LastSellerCents: 106_000})
	if above.Action != ActionWalkAway {
		t.Fatalf("above walk-away should walk away, got %s", above.Action)
	}
}

func TestDecide_SellerHoldingPriceIsFinal(t *testing.T) {
	d := Decide(baseMandate(), Offer{PriceCents: 110_000}, History{Counters: 1, LastOfferCents: 90_000, LastSellerCents: 110_000})
	if d.Action != ActionWalkAway {
		t.Fatalf("held price above walk-away should walk away, got %s", d.Action)
	}
}

func TestDecide_UsesRivalAsCeiling(t *testing.T) {
	h := History{Counters: 3, LastOfferCents: 96_000, LastSellerCents: 130_000, Competitors: 1, BestRivalCents: 93_000}
	d := Decide(baseMandate(), Offer{PriceCents: 120_000, DeliveryDays: i32(10)}, h)
	if d.Action != ActionCounter {
		t.Fatalf("want counter, got %s", d.Action)
	}
	if d.OfferCents != 96_000 {
		// never moves backwards below our own last offer
		t.Fatalf("want 96000 (last offer floor), got %d", d.OfferCents)
	}
	h.LastOfferCents = 0
	h.Counters = 3
	d = Decide(baseMandate(), Offer{PriceCents: 120_000, DeliveryDays: i32(10)}, h)
	if d.OfferCents != 93_000 {
		t.Fatalf("rival should cap offer at 93000, got %d", d.OfferCents)
	}
	if !strings.Contains(d.Message, "1 other competitive offer") {
		t.Fatalf("message should mention competition: %q", d.Message)
	}
}

func TestDecide_PushesDeliveryIntoLimit(t *testing.T) {
	d := Decide(baseMandate(), Offer{PriceCents: 95_000, DeliveryDays: i32(30)}, History{})
	if d.Action != ActionCounter {
		t.Fatalf("slow delivery under target price still needs a counter, got %s", d.Action)
	}
	if d.OfferDeliveryDays == nil || *d.OfferDeliveryDays != 14 {
		t.Fatalf("want delivery 14, got %v", d.OfferDeliveryDays)
	}
}

func TestDecide_AcceptsWhenNextStepMeetsAsk(t *testing.T) {
	h := History{Counters: 2, LastOfferCents: 95_000, LastSellerCents: 99_000}
	d := Decide(baseMandate(), Offer{PriceCents: 96_000, DeliveryDays: i32(10)}, h)
	if d.Action != ActionAcceptReady {
		t.Fatalf("want accept_ready when our next step ≥ ask, got %s (%d)", d.Action, d.OfferCents)
	}
}

func TestUtilityAndPickBest(t *testing.T) {
	m := baseMandate()
	if u := Utility(m, Offer{PriceCents: 90_000, DeliveryDays: i32(0)}); u != 1 {
		t.Fatalf("perfect offer utility = %v", u)
	}
	if u := Utility(m, Offer{PriceCents: 100_000, DeliveryDays: i32(20)}); u != 0 {
		t.Fatalf("walk-away price + late delivery utility = %v", u)
	}
	best, _, ok := PickBest(m, []Candidate{
		{QuoteID: "over", Offer: Offer{PriceCents: 101_000, DeliveryDays: i32(1)}},
		{QuoteID: "cheap-slow", Offer: Offer{PriceCents: 91_000, DeliveryDays: i32(14)}},
		{QuoteID: "fair-fast", Offer: Offer{PriceCents: 92_000, DeliveryDays: i32(2)}},
	})
	if !ok || best.QuoteID != "fair-fast" {
		t.Fatalf("want fair-fast, got %v %v", best.QuoteID, ok)
	}
	if _, _, ok := PickBest(m, []Candidate{{QuoteID: "x", Offer: Offer{PriceCents: 200_000}}}); ok {
		t.Fatal("nothing within walk-away should return ok=false")
	}
}

func TestMoneyFormat(t *testing.T) {
	if got := money(123456789, "AED"); got != "AED 1,234,567.89" {
		t.Fatal(got)
	}
}

func TestRoundDown(t *testing.T) {
	cases := map[int64]int64{
		1_003_288: 1_000_000, // AED 10,032.88 -> AED 10,000
		417_845:   417_000,   // AED 4,178.45 -> AED 4,170
		98_765:    98_700,    // AED 987.65 -> AED 987
		50:        50,
	}
	for in, want := range cases {
		if got := roundDown(in); got != want {
			t.Fatalf("roundDown(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestDecide_LaterCountersDropTheIntro(t *testing.T) {
	d := Decide(baseMandate(), Offer{PriceCents: 120_000, DeliveryDays: i32(10)}, History{Counters: 1, LastSellerCents: 125_000, Competitors: 2})
	if !strings.HasPrefix(d.Message, "Thanks for the revised quote. We still have 2 other competitive offers.") {
		t.Fatalf("later counter message: %q", d.Message)
	}
	if strings.Contains(d.Rationale, "concession") {
		t.Fatalf("rationale should be plain language: %q", d.Rationale)
	}
}

// ---------------------------------------------------------------------------
// Mandate invariants (property tests). The engine spends real money, so the
// safety properties below are checked over thousands of random mandates and
// seller behaviours, plus a Go fuzz target over the raw inputs:
//
//   P1 a counter never offers above the walk-away price (MaxCents), nor above
//      the seller's own ask;
//   P2 counters never retreat: each counter is ≥ the previous one, except
//      that it may match a seller ask that has dropped below our last offer;
//   P3 at most MaxRounds counters per quote;
//   P4 accept_ready (the engine's "this is a deal" signal) only for offers
//      within the walk-away price and the delivery limit;
//   P5 PickBest — the auto-accept / approval choice — never selects an offer
//      above the walk-away price or past MaxDeliveryDays, and picks the
//      highest-utility eligible offer when one exists.
// ---------------------------------------------------------------------------

func randMandate(r *rand.Rand) Mandate {
	target := int64(1_000 + r.Intn(10_000_000))
	gap := int64(0)
	if r.Intn(5) > 0 {
		gap = int64(r.Intn(int(target)))
	}
	m := Mandate{
		TargetCents: target, MaxCents: target + gap,
		PriceWeight: r.Float64(), MaxRounds: 1 + r.Intn(10), Tone: []string{"collaborative", "firm"}[r.Intn(2)],
	}
	if r.Intn(3) > 0 {
		m.MaxDeliveryDays = i32(int32(1 + r.Intn(60)))
	}
	return m
}

func randDelivery(r *rand.Rand) *int32 {
	if r.Intn(6) == 0 {
		return nil
	}
	return i32(int32(r.Intn(90)))
}

func withinDelivery(m Mandate, d *int32) bool {
	return m.MaxDeliveryDays == nil || (d != nil && *d <= *m.MaxDeliveryDays)
}

// checkDecision asserts the per-move invariants P1, P2 and P4.
func checkDecision(t *testing.T, m Mandate, o Offer, h History, d Decision) {
	t.Helper()
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("mandate=%+v offer=%+v history=%+v decision=%s/%d: "+format,
			append([]any{m, o, h, d.Action, d.OfferCents}, args...)...)
	}
	if d.Utility < 0 || d.Utility > 1 {
		fail("utility %v outside [0,1]", d.Utility)
	}
	switch d.Action {
	case ActionCounter:
		if h.Counters >= m.MaxRounds {
			fail("countered with the round budget exhausted (P3)")
		}
		if d.OfferCents > m.MaxCents {
			fail("offer above walk-away %d (P1)", m.MaxCents)
		}
		if d.OfferCents > o.PriceCents {
			fail("offer above the seller's ask (P1)")
		}
		if d.OfferCents <= 0 {
			fail("non-positive offer")
		}
		if h.LastOfferCents > 0 && d.OfferCents < h.LastOfferCents && d.OfferCents != o.PriceCents {
			fail("counter retreated below our last offer %d (P2)", h.LastOfferCents)
		}
		if d.OfferDeliveryDays != nil && !withinDelivery(m, d.OfferDeliveryDays) {
			fail("counter proposes delivery past the limit")
		}
	case ActionAcceptReady:
		if o.PriceCents > m.MaxCents {
			fail("accept_ready above walk-away (P4)")
		}
		if !withinDelivery(m, o.DeliveryDays) {
			fail("accept_ready past the delivery limit (P4)")
		}
	case ActionWalkAway:
	default:
		fail("unknown action")
	}
}

// simulate plays one quote's negotiation the way Engine.tick does: it feeds
// each seller version to Decide, updating History exactly like the engine's
// round replay, until the engine stops countering or the seller gives up.
func simulate(t *testing.T, r *rand.Rand, m Mandate) {
	t.Helper()
	o := Offer{PriceCents: m.TargetCents/2 + int64(r.Intn(int(m.MaxCents*2))), DeliveryDays: randDelivery(r), Currency: "AED"}
	var h History
	if r.Intn(2) == 0 {
		h.Competitors = 1 + r.Intn(4)
		h.BestRivalCents = m.TargetCents/2 + int64(r.Intn(int(m.MaxCents*2)))
	}
	counters := 0
	for step := 0; step < 40; step++ {
		d := Decide(m, o, h)
		checkDecision(t, m, o, h, d)
		if d.Action != ActionCounter {
			return
		}
		counters++
		if counters > m.MaxRounds {
			t.Fatalf("mandate=%+v: %d counters exceed MaxRounds (P3)", m, counters)
		}
		h.Counters, h.LastOfferCents, h.LastSellerCents = counters, d.OfferCents, o.PriceCents
		// Seller reply: concede toward our offer, hold, or (rarely) raise,
		// and sometimes change its delivery.
		switch k := r.Intn(10); {
		case k < 6:
			gap := o.PriceCents - d.OfferCents
			if gap > 0 {
				o.PriceCents -= 1 + int64(r.Intn(int(gap)+1))
			} else {
				o.PriceCents -= int64(r.Intn(1000))
			}
		case k < 8:
			// hold
		default:
			o.PriceCents += int64(r.Intn(int(m.MaxCents/10) + 1))
		}
		if o.PriceCents < 1 {
			o.PriceCents = 1
		}
		if r.Intn(3) == 0 {
			o.DeliveryDays = randDelivery(r)
		}
		if h.Competitors > 0 && r.Intn(3) == 0 {
			h.BestRivalCents = m.TargetCents/2 + int64(r.Intn(int(m.MaxCents*2)))
		}
	}
}

func TestProperty_MandateInvariants(t *testing.T) {
	r := rand.New(rand.NewSource(20261101))
	for i := 0; i < 20_000; i++ {
		simulate(t, r, randMandate(r))
	}
}

func TestProperty_PickBestRespectsMandate(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 20_000; i++ {
		m := randMandate(r)
		n := r.Intn(6)
		cs := make([]Candidate, n)
		for j := range cs {
			cs[j] = Candidate{QuoteID: string(rune('a' + j)), Offer: Offer{
				PriceCents: m.TargetCents/2 + int64(r.Intn(int(m.MaxCents*2))), DeliveryDays: randDelivery(r),
			}}
		}
		checkPickBest(t, m, cs)
	}
}

func checkPickBest(t *testing.T, m Mandate, cs []Candidate) {
	t.Helper()
	best, u, ok := PickBest(m, cs)
	eligible := 0
	bestU := -1.0
	for _, c := range cs {
		if c.Offer.PriceCents <= m.MaxCents && withinDelivery(m, c.Offer.DeliveryDays) {
			eligible++
			bestU = math.Max(bestU, Utility(m, c.Offer))
		}
	}
	if !ok {
		if eligible > 0 {
			t.Fatalf("mandate=%+v: PickBest found nothing among %d eligible offers %+v", m, eligible, cs)
		}
		return
	}
	if best.Offer.PriceCents > m.MaxCents {
		t.Fatalf("mandate=%+v: PickBest chose %+v above walk-away (P5)", m, best)
	}
	if !withinDelivery(m, best.Offer.DeliveryDays) {
		t.Fatalf("mandate=%+v: PickBest chose %+v past the delivery limit (P5)", m, best)
	}
	if u != bestU {
		t.Fatalf("mandate=%+v: PickBest utility %v, best eligible %v", m, u, bestU)
	}
}

// FuzzDecide drives Decide and PickBest with arbitrary mandates, offers and
// histories (go test -fuzz=FuzzDecide ./internal/negotiation). The seed
// corpus runs on every plain `go test`.
func FuzzDecide(f *testing.F) {
	f.Add(int64(90_000), int64(10_000), int32(14), 0.7, 4, false, int64(120_000), int32(10), 0, int64(0), int64(0), int64(0))
	f.Add(int64(90_000), int64(10_000), int32(14), 0.7, 4, true, int64(95_000), int32(30), 2, int64(96_000), int64(97_000), int64(0))
	f.Add(int64(50_000), int64(0), int32(-1), 1.0, 1, true, int64(50_001), int32(-1), 0, int64(0), int64(0), int64(40_000))
	f.Add(int64(1_000), int64(999), int32(5), 0.0, 10, false, int64(1_500), int32(6), 9, int64(1_998), int64(1_600), int64(1_200))
	f.Fuzz(func(t *testing.T, target, gap int64, maxDelivery int32, weight float64, rounds int, firm bool,
		price int64, delivery int32, counters int, lastOffer, lastSeller, rival int64) {
		// Constrain to what the API accepts (see strategyInput.validate).
		if target <= 0 || target > 1e12 || gap < 0 || gap > 1e12 || price <= 0 || price > 1e13 {
			t.Skip()
		}
		if math.IsNaN(weight) || weight < 0 || weight > 1 || rounds < 1 || rounds > 10 {
			t.Skip()
		}
		m := Mandate{TargetCents: target, MaxCents: target + gap, PriceWeight: weight, MaxRounds: rounds, Tone: "collaborative"}
		if firm {
			m.Tone = "firm"
		}
		if maxDelivery > 0 {
			m.MaxDeliveryDays = &maxDelivery
		}
		o := Offer{PriceCents: price}
		if delivery >= 0 {
			o.DeliveryDays = &delivery
		}
		// A reachable history: counters within budget, our last offer a past
		// counter (so itself within the walk-away price).
		if counters < 0 || counters > rounds || lastOffer < 0 || lastOffer > m.MaxCents || lastSeller < 0 || rival < 0 {
			t.Skip()
		}
		if (counters == 0) != (lastOffer == 0) {
			t.Skip()
		}
		h := History{Counters: counters, LastOfferCents: lastOffer, LastSellerCents: lastSeller, BestRivalCents: rival}
		if rival > 0 {
			h.Competitors = 1
		}
		checkDecision(t, m, o, h, Decide(m, o, h))
		checkPickBest(t, m, []Candidate{{QuoteID: "a", Offer: o}, {QuoteID: "b", Offer: Offer{PriceCents: rival + 1, DeliveryDays: m.MaxDeliveryDays}}})
	})
}

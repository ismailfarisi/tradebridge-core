package negotiation

import (
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

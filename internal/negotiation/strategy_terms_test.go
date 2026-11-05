package negotiation

import (
	"math/rand"
	"strings"
	"testing"
)

// termsMandate negotiates payment terms: the buyer prefers net 60, accepts no
// less than net 15, and weighs price 70%, delivery 20%, terms 10%.
func termsMandate() Mandate {
	m := baseMandate()
	m.PreferredTermsDays = i32(60)
	m.MinTermsDays = i32(15)
	m.TermsWeight = 0.1
	return m
}

func TestUtility_WithoutTermsIsUnchangedTwoIssue(t *testing.T) {
	m := baseMandate()
	m.TermsWeight = 0.4 // ignored: no preferred terms
	for _, o := range []Offer{
		{PriceCents: 95_000, DeliveryDays: i32(7)},
		{PriceCents: 99_000, DeliveryDays: nil, PaymentTermsDays: i32(0)},
		{PriceCents: 91_000, DeliveryDays: i32(14), PaymentTermsDays: i32(90)},
	} {
		want := round3(0.7*priceScore(m, o.PriceCents) + 0.3*deliveryScore(m, o.DeliveryDays))
		if got := Utility(m, o); got != want {
			t.Fatalf("utility %v, want two-issue %v for %+v", got, want, o)
		}
	}
}

func TestDecide_WithoutTermsMandateNeverTouchesTerms(t *testing.T) {
	m := baseMandate()
	cases := []struct {
		o Offer
		h History
	}{
		{Offer{PriceCents: 120_000, DeliveryDays: i32(10), PaymentTermsDays: i32(0)}, History{}},
		{Offer{PriceCents: 110_000, DeliveryDays: i32(7), PaymentTermsDays: i32(30)}, History{Counters: 1, LastOfferCents: 90_000, LastSellerCents: 110_000}},
		{Offer{PriceCents: 108_000, DeliveryDays: i32(7)}, History{Counters: 1, LastOfferCents: 90_000, LastSellerCents: 109_000}},
	}
	for _, c := range cases {
		d := Decide(m, c.o, c.h)
		if d.OfferTermsDays != nil || d.TradeOff != nil {
			t.Fatalf("no terms mandate must not counter on terms: %+v", d)
		}
		if strings.Contains(d.Message, "payment within") || strings.Contains(d.Rationale, "Trade-off") {
			t.Fatalf("no terms mandate must keep the old wording: %q / %q", d.Message, d.Rationale)
		}
	}
}

func TestUtility_ThreeIssueRewardsLongerTerms(t *testing.T) {
	m := termsMandate()
	cash := Utility(m, Offer{PriceCents: 95_000, DeliveryDays: i32(7), PaymentTermsDays: i32(0)})
	net30 := Utility(m, Offer{PriceCents: 95_000, DeliveryDays: i32(7), PaymentTermsDays: i32(30)})
	net60 := Utility(m, Offer{PriceCents: 95_000, DeliveryDays: i32(7), PaymentTermsDays: i32(60)})
	net90 := Utility(m, Offer{PriceCents: 95_000, DeliveryDays: i32(7), PaymentTermsDays: i32(90)})
	if !(cash < net30 && net30 < net60 && net60 == net90) {
		t.Fatalf("terms should raise utility up to the preference: cash %v net30 %v net60 %v net90 %v", cash, net30, net60, net90)
	}
	if u := Utility(m, Offer{PriceCents: 90_000, DeliveryDays: i32(0), PaymentTermsDays: i32(60)}); u != 1 {
		t.Fatalf("perfect three-issue offer = %v", u)
	}
}

func TestDecide_FirstCounterAsksForPreferredTerms(t *testing.T) {
	d := Decide(termsMandate(), Offer{PriceCents: 120_000, DeliveryDays: i32(10), PaymentTermsDays: i32(0), Currency: "AED"}, History{})
	if d.Action != ActionCounter || d.OfferTermsDays == nil || *d.OfferTermsDays != 60 {
		t.Fatalf("want counter asking net 60, got %s %v", d.Action, d.OfferTermsDays)
	}
	if d.TradeOff != nil {
		t.Fatal("no trade-off on the first counter")
	}
	if !strings.Contains(d.Message, "with delivery within 10 days and payment within 60 days") {
		t.Fatalf("message should state the terms: %q", d.Message)
	}
	// A supplier already offering longer terms keeps them.
	d = Decide(termsMandate(), Offer{PriceCents: 120_000, DeliveryDays: i32(10), PaymentTermsDays: i32(90)}, History{})
	if *d.OfferTermsDays != 90 {
		t.Fatalf("keep the supplier's better terms, got %d", *d.OfferTermsDays)
	}
}

// When the supplier holds its price, the two-issue engine gives up (walks
// away above the walk-away price). With terms in the mandate it logrolls:
// it offers faster payment, which this buyer weighs less, for a lower price
// than a straight concession.
func TestDecide_LogrollsTermsWhenSupplierHoldsPrice(t *testing.T) {
	o := Offer{PriceCents: 110_000, DeliveryDays: i32(14), PaymentTermsDays: i32(0), Currency: "AED"}
	h := History{Counters: 1, LastOfferCents: 90_000, LastSellerCents: 110_000, LastOfferTermsDays: i32(60), LastOfferDeliveryDays: i32(14)}

	if d := Decide(baseMandate(), o, h); d.Action != ActionWalkAway {
		t.Fatalf("sanity: two-issue engine walks away, got %s", d.Action)
	}
	m := termsMandate()
	d := Decide(m, o, h)
	if d.Action != ActionCounter || d.TradeOff == nil {
		t.Fatalf("want logrolled counter, got %s %+v", d.Action, d.TradeOff)
	}
	if d.TradeOff.Issue != IssueTerms || d.TradeOff.From != 60 || d.TradeOff.To != 30 {
		t.Fatalf("want terms 60 -> 30, got %+v", d.TradeOff)
	}
	if d.OfferTermsDays == nil || *d.OfferTermsDays != 30 {
		t.Fatalf("counter terms = %v", d.OfferTermsDays)
	}
	straight := m.TargetCents + int64(float64(m.MaxCents-m.TargetCents)*concession(2, m.MaxRounds, m.Tone)+0.5)
	if d.OfferCents >= straight || d.OfferCents < h.LastOfferCents || d.OfferCents > m.MaxCents {
		t.Fatalf("logrolled price %d should be in [%d, %d)", d.OfferCents, h.LastOfferCents, straight)
	}
	if d.TradeOff.PriceCreditCents != straight-d.OfferCents {
		t.Fatalf("price credit %d, want %d", d.TradeOff.PriceCreditCents, straight-d.OfferCents)
	}
	for _, want := range []string{"They held at AED 1,100", "payment terms matter less to you (10%) than price (70%)", "net 30 days instead of net 60 days"} {
		if !strings.Contains(d.Rationale, want) {
			t.Fatalf("rationale missing %q: %q", want, d.Rationale)
		}
	}
	if !strings.Contains(d.Message, "payment within 30 days") || !strings.Contains(d.Message, "net 30 days instead of net 60 days") {
		t.Fatalf("message should explain the trade: %q", d.Message)
	}
	// Utility of our logrolled package matches the straight one (iso-utility)
	// up to whole-unit rounding.
	ours := Utility(m, Offer{PriceCents: d.OfferCents, DeliveryDays: d.OfferDeliveryDays, PaymentTermsDays: d.OfferTermsDays})
	plain := Utility(m, Offer{PriceCents: straight, DeliveryDays: d.OfferDeliveryDays, PaymentTermsDays: i32(60)})
	if diff := ours - plain; diff < -0.01 || diff > 0.01 {
		t.Fatalf("logroll should be utility-neutral for the buyer: %v vs %v", ours, plain)
	}
}

func TestDecide_LogrollsDeliveryWhenItMattersLess(t *testing.T) {
	m := termsMandate()
	m.TermsWeight = 0.25 // delivery now 5%: the cheaper issue to give
	o := Offer{PriceCents: 109_500, DeliveryDays: i32(7), PaymentTermsDays: i32(30)}
	h := History{Counters: 1, LastOfferCents: 90_000, LastSellerCents: 110_000, LastOfferTermsDays: i32(60), LastOfferDeliveryDays: i32(7)}
	d := Decide(m, o, h)
	if d.Action != ActionCounter || d.TradeOff == nil || d.TradeOff.Issue != IssueDelivery {
		t.Fatalf("want delivery logroll (seller barely moved), got %s %+v", d.Action, d.TradeOff)
	}
	if d.OfferDeliveryDays == nil || *d.OfferDeliveryDays != 11 || *d.OfferDeliveryDays > *m.MaxDeliveryDays {
		t.Fatalf("want delivery 11 within the 14-day limit, got %v", d.OfferDeliveryDays)
	}
	if !strings.Contains(d.Rationale, "a longer delivery (11 days instead of 7, within your 14-day limit)") {
		t.Fatalf("rationale: %q", d.Rationale)
	}
}

func TestDecide_NoLogrollWhileSupplierIsConceding(t *testing.T) {
	o := Offer{PriceCents: 100_000, DeliveryDays: i32(10), PaymentTermsDays: i32(0)}
	h := History{Counters: 1, LastOfferCents: 90_000, LastSellerCents: 120_000, LastOfferTermsDays: i32(60)}
	d := Decide(termsMandate(), o, h)
	if d.TradeOff != nil {
		t.Fatalf("supplier moved 2/3 of the gap; no trade expected, got %+v", d.TradeOff)
	}
	if *d.OfferTermsDays != 60 {
		t.Fatalf("keep asking net 60, got %d", *d.OfferTermsDays)
	}
}

func TestDecide_TermsConcessionIsNeverTakenBack(t *testing.T) {
	o := Offer{PriceCents: 100_000, DeliveryDays: i32(10), PaymentTermsDays: i32(0)}
	h := History{Counters: 2, LastOfferCents: 93_000, LastSellerCents: 120_000, LastOfferTermsDays: i32(30)}
	d := Decide(termsMandate(), o, h)
	if d.OfferTermsDays == nil || *d.OfferTermsDays != 30 {
		t.Fatalf("after conceding net 30 we must not ask net 60 again, got %v", d.OfferTermsDays)
	}
}

func TestDecide_NoLogrollRoomEndsAsBefore(t *testing.T) {
	m := termsMandate()
	m.MaxDeliveryDays = i32(7)
	// Terms already at the minimum and delivery at the limit: nothing to trade.
	o := Offer{PriceCents: 98_000, DeliveryDays: i32(7), PaymentTermsDays: i32(15)}
	h := History{Counters: 2, LastOfferCents: 93_000, LastSellerCents: 98_000, LastOfferTermsDays: i32(15)}
	d := Decide(m, o, h)
	if d.Action != ActionAcceptReady {
		t.Fatalf("held within walk-away and limits: want accept_ready, got %s (%s)", d.Action, d.Rationale)
	}
}

func TestDecide_NeverAcceptsTermsBelowMinimum(t *testing.T) {
	m := termsMandate()
	if d := Decide(m, Offer{PriceCents: 85_000, DeliveryDays: i32(5), PaymentTermsDays: i32(0)}, History{}); d.Action != ActionCounter {
		t.Fatalf("below-target price on cash terms must be countered, got %s", d.Action)
	}
	d := Decide(m, Offer{PriceCents: 95_000, DeliveryDays: i32(5), PaymentTermsDays: i32(7)}, History{Counters: 4, LastSellerCents: 95_000})
	if d.Action != ActionWalkAway || !strings.Contains(d.Rationale, "net 7 days is shorter than your minimum of net 15 days") {
		t.Fatalf("want walk_away on terms, got %s %q", d.Action, d.Rationale)
	}
	d = Decide(m, Offer{PriceCents: 95_000, DeliveryDays: i32(5)}, History{Counters: 4, LastSellerCents: 95_000})
	if d.Action != ActionWalkAway {
		t.Fatalf("unstated terms with a net-15 minimum is not acceptable, got %s", d.Action)
	}
	if _, _, ok := PickBest(m, []Candidate{{QuoteID: "cash", Offer: Offer{PriceCents: 91_000, PaymentTermsDays: i32(0)}}}); ok {
		t.Fatal("PickBest must skip terms below the minimum")
	}
}

func TestDecide_NeverAcceptsDeliveryPastLimit(t *testing.T) {
	d := Decide(baseMandate(), Offer{PriceCents: 98_000, DeliveryDays: i32(30)}, History{Counters: 4, LastSellerCents: 98_000})
	if d.Action != ActionWalkAway || !strings.Contains(d.Rationale, "30-day delivery is past your 14-day limit") {
		t.Fatalf("want walk_away on delivery, got %s %q", d.Action, d.Rationale)
	}
	if _, _, ok := PickBest(baseMandate(), []Candidate{{QuoteID: "slow", Offer: Offer{PriceCents: 91_000, DeliveryDays: i32(30)}}}); ok {
		t.Fatal("PickBest must skip delivery past the limit")
	}
}

// Random sweep of mandates, offers and histories: whatever the engine does,
// it never offers above the walk-away price, never counters with terms below
// the minimum or delivery past the limit, and never accepts outside them.
func TestDecide_WalkAwayGuaranteesHoldWithTerms(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	terms := []int32{0, 7, 15, 30, 45, 60, 90}
	for i := 0; i < 5000; i++ {
		target := int64(50_000 + rng.Intn(50_000))
		m := Mandate{
			TargetCents: target, MaxCents: target + int64(rng.Intn(30_000)),
			MaxDeliveryDays: i32(int32(5 + rng.Intn(20))), PriceWeight: 0.3 + rng.Float64()*0.6,
			MaxRounds: 1 + rng.Intn(6), Tone: []string{"firm", "collaborative"}[rng.Intn(2)],
		}
		pref := terms[rng.Intn(len(terms))]
		minT := terms[rng.Intn(len(terms))]
		if minT > pref {
			minT = pref
		}
		m.PreferredTermsDays, m.MinTermsDays = &pref, &minT
		m.TermsWeight = rng.Float64() * (1 - m.PriceWeight)
		o := Offer{PriceCents: target - 10_000 + int64(rng.Intn(80_000)), DeliveryDays: i32(int32(1 + rng.Intn(30))), PaymentTermsDays: &terms[rng.Intn(len(terms))]}
		h := History{Counters: rng.Intn(m.MaxRounds + 1)}
		if h.Counters > 0 {
			h.LastOfferCents = target + int64(rng.Intn(int(m.MaxCents-target)+1))
			h.LastSellerCents = o.PriceCents + int64(rng.Intn(5_000)) - 1_000
			lt := terms[rng.Intn(len(terms))]
			if lt < minT {
				lt = minT
			}
			h.LastOfferTermsDays = &lt
		}
		d := Decide(m, o, h)
		switch d.Action {
		case ActionCounter:
			if d.OfferCents > m.MaxCents {
				t.Fatalf("offered %d above walk-away %d (%+v %+v %+v)", d.OfferCents, m.MaxCents, m, o, h)
			}
			if d.OfferTermsDays == nil || *d.OfferTermsDays < minT {
				t.Fatalf("countered terms %v below minimum %d", d.OfferTermsDays, minT)
			}
			if d.OfferDeliveryDays != nil && *d.OfferDeliveryDays > *m.MaxDeliveryDays {
				t.Fatalf("countered delivery %d past limit %d", *d.OfferDeliveryDays, *m.MaxDeliveryDays)
			}
			// The only allowed retreat is to the seller's own (dropped) ask:
			// matching it, or undercutting it by a cent to press on delivery or terms.
			if h.LastOfferCents > 0 && d.OfferCents < h.LastOfferCents && d.TradeOff == nil && d.OfferCents < o.PriceCents-1 {
				t.Fatalf("price moved backwards without a trade: %d < %d", d.OfferCents, h.LastOfferCents)
			}
		case ActionAcceptReady:
			if o.PriceCents > m.MaxCents || !withinLimits(m, o) {
				t.Fatalf("accepted outside the mandate: %+v %+v", m, o)
			}
		}
	}
}

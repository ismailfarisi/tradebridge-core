package simseller

import (
	"testing"

	"github.com/tradebridge/core/internal/negotiation"
)

// TestBuyerEngineLogrollsAgainstPersonas plays the real buyer strategy
// against each simulated persona, as make demo-seed does, and checks that a
// mandate with payment terms produces at least one logrolled round and still
// settles inside the walk-away price.
func TestBuyerEngineLogrollsAgainstPersonas(t *testing.T) {
	trades := 0
	for _, p := range Personas {
		opening := roundDollars(int64(float64(200_000) * p.Markup))
		priceOnly := negotiation.Mandate{TargetCents: 200_000 * 108 / 100, MaxCents: opening, MaxDeliveryDays: i32p(14), PriceWeight: 0.75, MaxRounds: 4, Tone: "collaborative"}
		withTerms := priceOnly
		withTerms.PriceWeight, withTerms.TermsWeight = 0.7, 0.1
		withTerms.PreferredTermsDays, withTerms.MinTermsDays = i32p(60), i32p(0)

		plain, _ := play(t, p, opening, priceOnly)
		final, n := play(t, p, opening, withTerms)
		trades += n
		if final > withTerms.MaxCents {
			t.Fatalf("%s: settled above walk-away", p.Name)
		}
		t.Logf("%s: price-only %d, with terms %d (%d trades)", p.Name, plain, final, n)
	}
	if trades == 0 {
		t.Fatal("expected at least one logrolled round across the personas")
	}
}

// play runs the negotiation until the engine stops countering, returning the
// seller's final price and how many logrolled counters the engine made.
func play(t *testing.T, p Persona, opening int64, m negotiation.Mandate) (int64, int) {
	t.Helper()
	d := &deal{Opening: opening, Current: opening, Delivery: p.DeliveryDays, Terms: p.TermsDays}
	h := negotiation.History{}
	trades := 0
	for i := 0; i < 10; i++ {
		terms := d.Terms
		dec := negotiation.Decide(m, negotiation.Offer{PriceCents: d.Current, DeliveryDays: &d.Delivery, PaymentTermsDays: &terms, Currency: "AED"}, h)
		if dec.Action != negotiation.ActionCounter {
			return d.Current, trades
		}
		if dec.TradeOff != nil {
			trades++
		}
		h.LastSellerCents = d.Current
		h.Counters++
		h.LastOfferCents = dec.OfferCents
		h.LastOfferDeliveryDays, h.LastOfferTermsDays = dec.OfferDeliveryDays, dec.OfferTermsDays
		reply(p, d, dec.OfferCents, dec.OfferDeliveryDays, dec.OfferTermsDays)
	}
	return d.Current, trades
}

func i32p(v int32) *int32 { return &v }

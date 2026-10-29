// Package negotiation is TradeBridge's autonomous buyer-side negotiator. A
// buyer sets a mandate per RFQ (target price, walk-away price, delivery need,
// price-vs-delivery weight, round budget); the engine answers every supplier
// quote version inside that mandate with a time-dependent concession counter,
// logs each move with its rationale, and closes on the best-utility deal —
// auto-accepting it or holding it for the buyer's approval.
//
// The decision logic in this file is pure (no I/O) so it is exhaustively
// unit-testable and deterministic: the same mandate and offers always produce
// the same moves, which is what a buyer needs to trust an agent with spend.
package negotiation

import (
	"fmt"
	"math"
	"strings"
)

// Actions recorded on a round.
const (
	ActionCounter     = "counter"
	ActionAcceptReady = "accept_ready"
	ActionWalkAway    = "walk_away"
)

// Strategy statuses.
const (
	StatusActive           = "active"
	StatusPaused           = "paused"
	StatusAwaitingApproval = "awaiting_approval"
	StatusCompleted        = "completed"
	StatusCancelled        = "cancelled"
)

// Mandate is the buyer's negotiation brief for one RFQ.
type Mandate struct {
	TargetCents     int64
	MaxCents        int64
	MaxDeliveryDays *int32
	PriceWeight     float64
	MaxRounds       int
	Tone            string // collaborative | firm
}

// Offer is a supplier's current terms.
type Offer struct {
	PriceCents   int64
	DeliveryDays *int32
	Currency     string
}

// History is what the engine already did on one quote.
type History struct {
	Counters        int   // counters already sent on this quote
	LastOfferCents  int64 // our last counter price (0 if none)
	LastSellerCents int64 // seller price we last answered (0 if none)
	Competitors     int   // other live quotes on the RFQ
	BestRivalCents  int64 // lowest rival price (0 if none)
}

// Decision is the engine's move on one supplier version.
type Decision struct {
	Action            string
	OfferCents        int64
	OfferDeliveryDays *int32
	Utility           float64
	Rationale         string
	Message           string
}

// defaultDeliveryHorizon is the delivery scale (days) used for scoring when
// the buyer did not set a delivery requirement.
const defaultDeliveryHorizon = 30

// Utility scores an offer in [0,1] against the mandate: a weighted blend of a
// price score (1 at/below target, 0 at/above walk-away) and a delivery score
// (1 for immediate, falling linearly; offers past the buyer's limit score 0).
func Utility(m Mandate, o Offer) float64 {
	w := clamp(m.PriceWeight, 0, 1)
	return round3(w*priceScore(m, o.PriceCents) + (1-w)*deliveryScore(m, o.DeliveryDays))
}

func priceScore(m Mandate, p int64) float64 {
	if p <= m.TargetCents {
		return 1
	}
	if m.MaxCents <= m.TargetCents {
		if p <= m.MaxCents {
			return 1
		}
		return 0
	}
	return clamp(float64(m.MaxCents-p)/float64(m.MaxCents-m.TargetCents), 0, 1)
}

func deliveryScore(m Mandate, d *int32) float64 {
	if d == nil {
		return 0.5 // unknown lead time: neutral
	}
	limit := float64(defaultDeliveryHorizon)
	if m.MaxDeliveryDays != nil {
		limit = float64(*m.MaxDeliveryDays)
		if float64(*d) > limit {
			return 0
		}
	}
	return clamp(1-float64(*d)/(limit*1.5), 0, 1)
}

// concession is the fraction of the target→walk-away gap conceded at counter
// number n (1-based) of maxRounds. Firm uses a Boulware curve (holds near
// target, concedes late); collaborative is linear.
func concession(n, maxRounds int, tone string) float64 {
	if maxRounds <= 1 {
		return 0
	}
	t := clamp(float64(n-1)/float64(maxRounds-1), 0, 1)
	beta := 1.0
	if tone == "firm" {
		beta = 3.0
	}
	return math.Pow(t, beta)
}

// Decide chooses the move on a supplier's current version.
func Decide(m Mandate, o Offer, h History) Decision {
	u := Utility(m, o)
	deliveryOK := m.MaxDeliveryDays == nil || (o.DeliveryDays != nil && *o.DeliveryDays <= *m.MaxDeliveryDays)
	sellerHeld := h.LastSellerCents > 0 && o.PriceCents >= h.LastSellerCents

	switch {
	case o.PriceCents <= m.TargetCents && deliveryOK:
		return Decision{Action: ActionAcceptReady, Utility: u,
			Rationale: fmt.Sprintf("Offer %s meets the %s target%s.", money(o.PriceCents, o.Currency), money(m.TargetCents, o.Currency), deliveryNote(m, o))}
	case h.Counters >= m.MaxRounds || (sellerHeld && h.Counters > 0):
		why := "Round budget used"
		if h.Counters < m.MaxRounds {
			why = "Supplier held its price (final offer)"
		}
		if o.PriceCents <= m.MaxCents {
			return Decision{Action: ActionAcceptReady, Utility: u,
				Rationale: fmt.Sprintf("%s; %s is within the %s walk-away price.", why, money(o.PriceCents, o.Currency), money(m.MaxCents, o.Currency))}
		}
		return Decision{Action: ActionWalkAway, Utility: u,
			Rationale: fmt.Sprintf("%s; %s is above the %s walk-away price.", why, money(o.PriceCents, o.Currency), money(m.MaxCents, o.Currency))}
	}

	n := h.Counters + 1
	offer := m.TargetCents + int64(math.Round(float64(m.MaxCents-m.TargetCents)*concession(n, m.MaxRounds, m.Tone)))
	// Use competition as leverage: never offer more than the best rival.
	if h.BestRivalCents > 0 && h.BestRivalCents < o.PriceCents && offer > h.BestRivalCents {
		offer = max64(h.BestRivalCents, m.TargetCents)
	}
	if h.LastOfferCents > offer {
		offer = h.LastOfferCents // never move backwards
	}
	if offer >= o.PriceCents {
		// Our next step would meet or exceed their ask: take their price if acceptable.
		if o.PriceCents <= m.MaxCents && deliveryOK {
			return Decision{Action: ActionAcceptReady, Utility: u,
				Rationale: fmt.Sprintf("Offer %s is at or below our next step; accepting it.", money(o.PriceCents, o.Currency))}
		}
		offer = o.PriceCents - 1
	}
	var offerDelivery *int32
	if m.MaxDeliveryDays != nil && (o.DeliveryDays == nil || *o.DeliveryDays > *m.MaxDeliveryDays) {
		d := *m.MaxDeliveryDays
		offerDelivery = &d
	} else if o.DeliveryDays != nil {
		d := *o.DeliveryDays
		offerDelivery = &d
	}

	rationale := fmt.Sprintf("Round %d of %d: supplier at %s, countering at %s (%s concession curve).",
		n, m.MaxRounds, money(o.PriceCents, o.Currency), money(offer, o.Currency), toneLabel(m.Tone))
	if h.BestRivalCents > 0 && h.BestRivalCents < o.PriceCents {
		rationale += fmt.Sprintf(" A rival quote is at %s.", money(h.BestRivalCents, o.Currency))
	}
	return Decision{
		Action: ActionCounter, OfferCents: offer, OfferDeliveryDays: offerDelivery, Utility: u,
		Rationale: rationale, Message: counterMessage(m, o, h, offer, offerDelivery, n),
	}
}

func counterMessage(m Mandate, o Offer, h History, offer int64, d *int32, n int) string {
	var b strings.Builder
	if m.Tone == "firm" {
		b.WriteString("Thank you for the quote. ")
	} else {
		b.WriteString("Thanks for your offer — we'd like to work with you on this. ")
	}
	if h.Competitors > 0 {
		b.WriteString(fmt.Sprintf("We are evaluating %d other competitive offer%s. ", h.Competitors, plural(h.Competitors)))
	}
	b.WriteString(fmt.Sprintf("We can commit today at %s", money(offer, o.Currency)))
	if d != nil {
		b.WriteString(fmt.Sprintf(" with delivery within %d days", *d))
	}
	b.WriteString(".")
	if n >= m.MaxRounds {
		b.WriteString(" This is our final offer.")
	} else if m.Tone != "firm" {
		b.WriteString(" Happy to discuss volume or payment terms if that helps you get there.")
	}
	return b.String()
}

// Candidate is a quote considered for the close decision.
type Candidate struct {
	QuoteID string
	Offer   Offer
}

// PickBest returns the highest-utility candidate within the walk-away price
// (ties broken by lower price), or ok=false when none qualifies.
func PickBest(m Mandate, cs []Candidate) (Candidate, float64, bool) {
	var best Candidate
	bestU := -1.0
	for _, c := range cs {
		if c.Offer.PriceCents > m.MaxCents {
			continue
		}
		u := Utility(m, c.Offer)
		if u > bestU || (u == bestU && c.Offer.PriceCents < best.Offer.PriceCents) {
			best, bestU = c, u
		}
	}
	return best, bestU, bestU >= 0
}

func deliveryNote(m Mandate, o Offer) string {
	if o.DeliveryDays == nil {
		return ""
	}
	return fmt.Sprintf(" with %d-day delivery", *o.DeliveryDays)
}

func toneLabel(t string) string {
	if t == "firm" {
		return "firm"
	}
	return "collaborative"
}

func money(cents int64, currency string) string {
	if currency == "" {
		currency = "AED"
	}
	return fmt.Sprintf("%s %s", currency, formatThousands(cents))
}

func formatThousands(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	whole, frac := cents/100, cents%100
	s := fmt.Sprintf("%d", whole)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	out := fmt.Sprintf("%s.%02d", s, frac)
	if neg {
		return "-" + out
	}
	return out
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func round3(v float64) float64        { return math.Round(v*1000) / 1000 }
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Package negotiation is TradeBridge's autonomous buyer-side negotiator. A
// buyer sets a mandate per RFQ (target price, walk-away price, delivery need,
// optional payment terms, issue weights, round budget); the engine answers every supplier
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

	// Payment terms (days after delivery the buyer has to pay; 0 = cash).
	// Longer is better for the buyer's cash flow. PreferredTermsDays nil
	// means terms are not negotiated at all and the engine behaves exactly
	// as a two-issue (price, delivery) negotiator.
	PreferredTermsDays *int32
	// MinTermsDays is the shortest terms the buyer accepts; nil = any.
	MinTermsDays *int32
	// TermsWeight is the share of utility given to terms. Weights are shares
	// of one: price PriceWeight, terms TermsWeight, delivery the rest.
	TermsWeight float64
}

// Offer is a supplier's current terms.
type Offer struct {
	PriceCents       int64
	DeliveryDays     *int32
	PaymentTermsDays *int32
	Currency         string
}

// History is what the engine already did on one quote.
type History struct {
	Counters        int   // counters already sent on this quote
	LastOfferCents  int64 // our last counter price (0 if none)
	LastSellerCents int64 // seller price we last answered (0 if none)
	Competitors     int   // other live quotes on the RFQ
	BestRivalCents  int64 // lowest rival price (0 if none)

	LastOfferDeliveryDays *int32 // delivery in our last counter (nil if none)
	LastOfferTermsDays    *int32 // payment terms in our last counter (nil if none)
}

// Trade-off issues a logrolled counter can concede on.
const (
	IssueTerms    = "payment_terms"
	IssueDelivery = "delivery"
)

// TradeOff describes a logrolled counter: the buyer conceded on a lower-weight
// issue (From → To) instead of conceding as much on price. PriceCreditCents is
// how much lower our price is than a straight concession would have been.
type TradeOff struct {
	Issue            string
	From, To         int32
	PriceCreditCents int64
}

// Decision is the engine's move on one supplier version.
type Decision struct {
	Action            string
	OfferCents        int64
	OfferDeliveryDays *int32
	OfferTermsDays    *int32
	TradeOff          *TradeOff
	Final             bool // the counter is our last one on this quote
	Utility           float64
	Rationale         string
	Message           string
}

// defaultDeliveryHorizon is the delivery scale (days) used for scoring when
// the buyer did not set a delivery requirement.
const defaultDeliveryHorizon = 30

// Utility scores an offer in [0,1] against the mandate: a weighted blend of a
// price score (1 at/below target, 0 at/above walk-away), a delivery score
// (1 for immediate, falling linearly; offers past the buyer's limit score 0)
// and, when the mandate sets payment terms, a terms score (1 at/above the
// preferred terms, falling to 0 for cash; below the minimum scores 0).
func Utility(m Mandate, o Offer) float64 {
	wp, wd, wt := weights(m)
	u := wp*priceScore(m, o.PriceCents) + wd*deliveryScore(m, o.DeliveryDays)
	if wt > 0 {
		u += wt * termsScore(m, o.PaymentTermsDays)
	}
	return round3(u)
}

// weights splits utility across price, delivery and terms. Without a terms
// preference it is exactly the two-issue split (price w, delivery 1-w).
func weights(m Mandate) (wp, wd, wt float64) {
	wp = clamp(m.PriceWeight, 0, 1)
	if m.PreferredTermsDays == nil {
		return wp, 1 - wp, 0
	}
	wt = clamp(m.TermsWeight, 0, 1-wp)
	return wp, 1 - wp - wt, wt
}

func termsScore(m Mandate, t *int32) float64 {
	if m.PreferredTermsDays == nil {
		return 1
	}
	if t == nil {
		return 0.5 // unstated terms: neutral
	}
	if m.MinTermsDays != nil && *t < *m.MinTermsDays {
		return 0
	}
	p := float64(*m.PreferredTermsDays)
	if p <= 0 {
		return 1 // buyer is happy to pay cash
	}
	return clamp(float64(*t)/p, 0, 1)
}

// termsOK reports whether the offer's payment terms are inside the mandate.
// Unstated terms only satisfy a minimum of 0 (cash is acceptable).
func termsOK(m Mandate, o Offer) bool {
	if m.MinTermsDays == nil {
		return true
	}
	if o.PaymentTermsDays == nil {
		return *m.MinTermsDays == 0
	}
	return *o.PaymentTermsDays >= *m.MinTermsDays
}

// withinLimits is the hard walk-away check on the non-price issues: a stated
// delivery beyond the buyer's limit, or terms below the buyer's minimum, can
// never be accepted.
func withinLimits(m Mandate, o Offer) bool {
	if m.MaxDeliveryDays != nil && o.DeliveryDays != nil && *o.DeliveryDays > *m.MaxDeliveryDays {
		return false
	}
	return termsOK(m, o)
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
//
// With payment terms in the mandate, Decide logrolls: when the supplier
// resists on price it may concede on the issue the buyer weighs less (faster
// payment, or a longer delivery within the limit) and ask for a lower price in
// return, sized so the buyer's utility matches a straight price concession.
// The walk-away guarantees hold throughout: never offer above the walk-away
// price, never accept delivery past the limit or terms below the minimum.
func Decide(m Mandate, o Offer, h History) Decision {
	u := Utility(m, o)
	deliveryOK := m.MaxDeliveryDays == nil || (o.DeliveryDays != nil && *o.DeliveryDays <= *m.MaxDeliveryDays)
	tOK := termsOK(m, o)
	sellerHeld := h.LastSellerCents > 0 && o.PriceCents >= h.LastSellerCents
	baseDelivery, baseTerms := counterTerms(m, o, h)
	lr, canLogroll := planLogroll(m, o, h, baseDelivery, baseTerms)

	switch {
	case o.PriceCents <= m.TargetCents && deliveryOK && tOK:
		return Decision{Action: ActionAcceptReady, Utility: u,
			Rationale: fmt.Sprintf("%s meets your %s target%s%s.", money(o.PriceCents, o.Currency), money(m.TargetCents, o.Currency), deliveryNote(m, o), termsNote(m, o))}
	case h.Counters >= m.MaxRounds || (sellerHeld && h.Counters > 0 && !canLogroll):
		why := "No rounds left"
		if h.Counters < m.MaxRounds {
			why = "Supplier held its price"
		}
		if o.PriceCents > m.MaxCents {
			return Decision{Action: ActionWalkAway, Utility: u,
				Rationale: fmt.Sprintf("%s; %s is above your %s walk-away price.", why, money(o.PriceCents, o.Currency), money(m.MaxCents, o.Currency))}
		}
		if !withinLimits(m, o) {
			return Decision{Action: ActionWalkAway, Utility: u,
				Rationale: fmt.Sprintf("%s; %s.", why, limitBreach(m, o))}
		}
		return Decision{Action: ActionAcceptReady, Utility: u,
			Rationale: fmt.Sprintf("%s; %s is within your %s walk-away price.", why, money(o.PriceCents, o.Currency), money(m.MaxCents, o.Currency))}
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
		if o.PriceCents <= m.MaxCents && deliveryOK && tOK {
			return Decision{Action: ActionAcceptReady, Utility: u,
				Rationale: fmt.Sprintf("%s is at or below what we would offer next, so it is acceptable.", money(o.PriceCents, o.Currency))}
		}
		offer = o.PriceCents - 1
		canLogroll = false // nothing left to trade on price
	}
	offerDelivery, offerTerms := baseDelivery, baseTerms

	var trade *TradeOff
	if canLogroll {
		straight := offer
		credit := int64(math.Round(lr.cost / lr.priceWeight * float64(m.MaxCents-m.TargetCents)))
		lowered := roundToUnit(straight - credit)
		floor := max64(h.LastOfferCents, m.TargetCents)
		if lowered < floor {
			lowered = floor
		}
		if lowered > straight {
			lowered = straight
		}
		offer = lowered
		trade = &TradeOff{Issue: lr.issue, From: lr.from, To: lr.to, PriceCreditCents: straight - lowered}
		v := lr.to
		if lr.issue == IssueTerms {
			offerTerms = &v
		} else {
			offerDelivery = &v
		}
	}

	rationale := fmt.Sprintf("They asked %s; we offered %s (round %d of %d).",
		money(o.PriceCents, o.Currency), money(offer, o.Currency), n, m.MaxRounds)
	if h.BestRivalCents > 0 && h.BestRivalCents < o.PriceCents {
		rationale += fmt.Sprintf(" Best rival offer is %s.", money(h.BestRivalCents, o.Currency))
	}
	if trade != nil {
		rationale += " " + tradeRationale(m, o, h, trade, sellerHeld)
	}
	d := Decision{
		Action: ActionCounter, OfferCents: offer, OfferDeliveryDays: offerDelivery, OfferTermsDays: offerTerms,
		TradeOff: trade, Final: n >= m.MaxRounds, Utility: u, Rationale: rationale,
	}
	d.Message = counterMessage(m, o, h, d, n)
	return d
}

// counterTerms are the delivery and payment terms we ask for on a straight
// counter. Delivery is pushed into the buyer's limit (or the supplier's own
// lead time is kept); terms ask for the buyer's preference unless the
// supplier already offers longer. A concession made in an earlier logroll is
// never taken back.
func counterTerms(m Mandate, o Offer, h History) (delivery, terms *int32) {
	if m.MaxDeliveryDays != nil && (o.DeliveryDays == nil || *o.DeliveryDays > *m.MaxDeliveryDays) {
		d := *m.MaxDeliveryDays
		delivery = &d
	} else if o.DeliveryDays != nil {
		d := *o.DeliveryDays
		delivery = &d
	}
	if m.PreferredTermsDays == nil {
		return delivery, nil
	}
	if last := h.LastOfferDeliveryDays; last != nil && delivery != nil && *last > *delivery &&
		(m.MaxDeliveryDays == nil || *last <= *m.MaxDeliveryDays) {
		d := *last
		delivery = &d
	}
	ask := *m.PreferredTermsDays
	if last := h.LastOfferTermsDays; last != nil && *last < ask {
		ask = *last
	}
	if o.PaymentTermsDays != nil && *o.PaymentTermsDays >= ask {
		ask = *o.PaymentTermsDays
	}
	return delivery, &ask
}

type logrollPlan struct {
	issue       string
	from, to    int32
	cost        float64 // buyer utility given up on the conceded issue
	priceWeight float64
}

// standardTerms are the payment terms suppliers recognise; a terms concession
// snaps to one of these.
var standardTerms = []int32{0, 7, 15, 30, 45, 60, 90, 120, 180}

// planLogroll decides whether, and on which issue, to trade. It only fires
// when the mandate negotiates payment terms and the supplier is resisting on
// price (held, or conceded under a third of the gap since our last offer).
// It concedes on the issue the buyer weighs less that still has room.
func planLogroll(m Mandate, o Offer, h History, delivery, terms *int32) (logrollPlan, bool) {
	wp, wd, wt := weights(m)
	if m.PreferredTermsDays == nil || wp <= 0 || h.Counters == 0 || h.LastSellerCents == 0 || h.LastOfferCents == 0 {
		return logrollPlan{}, false
	}
	gap := h.LastSellerCents - h.LastOfferCents
	moved := h.LastSellerCents - o.PriceCents
	resisting := o.PriceCents >= h.LastSellerCents || (gap > 0 && moved*3 < gap)
	if !resisting {
		return logrollPlan{}, false
	}
	var opts []logrollPlan
	if terms != nil {
		floor := int32(0)
		if m.MinTermsDays != nil {
			floor = *m.MinTermsDays
		}
		if o.PaymentTermsDays != nil && *o.PaymentTermsDays > floor && *o.PaymentTermsDays < *terms {
			floor = *o.PaymentTermsDays // no point paying faster than they ask
		}
		if *terms > floor {
			mid := float64(*terms+floor) / 2
			to := floor
			for _, st := range standardTerms {
				if st >= floor && float64(st) <= mid && st < *terms {
					to = st
				}
			}
			from := *terms
			opts = append(opts, logrollPlan{issue: IssueTerms, from: from, to: to,
				cost: wt * (termsScore(m, &from) - termsScore(m, &to)), priceWeight: wp})
		}
	}
	if m.MaxDeliveryDays != nil && delivery != nil && *delivery < *m.MaxDeliveryDays {
		from := *delivery
		to := int32(math.Ceil(float64(from+*m.MaxDeliveryDays) / 2))
		if to <= from {
			to = *m.MaxDeliveryDays
		}
		opts = append(opts, logrollPlan{issue: IssueDelivery, from: from, to: to,
			cost: wd * (deliveryScore(m, &from) - deliveryScore(m, &to)), priceWeight: wp})
	}
	if len(opts) == 0 {
		return logrollPlan{}, false
	}
	best := opts[0]
	if len(opts) > 1 && wd < wt {
		best = opts[1] // delivery matters less to this buyer than terms
	}
	return best, true
}

func tradeRationale(m Mandate, o Offer, h History, t *TradeOff, held bool) string {
	wp, wd, wt := weights(m)
	resist := fmt.Sprintf("They only moved from %s to %s", money(h.LastSellerCents, o.Currency), money(o.PriceCents, o.Currency))
	if held {
		resist = fmt.Sprintf("They held at %s", money(o.PriceCents, o.Currency))
	}
	var what string
	var w float64
	switch t.Issue {
	case IssueTerms:
		what = fmt.Sprintf("faster payment (%s instead of %s)", termsLabel(t.To), termsLabel(t.From))
		w = wt
	default:
		what = fmt.Sprintf("a longer delivery (%d days instead of %d, within your %d-day limit)", t.To, t.From, *m.MaxDeliveryDays)
		w = wd
	}
	issueName := map[string]string{IssueTerms: "payment terms", IssueDelivery: "delivery"}[t.Issue]
	price := fmt.Sprintf("%s less than a straight price concession", money(t.PriceCreditCents, o.Currency))
	if t.PriceCreditCents == 0 {
		price = "no extra price concession"
	}
	return fmt.Sprintf("%s. Trade-off: %s matter%s less to you (%d%%) than price (%d%%), so we offered %s for %s.",
		resist, issueName, verbS(issueName), pct(w), pct(wp), what, price)
}

func verbS(noun string) string {
	if strings.HasSuffix(noun, "s") {
		return ""
	}
	return "s"
}

func pct(w float64) int { return int(math.Round(w * 100)) }

// termsLabel renders payment terms for messages and rationales.
func termsLabel(days int32) string {
	if days == 0 {
		return "cash on delivery"
	}
	return fmt.Sprintf("net %d days", days)
}

func counterMessage(m Mandate, o Offer, h History, d Decision, n int) string {
	var b strings.Builder
	switch {
	case n > 1:
		b.WriteString("Thanks for the revised quote. ")
	case m.Tone == "firm":
		b.WriteString("Thank you for the quote. ")
	default:
		b.WriteString("Thanks for your offer — we'd like to work with you on this. ")
	}
	if h.Competitors > 0 {
		verb := "are evaluating"
		if n > 1 {
			verb = "still have"
		}
		b.WriteString(fmt.Sprintf("We %s %d other competitive offer%s. ", verb, h.Competitors, plural(h.Competitors)))
	}
	b.WriteString(fmt.Sprintf("We can commit today at %s", money(d.OfferCents, o.Currency)))
	if d.OfferDeliveryDays != nil {
		b.WriteString(fmt.Sprintf(" with delivery within %d days", *d.OfferDeliveryDays))
	}
	if d.OfferTermsDays != nil {
		if d.OfferDeliveryDays != nil {
			b.WriteString(" and")
		} else {
			b.WriteString(" with")
		}
		if *d.OfferTermsDays == 0 {
			b.WriteString(" payment in cash on delivery")
		} else {
			b.WriteString(fmt.Sprintf(" payment within %d days", *d.OfferTermsDays))
		}
	}
	b.WriteString(".")
	if t := d.TradeOff; t != nil {
		switch t.Issue {
		case IssueTerms:
			b.WriteString(fmt.Sprintf(" To help your cash flow we can pay faster at this price: %s instead of %s.", termsLabel(t.To), termsLabel(t.From)))
		case IssueDelivery:
			b.WriteString(fmt.Sprintf(" To give you more time we can accept delivery within %d days instead of %d at this price.", t.To, t.From))
		}
	}
	if n >= m.MaxRounds {
		b.WriteString(" This is our final offer.")
	} else if m.Tone != "firm" && n == 1 {
		if m.PreferredTermsDays != nil {
			b.WriteString(" Happy to discuss volume or delivery if that helps you get there.")
		} else {
			b.WriteString(" Happy to discuss volume or payment terms if that helps you get there.")
		}
	}
	return b.String()
}

// Candidate is a quote considered for the close decision.
type Candidate struct {
	QuoteID string
	Offer   Offer
}

// PickBest returns the highest-utility candidate within the walk-away price
// and the mandate's delivery and payment-terms limits (ties broken by lower
// price), or ok=false when none qualifies.
func PickBest(m Mandate, cs []Candidate) (Candidate, float64, bool) {
	var best Candidate
	bestU := -1.0
	for _, c := range cs {
		if c.Offer.PriceCents > m.MaxCents || !withinLimits(m, c.Offer) {
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

func termsNote(m Mandate, o Offer) string {
	if m.PreferredTermsDays == nil || o.PaymentTermsDays == nil {
		return ""
	}
	return " on " + termsLabel(*o.PaymentTermsDays)
}

// limitBreach explains why an offer inside the walk-away price still fails
// the mandate.
func limitBreach(m Mandate, o Offer) string {
	if m.MaxDeliveryDays != nil && o.DeliveryDays != nil && *o.DeliveryDays > *m.MaxDeliveryDays {
		return fmt.Sprintf("%d-day delivery is past your %d-day limit", *o.DeliveryDays, *m.MaxDeliveryDays)
	}
	if o.PaymentTermsDays == nil {
		return fmt.Sprintf("they did not offer payment terms and you need at least %s", termsLabel(*m.MinTermsDays))
	}
	return fmt.Sprintf("%s is shorter than your minimum of %s", termsLabel(*o.PaymentTermsDays), termsLabel(*m.MinTermsDays))
}

// roundToUnit rounds cents to a whole currency unit, the figure a person types.
func roundToUnit(cents int64) int64 { return int64(math.Round(float64(cents)/100)) * 100 }

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
	out := s
	if frac != 0 {
		out = fmt.Sprintf("%s.%02d", s, frac)
	}
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

package negotiation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/rfq"
)

// Message sources recorded on a round.
const (
	MessageTemplate = "template"
	MessageLLM      = "llm"
)

// PhraseInput is a counter the engine already decided, handed to a phraser
// to word for the supplier. Every figure in it is final: the phraser may only
// restate them.
type PhraseInput struct {
	Tone             string    `json:"tone"`
	Currency         string    `json:"currency"`
	SellerPriceCents int64     `json:"seller_price_cents"`
	OfferCents       int64     `json:"offer_price_cents"`
	DeliveryDays     *int32    `json:"offer_delivery_days,omitempty"`
	TermsDays        *int32    `json:"offer_payment_terms_days,omitempty"`
	TradeOff         *TradeOff `json:"trade_off,omitempty"`
	Round            int       `json:"round"`
	Final            bool      `json:"final_offer"`
	Competitors      int       `json:"other_competitive_offers"`
	Template         string    `json:"reference_message"`
}

// MessagePhraser words a decided counter as a supplier-facing message. It
// never chooses the action or any number; the engine verifies its output and
// falls back to the deterministic template on any doubt.
type MessagePhraser interface {
	Phrase(ctx context.Context, in PhraseInput) (string, error)
}

// defaultPhraseTimeout keeps a slow model from stalling a negotiation tick.
const defaultPhraseTimeout = 4 * time.Second

// SetPhraser enables LLM phrasing of counter messages. A nil phraser (the
// default) always uses the template. timeout <= 0 uses a short default.
func (e *Engine) SetPhraser(p MessagePhraser, timeout time.Duration) {
	if timeout <= 0 {
		timeout = defaultPhraseTimeout
	}
	e.phraser, e.phraseTimeout = p, timeout
}

// phraseInput builds the phraser's view of a counter decision.
func phraseInput(m Mandate, o Offer, h History, d Decision) PhraseInput {
	return PhraseInput{
		Tone: m.Tone, Currency: currencyOr(o.Currency), SellerPriceCents: o.PriceCents,
		OfferCents: d.OfferCents, DeliveryDays: d.OfferDeliveryDays, TermsDays: d.OfferTermsDays,
		TradeOff: d.TradeOff, Round: h.Counters + 1, Final: d.Final, Competitors: h.Competitors,
		Template: d.Message,
	}
}

// phrase returns the supplier-facing message for a counter and where it came
// from. Any phraser error, timeout or failed verification yields the template.
func (e *Engine) phrase(ctx context.Context, in PhraseInput) (string, string) {
	if e.phraser == nil {
		return in.Template, MessageTemplate
	}
	pctx, cancel := context.WithTimeout(ctx, e.phraseTimeout)
	defer cancel()
	text, err := e.phraser.Phrase(pctx, in)
	if err == nil {
		text = cleanPhrased(text)
		err = VerifyMessage(text, in)
	}
	if err != nil {
		e.logger.Warn().Err(err).Msg("negotiation: LLM phrasing rejected; using template")
		return in.Template, MessageTemplate
	}
	return text, MessageLLM
}

func cleanPhrased(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"“”")
	return strings.TrimSpace(s)
}

var (
	reNumber = regexp.MustCompile(`\d[\d,]*(?:\.\d+)?`)
	// A money amount: a number directly after or before a currency marker.
	// Currency codes are matched case-sensitively so "for 30 days" is not money.
	reMoney = regexp.MustCompile(`(?:\b[A-Z]{3}\b|\$|€|£|(?i:\bdhs?\b|\bdirhams?\b))\s?(\d[\d,]*(?:\.\d+)?)|(\d[\d,]*(?:\.\d+)?)\s?(?:\b[A-Z]{3}\b|(?i:\bdhs?\b|\bdirhams?\b))`)
	reFinal = regexp.MustCompile(`(?i)\b(final|last|best and final)\s+(offer|price|round)\b`)
	reCash  = regexp.MustCompile(`(?i)\bcash\b|\b(on|upon)\s+delivery\b`)
)

const maxPhrasedLen = 1200

// VerifyMessage is the hard guard on LLM output: the text must state the
// engine's price, delivery and payment terms exactly, may contain no other
// figures (in particular no other money amount), and may not claim a final
// offer the engine did not make.
func VerifyMessage(text string, in PhraseInput) error {
	if text == "" {
		return errors.New("empty message")
	}
	if len(text) > maxPhrasedLen {
		return errors.New("message too long")
	}
	price := float64(in.OfferCents) / 100
	allowed := []float64{price}
	if in.DeliveryDays != nil {
		allowed = append(allowed, float64(*in.DeliveryDays))
	}
	if in.TermsDays != nil {
		allowed = append(allowed, float64(*in.TermsDays))
	}
	if t := in.TradeOff; t != nil {
		allowed = append(allowed, float64(t.From), float64(t.To))
	}
	if in.Competitors > 0 {
		allowed = append(allowed, float64(in.Competitors))
	}
	seen := map[float64]bool{}
	for _, raw := range reNumber.FindAllString(text, -1) {
		v, ok := parseNumber(raw)
		if !ok {
			return fmt.Errorf("unparseable figure %q", raw)
		}
		if !contains(allowed, v) {
			return fmt.Errorf("figure %q is not one of the engine's numbers", raw)
		}
		seen[v] = true
	}
	for _, mm := range reMoney.FindAllStringSubmatch(text, -1) {
		raw := mm[1]
		if raw == "" {
			raw = mm[2]
		}
		if v, _ := parseNumber(raw); !sameNumber(v, price) {
			return fmt.Errorf("money amount %q is not the offer price", raw)
		}
	}
	if !seenEqual(seen, price) {
		return errors.New("offer price missing")
	}
	if in.DeliveryDays != nil && !seenEqual(seen, float64(*in.DeliveryDays)) {
		return errors.New("delivery days missing")
	}
	if in.TermsDays != nil {
		if *in.TermsDays == 0 {
			if !reCash.MatchString(text) {
				return errors.New("cash payment terms missing")
			}
		} else if !seenEqual(seen, float64(*in.TermsDays)) {
			return errors.New("payment terms missing")
		}
	}
	if !in.Final && reFinal.MatchString(text) {
		return errors.New("claims a final offer the engine did not make")
	}
	return nil
}

func parseNumber(raw string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimRight(raw, ".,"), ",", ""), 64)
	return v, err == nil
}

func sameNumber(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func contains(xs []float64, v float64) bool {
	for _, x := range xs {
		if sameNumber(x, v) {
			return true
		}
	}
	return false
}

func seenEqual(seen map[float64]bool, v float64) bool {
	for k := range seen {
		if sameNumber(k, v) {
			return true
		}
	}
	return false
}

func currencyOr(c string) string {
	if c == "" {
		return "AED"
	}
	return c
}

// phraseSystemPrompt instructs the model to word, never to decide.
const phraseSystemPrompt = `You write one short message from a buyer's procurement team to a supplier during a price negotiation.
The buyer is represented by an AI negotiation agent; say so plainly where it reads naturally (for example "I'm the buyer's AI purchasing agent").
The negotiating move is ALREADY decided. Restate it; do not change it.
Rules:
- Use exactly the figures given: the offer price (with its currency), the delivery days and the payment terms. Write the price exactly as given in "price_text".
- Do not mention any other number or money amount: no supplier price, no target, no walk-away, no discounts, no percentages, no dates, no round numbers.
- Only mention other competitive offers if "other_competitive_offers" is above 0, and then only their count.
- Only call it a final offer if "final_offer" is true.
- If "trade_off" is present, explain the trade honestly: the buyer moves on that issue (from -> to) in exchange for the price.
- Be courteous and concise (2-4 sentences), in English, matching the tone ("firm" or "collaborative").
- Reply with the message text only: no subject line, no greeting placeholders, no signature, no quotes.`

// LLMPhraser phrases counters with the platform's configured LLM. It resolves
// the provider settings on every call, so an admin adding or removing the
// key takes effect without a restart; with no key it returns an error and the
// engine uses the template.
type LLMPhraser struct {
	resolver rfq.ExtractorResolver
	logger   zerolog.Logger
	build    func(rfq.ProviderConfig, zerolog.Logger) rfq.Completer
}

// NewLLMPhraser builds a phraser over the platform LLM settings.
func NewLLMPhraser(resolver rfq.ExtractorResolver, logger zerolog.Logger) *LLMPhraser {
	return &LLMPhraser{resolver: resolver, logger: logger, build: rfq.BuildCompleter}
}

// ErrNoLLM means no LLM provider key is configured.
var ErrNoLLM = errors.New("no LLM configured")

// Phrase implements MessagePhraser.
func (p *LLMPhraser) Phrase(ctx context.Context, in PhraseInput) (string, error) {
	cfg, err := p.resolver.ExtractionConfig(ctx, "")
	if err != nil {
		return "", err
	}
	c := p.build(cfg, p.logger)
	if c == nil {
		return "", ErrNoLLM
	}
	return c.Complete(ctx, phraseSystemPrompt, phraseUserPrompt(in))
}

func phraseUserPrompt(in PhraseInput) string {
	facts := map[string]any{
		"tone":                     in.Tone,
		"price_text":               money(in.OfferCents, in.Currency),
		"final_offer":              in.Final,
		"other_competitive_offers": in.Competitors,
		"first_message":            in.Round == 1,
	}
	if in.DeliveryDays != nil {
		facts["delivery_within_days"] = *in.DeliveryDays
	}
	if in.TermsDays != nil {
		facts["payment_terms"] = termsLabel(*in.TermsDays)
	}
	if t := in.TradeOff; t != nil {
		tf := map[string]any{"issue": t.Issue}
		if t.Issue == IssueTerms {
			tf["from"], tf["to"] = termsLabel(t.From), termsLabel(t.To)
		} else {
			tf["from"], tf["to"] = fmt.Sprintf("%d days", t.From), fmt.Sprintf("%d days", t.To)
		}
		facts["trade_off"] = tf
	}
	b, _ := json.MarshalIndent(facts, "", "  ")
	return "Decided move:\n" + string(b) + "\n\nA plain reference wording (improve it, keep every figure):\n" + in.Template
}

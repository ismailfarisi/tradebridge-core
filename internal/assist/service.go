// Package assist provides the buyer-facing AI assistant: sharpening an RFQ
// draft before it's sent, and explaining a quote comparison in plain
// language. Both run on Claude Haiku when an API key is configured, and fall
// back to a deterministic, pure-Go stub (no network) when it isn't — or when
// the SDK call itself errors, so a provider hiccup never hard-fails the
// assist call.
package assist

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rs/zerolog"
)

// haikuModel is pinned so assist cost/latency stay predictable regardless of
// the platform-wide extraction model configured elsewhere.
const haikuModel = "claude-haiku-4-5-20251001"

const improveRFQSystemPrompt = `You help a B2B buyer sharpen a draft RFQ (request for quote) before sending it
to suppliers. Rewrite their draft to be clearer and more complete, and call out anything important that's
missing (quantity, deadline, budget, specifications). Respond with plain text only, no markdown headers.
Keep it concise — a few short paragraphs or a short list at most.`

const explainQuotesSystemPrompt = `You help a B2B buyer compare quotes from multiple suppliers on one RFQ.
Given the RFQ title and a list of quotes (seller, price, delivery time, review count/rating, verification
status), write a short, plain-language comparison highlighting the cheapest, fastest, and best-rated
options, and note any tradeoffs. Respond with plain text only, no markdown headers. Keep it concise.`

// Service calls Anthropic Haiku to power the buyer assistant, degrading to a
// deterministic stub when no API key is configured.
type Service struct {
	apiKey string
	model  string
	logger zerolog.Logger
}

// NewService builds the assist service. An empty apiKey makes every call use
// the deterministic stub (no network) — the path exercised by tests and any
// keyless deployment.
func NewService(apiKey string, logger zerolog.Logger) *Service {
	return &Service{apiKey: apiKey, model: haikuModel, logger: logger}
}

// QuoteSummary is the minimal view of a quote ExplainQuotes reasons over.
type QuoteSummary struct {
	SellerName   string
	PriceCents   int64
	Currency     string
	DeliveryDays *int32
	ReviewCount  int64
	AvgRating    float64
	Verified     bool
}

// ImproveRFQ returns a sharpened, clarified version of a buyer's RFQ draft.
func (s *Service) ImproveRFQ(ctx context.Context, draft string) (string, error) {
	if s.apiKey == "" {
		return stubImproveRFQ(draft), nil
	}
	out, err := s.callHaiku(ctx, improveRFQSystemPrompt, draft)
	if err != nil {
		s.logger.Warn().Err(err).Msg("assist: haiku ImproveRFQ call failed; using deterministic stub")
		return stubImproveRFQ(draft), nil
	}
	return out, nil
}

// ExplainQuotes returns a plain-language comparison of the RFQ's quotes.
func (s *Service) ExplainQuotes(ctx context.Context, rfqTitle string, quotes []QuoteSummary) (string, error) {
	if len(quotes) == 0 {
		return "No quotes yet for \"" + rfqTitle + "\". Check back once suppliers respond.", nil
	}
	if s.apiKey == "" {
		return stubExplainQuotes(rfqTitle, quotes), nil
	}
	out, err := s.callHaiku(ctx, explainQuotesSystemPrompt, formatQuotesPrompt(rfqTitle, quotes))
	if err != nil {
		s.logger.Warn().Err(err).Msg("assist: haiku ExplainQuotes call failed; using deterministic stub")
		return stubExplainQuotes(rfqTitle, quotes), nil
	}
	return out, nil
}

// callHaiku mirrors internal/rfq/claude.go's anthropic-sdk-go usage: a single
// system + user turn, extracting the first text block from the reply.
func (s *Service) callHaiku(ctx context.Context, system, user string) (string, error) {
	client := anthropic.NewClient(option.WithAPIKey(s.apiKey))
	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(s.model),
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", err
	}
	var out strings.Builder
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			out.WriteString(tb.Text)
		}
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", fmt.Errorf("assist: empty response from haiku")
	}
	return text, nil
}

// stubImproveRFQ is the deterministic, keyless fallback: it echoes the draft
// wrapped with a checklist prompt so a buyer still gets actionable guidance
// without an LLM call.
func stubImproveRFQ(draft string) string {
	draft = strings.TrimSpace(draft)
	if draft == "" {
		draft = "(empty draft)"
	}
	return fmt.Sprintf("Suggested: %s. Consider adding quantity, deadline, and budget.", draft)
}

// stubExplainQuotes is the deterministic, keyless fallback: it computes the
// cheapest, fastest, and best-rated quote in pure Go and summarizes them.
func stubExplainQuotes(rfqTitle string, quotes []QuoteSummary) string {
	cheapest, fastest, best := quotes[0], quotes[0], quotes[0]
	for _, q := range quotes[1:] {
		if q.PriceCents < cheapest.PriceCents {
			cheapest = q
		}
		if fasterThan(q.DeliveryDays, fastest.DeliveryDays) {
			fastest = q
		}
		if q.AvgRating > best.AvgRating {
			best = q
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Comparing %d quote(s) for \"%s\":\n", len(quotes), rfqTitle)
	fmt.Fprintf(&b, "- Cheapest: %s at %s.\n", cheapest.SellerName, formatMoney(cheapest.PriceCents, cheapest.Currency))
	if fastest.DeliveryDays != nil {
		fmt.Fprintf(&b, "- Fastest delivery: %s in %d day(s).\n", fastest.SellerName, *fastest.DeliveryDays)
	}
	if best.ReviewCount > 0 {
		fmt.Fprintf(&b, "- Best-rated: %s at %.1f/5 (%d review(s)).\n", best.SellerName, best.AvgRating, best.ReviewCount)
	}
	return b.String()
}

// fasterThan reports whether a's delivery is strictly faster than b's. A nil
// delivery estimate never beats a known one.
func fasterThan(a, b *int32) bool {
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}
	return *a < *b
}

func formatMoney(cents int64, currency string) string {
	return fmt.Sprintf("%.2f %s", float64(cents)/100, currency)
}

// formatQuotesPrompt renders the quote list as plain text for the Haiku user
// turn.
func formatQuotesPrompt(rfqTitle string, quotes []QuoteSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "RFQ: %s\n\nQuotes:\n", rfqTitle)
	for _, q := range quotes {
		delivery := "unspecified"
		if q.DeliveryDays != nil {
			delivery = fmt.Sprintf("%d days", *q.DeliveryDays)
		}
		fmt.Fprintf(&b, "- %s: %s, delivery %s, rating %.1f/5 (%d reviews), verified=%v\n",
			q.SellerName, formatMoney(q.PriceCents, q.Currency), delivery, q.AvgRating, q.ReviewCount, q.Verified)
	}
	return b.String()
}

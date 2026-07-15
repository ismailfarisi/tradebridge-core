package rfq

import (
	"context"
	"regexp"
	"strings"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/attachments"
)

// Extractor turns a plain-text RFQ (optionally with attachments) into
// structured Requirements.
type Extractor interface {
	Extract(ctx context.Context, title, text string, atts []attachments.Loaded) (Requirements, error)
}

// ProviderConfig is a resolved LLM configuration for requirement extraction.
// Provider is "" (none → heuristic), "anthropic", or "openai_compatible".
type ProviderConfig struct {
	Provider         string
	Model            string
	BaseURL          string
	APIKey           string
	ExtractionPrompt string // overrides DefaultExtractionPrompt when non-empty
	IntakePrompt     string // overrides DefaultIntakePrompt when non-empty
}

// ExtractorResolver yields the extraction config for a given company, letting
// each tenant configure its own provider/model/key (with an env fallback).
type ExtractorResolver interface {
	ExtractionConfig(ctx context.Context, companyID string) (ProviderConfig, error)
}

// BuildExtractor picks an extractor from a resolved config. With no API key it
// falls back to the deterministic heuristic extractor, which keeps the RFQ
// engine fully functional (and testable) without any external dependency.
func BuildExtractor(cfg ProviderConfig, logger zerolog.Logger) Extractor {
	prompt := cfg.ExtractionPrompt
	if prompt == "" {
		prompt = DefaultExtractionPrompt
	}
	switch {
	case cfg.APIKey == "":
		return HeuristicExtractor{}
	case cfg.Provider == "openai_compatible":
		return newOpenAIExtractor(cfg.BaseURL, cfg.Model, cfg.APIKey, prompt, HeuristicExtractor{}, logger)
	default: // anthropic
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		return newClaudeExtractor(cfg.APIKey, model, prompt, HeuristicExtractor{}, logger)
	}
}

// HeuristicExtractor derives requirements with regexes and a category lexicon.
// It is intentionally simple: good enough to match sellers at pilot scale and
// to serve as the always-available fallback when Claude is unavailable.
type HeuristicExtractor struct{}

// categoryLexicon maps a canonical category to the terms that imply it.
var categoryLexicon = map[string][]string{
	"food & beverage":   {"food", "beverage", "snack", "kraft", "coffee", "tea", "juice", "grocery", "dairy"},
	"packaging":         {"packaging", "carton", "box", "boxes", "pallet", "pallets", "container", "wrap"},
	"electronics":       {"electronic", "electronics", "circuit", "pcb", "semiconductor", "cable", "sensor"},
	"apparel & textile": {"apparel", "textile", "fabric", "garment", "clothing", "cotton", "yarn"},
	"construction":      {"cement", "steel", "rebar", "construction", "lumber", "timber", "concrete"},
	"chemicals":         {"chemical", "chemicals", "polymer", "resin", "solvent", "adhesive"},
	"machinery":         {"machinery", "machine", "equipment", "pump", "motor", "cnc", "compressor"},
	"office supplies":   {"office", "stationery", "paper", "printer", "toner", "furniture"},
}

var (
	reQuantity = regexp.MustCompile(`(?i)\b(\d[\d,]*)\s*(units?|pcs|pieces|tons?|kg|kgs|kilograms?|boxes|cartons|pallets|liters?|litres?|metric tons?|mt)\b`)
	// The keyword prefix is case-insensitive, but the captured place name must
	// be genuinely capitalized — hence the (?-i:...) around the capture group.
	reLocation = regexp.MustCompile(`(?i)(?:deliver(?:ed|y)?\s+to|ship\s+to|located?\s+in|location[:\s]+|\bin)\s+((?-i:[A-Z][A-Za-z]+(?:\s+[A-Z][A-Za-z]+){0,2}))`)
	reDeadline = regexp.MustCompile(`(?i)(?:by|before|due|deadline[:\s]+|within)\s+([A-Za-z0-9,\s/-]{3,30}?)(?:\.|,|;|$)`)
	reWord     = regexp.MustCompile(`[A-Za-z][A-Za-z-]{2,}`)
)

// stopWords are common terms that carry no matching signal.
var stopWords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "need": {}, "want": {}, "looking": {},
	"please": {}, "quote": {}, "supplier": {}, "suppliers": {}, "our": {}, "your": {},
	"from": {}, "that": {}, "this": {}, "have": {}, "will": {}, "would": {}, "should": {},
	"deliver": {}, "delivery": {}, "ship": {}, "shipping": {}, "order": {}, "orders": {},
}

// Extract implements Extractor with no network calls. Attachments are
// ignored — the heuristic extractor works from text only.
func (HeuristicExtractor) Extract(_ context.Context, title, text string, _ []attachments.Loaded) (Requirements, error) {
	body := strings.TrimSpace(title + ". " + text)
	lower := strings.ToLower(body)

	req := Requirements{
		Category: classify(lower),
		Keywords: keywords(lower),
	}
	if m := reQuantity.FindStringSubmatch(body); m != nil {
		req.Quantity = strings.TrimSpace(m[0])
	}
	if m := reLocation.FindStringSubmatch(body); m != nil {
		req.Location = strings.TrimSpace(m[1])
	}
	if m := reDeadline.FindStringSubmatch(body); m != nil {
		req.Deadline = strings.TrimSpace(m[1])
	}
	return req, nil
}

// classify picks the category whose lexicon terms appear most often in text.
func classify(lower string) string {
	best, bestHits := "", 0
	for cat, terms := range categoryLexicon {
		hits := 0
		for _, t := range terms {
			if strings.Contains(lower, t) {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = cat, hits
		}
	}
	return best
}

// keywords returns up to eight distinct, meaningful terms from the text.
func keywords(lower string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, w := range reWord.FindAllString(lower, -1) {
		if _, stop := stopWords[w]; stop {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
		if len(out) == 8 {
			break
		}
	}
	return out
}

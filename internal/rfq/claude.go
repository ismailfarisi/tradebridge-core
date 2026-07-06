package rfq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rs/zerolog"
)

// claudeExtractor extracts requirements with Claude Haiku, falling back to the
// heuristic extractor on any API or parse error so RFQ creation never fails.
type claudeExtractor struct {
	client   anthropic.Client
	model    anthropic.Model
	fallback Extractor
	logger   zerolog.Logger
}

func newClaudeExtractor(apiKey, model string, fallback Extractor, logger zerolog.Logger) Extractor {
	return &claudeExtractor{
		client:   anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:    anthropic.Model(model),
		fallback: fallback,
		logger:   logger,
	}
}

const extractSystemPrompt = `You extract structured procurement requirements from a buyer's plain-text RFQ.
Respond with ONLY a JSON object, no prose, matching exactly this shape:
{"category":"","quantity":"","specs":[],"location":"","deadline":"","keywords":[]}
- category: a short product category (e.g. "packaging", "electronics", "food & beverage").
- quantity: the requested amount with its unit, if stated (e.g. "500 cartons").
- specs: notable specifications or constraints as short strings.
- location: delivery or buyer location, if stated.
- deadline: any delivery deadline or timeframe, if stated.
- keywords: 3-8 lowercase terms a supplier directory could be searched by.
Use empty string or empty array when a field is absent. Do not invent values.`

func (e *claudeExtractor) Extract(ctx context.Context, title, text string) (Requirements, error) {
	msg, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     e.model,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: extractSystemPrompt}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(
				fmt.Sprintf("Title: %s\n\nRFQ:\n%s", title, text),
			)),
		},
	})
	if err != nil {
		e.logger.Warn().Err(err).Msg("claude extraction failed; using heuristic fallback")
		return e.fallback.Extract(ctx, title, text)
	}

	var raw strings.Builder
	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			raw.WriteString(tb.Text)
		}
	}

	req, ok := parseRequirements(raw.String())
	if !ok {
		e.logger.Warn().Str("raw", raw.String()).Msg("claude returned unparseable JSON; using heuristic fallback")
		return e.fallback.Extract(ctx, title, text)
	}
	return req, nil
}

// parseRequirements pulls the first JSON object out of the model's reply and
// unmarshals it. Tolerant of surrounding text the model may add despite the
// instruction to return JSON only.
func parseRequirements(s string) (Requirements, bool) {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return Requirements{}, false
	}
	var req Requirements
	if err := json.Unmarshal([]byte(s[start:end+1]), &req); err != nil {
		return Requirements{}, false
	}
	return req, true
}

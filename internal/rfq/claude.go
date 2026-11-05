package rfq

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/attachments"
)

// claudeExtractor extracts requirements with Claude Haiku, falling back to the
// heuristic extractor on any API or parse error so RFQ creation never fails.
type claudeExtractor struct {
	client   anthropic.Client
	model    anthropic.Model
	system   string
	fallback Extractor
	logger   zerolog.Logger
}

func newClaudeExtractor(apiKey, model, system string, fallback Extractor, logger zerolog.Logger) Extractor {
	return &claudeExtractor{
		client:   anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:    anthropic.Model(model),
		system:   system,
		fallback: fallback,
		logger:   logger,
	}
}

// DefaultExtractionPrompt is the built-in extraction system prompt, used when
// the platform admin has not set a custom one.
const DefaultExtractionPrompt = `You extract structured procurement requirements from a buyer's plain-text RFQ.
Respond with ONLY a JSON object, no prose, matching exactly this shape:
{"category":"","quantity":"","specs":[],"location":"","deadline":"","keywords":[]}
- category: a short product category (e.g. "packaging", "electronics", "food & beverage").
- quantity: the requested amount with its unit, if stated (e.g. "500 cartons").
- specs: notable specifications or constraints as short strings.
- location: delivery or buyer location, if stated.
- deadline: any delivery deadline or timeframe, if stated.
- keywords: 3-8 lowercase terms a supplier directory could be searched by.
Use empty string or empty array when a field is absent. Do not invent values.`

func (e *claudeExtractor) Extract(ctx context.Context, title, text string, atts []attachments.Loaded) (Requirements, error) {
	msg, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     e.model,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: e.system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(userContent(
				fmt.Sprintf("Title: %s\n\nRFQ:\n%s", title, text), atts)...),
		},
	})
	if err != nil {
		e.logger.Warn().Err(err).Msg("claude extraction failed; using heuristic fallback")
		return e.fallback.Extract(ctx, title, text, atts)
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
		return e.fallback.Extract(ctx, title, text, atts)
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

// Completer is a one-shot text completion over the platform's configured LLM
// provider (the same client and settings RFQ intake uses). Other packages use
// it for short phrasing tasks, never for decisions.
type Completer interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// BuildCompleter returns a Completer for the resolved config, or nil when no
// API key is configured, so callers can treat "no LLM" as a plain branch.
func BuildCompleter(cfg ProviderConfig, logger zerolog.Logger) Completer {
	switch {
	case cfg.APIKey == "":
		return nil
	case cfg.Provider == "openai_compatible":
		return chatCompleter{chat: newOpenAIChat(cfg.BaseURL, cfg.Model, cfg.APIKey, logger)}
	default: // anthropic
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		return chatCompleter{chat: newAnthropicChat(cfg.APIKey, model, logger)}
	}
}

type chatCompleter struct{ chat llmChat }

func (c chatCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	return c.chat.complete(ctx, system, []ChatMessage{{Role: "user", Content: user}})
}

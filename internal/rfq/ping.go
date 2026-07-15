package rfq

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"
)

// PingProvider makes ONE minimal chat call against the resolved config to prove
// the provider + model + key actually work. Returns nil on a 200 reply, else a
// descriptive error. An empty APIKey is reported as an error: that config runs
// in heuristic mode, not against a model.
func PingProvider(ctx context.Context, cfg ProviderConfig, logger zerolog.Logger) error {
	if cfg.APIKey == "" {
		return errors.New("no API key configured")
	}
	var chat llmChat
	switch cfg.Provider {
	case "openai_compatible":
		chat = newOpenAIChat(cfg.BaseURL, cfg.Model, cfg.APIKey, logger)
	default: // anthropic
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		chat = newAnthropicChat(cfg.APIKey, model, logger)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := chat.complete(ctx, "Reply with the single word: ok", []ChatMessage{{Role: "user", Content: "ping"}})
	return err
}

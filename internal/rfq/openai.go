package rfq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// openAIExtractor calls any OpenAI-compatible /chat/completions endpoint (OpenAI,
// Gemini's OpenAI shim, local servers, …), so operators can point extraction at
// any model. Falls back to the heuristic extractor on any API/parse error.
type openAIExtractor struct {
	baseURL  string
	model    string
	apiKey   string
	client   *http.Client
	fallback Extractor
	logger   zerolog.Logger
}

func newOpenAIExtractor(baseURL, model, apiKey string, fallback Extractor, logger zerolog.Logger) Extractor {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return &openAIExtractor{
		baseURL:  baseURL,
		model:    model,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 30 * time.Second},
		fallback: fallback,
		logger:   logger,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (e *openAIExtractor) Extract(ctx context.Context, title, text string) (Requirements, error) {
	body, err := json.Marshal(chatRequest{
		Model: e.model,
		Messages: []chatMessage{
			{Role: "system", Content: extractSystemPrompt},
			{Role: "user", Content: fmt.Sprintf("Title: %s\n\nRFQ:\n%s", title, text)},
		},
	})
	if err != nil {
		return e.fallback.Extract(ctx, title, text)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return e.fallback.Extract(ctx, title, text)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req)
	if err != nil {
		e.logger.Warn().Err(err).Msg("openai extraction request failed; using heuristic fallback")
		return e.fallback.Extract(ctx, title, text)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		e.logger.Warn().Int("status", resp.StatusCode).Msg("openai extraction non-200; using heuristic fallback")
		return e.fallback.Extract(ctx, title, text)
	}

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Choices) == 0 {
		return e.fallback.Extract(ctx, title, text)
	}
	req2, ok := parseRequirements(out.Choices[0].Message.Content)
	if !ok {
		e.logger.Warn().Msg("openai returned unparseable JSON; using heuristic fallback")
		return e.fallback.Extract(ctx, title, text)
	}
	return req2, nil
}

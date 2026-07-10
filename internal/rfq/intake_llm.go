package rfq

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rs/zerolog"
)

// intakeSystemPrompt tells the model to return exactly one JSON decision. The
// service composes the user-facing confirmation on create, so on "create" the
// model only needs to supply a title + rfq_text.
const intakeSystemPrompt = `You are a procurement assistant helping a buyer post an RFQ (request for quote).
Given the conversation so far, decide whether you have enough to post the RFQ or need ONE more clarifying question.
Respond with ONLY a JSON object, no prose, matching exactly one of:
{"action":"ask","message":"<a single short clarifying question>"}
{"action":"create","title":"<short RFQ title>","rfq_text":"<one paragraph capturing what the buyer needs>"}
Prefer "create" once you know the product/material and a rough quantity. Ask at most one more question. If the buyer says to just post it, use "create".`

// llmDecision is the JSON the model returns.
type llmDecision struct {
	Action  string `json:"action"`
	Message string `json:"message"`
	Title   string `json:"title"`
	RFQText string `json:"rfq_text"`
}

// llmChat is a minimal chat-completion seam over a system prompt + the running
// conversation, returning the model's raw text.
type llmChat interface {
	complete(ctx context.Context, system string, msgs []ChatMessage) (string, error)
}

// llmIntake decides via the model, degrading to the heuristic on any API or
// parse error so RFQ chat never fails because a provider is unavailable.
type llmIntake struct {
	chat     llmChat
	fallback IntakeAgent
	logger   zerolog.Logger
}

func (l llmIntake) Decide(ctx context.Context, msgs []ChatMessage) (IntakeDecision, error) {
	raw, err := l.chat.complete(ctx, intakeSystemPrompt, msgs)
	if err != nil {
		l.logger.Warn().Err(err).Msg("intake LLM call failed; using heuristic")
		return l.fallback.Decide(ctx, msgs)
	}
	dec, ok := parseDecision(raw)
	if !ok {
		l.logger.Warn().Str("raw", raw).Msg("intake LLM returned unparseable JSON; using heuristic")
		return l.fallback.Decide(ctx, msgs)
	}
	if dec.Action == "create" {
		title := strings.TrimSpace(dec.Title)
		text := strings.TrimSpace(dec.RFQText)
		if text == "" {
			text = joinUser(msgs)
		}
		if title == "" {
			title = deriveTitle(text)
		}
		return IntakeDecision{Action: "create", Title: title, Text: text}, nil
	}
	// Any non-create decision is treated as ask; empty question → heuristic.
	q := strings.TrimSpace(dec.Message)
	if q == "" {
		return l.fallback.Decide(ctx, msgs)
	}
	return IntakeDecision{Action: "ask", Message: q}, nil
}

// parseDecision pulls the first JSON object out of the model reply.
func parseDecision(s string) (llmDecision, bool) {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return llmDecision{}, false
	}
	var d llmDecision
	if err := json.Unmarshal([]byte(s[start:end+1]), &d); err != nil {
		return llmDecision{}, false
	}
	if d.Action != "ask" && d.Action != "create" {
		return llmDecision{}, false
	}
	return d, true
}

// --- anthropic adapter ---

type anthropicChat struct {
	client anthropic.Client
	model  anthropic.Model
}

func newAnthropicChat(apiKey, model string, _ zerolog.Logger) llmChat {
	return anthropicChat{client: anthropic.NewClient(option.WithAPIKey(apiKey)), model: anthropic.Model(model)}
}

func (a anthropicChat) complete(ctx context.Context, system string, msgs []ChatMessage) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     a.model,
		MaxTokens: 512,
		System:    []anthropic.TextBlockParam{{Text: system}},
	}
	for _, m := range msgs {
		if m.Role == "assistant" {
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		} else {
			params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	// Anthropic requires the first message to be a user turn.
	if len(params.Messages) == 0 {
		params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock("")))
	}
	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(tb.Text)
		}
	}
	return b.String(), nil
}

// --- openai-compatible adapter (reuses chatMessage/chatRequest/chatResponse from openai.go) ---

type openAIChat struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

func newOpenAIChat(baseURL, model, apiKey string, _ zerolog.Logger) llmChat {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	return openAIChat{baseURL: baseURL, model: model, apiKey: apiKey, client: &http.Client{Timeout: 30 * time.Second}}
}

func (o openAIChat) complete(ctx context.Context, system string, msgs []ChatMessage) (string, error) {
	payload := chatRequest{Model: o.model, Messages: []chatMessage{{Role: "system", Content: system}}}
	for _, m := range msgs {
		role := "user"
		if m.Role == "assistant" {
			role = "assistant"
		}
		payload.Messages = append(payload.Messages, chatMessage{Role: role, Content: m.Content})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	resp, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", &httpStatusError{code: resp.StatusCode}
	}
	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || len(out.Choices) == 0 {
		return "", err
	}
	return out.Choices[0].Message.Content, nil
}

// httpStatusError carries a non-200 status so llmIntake falls back to heuristic.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return "openai chat non-200 status" }

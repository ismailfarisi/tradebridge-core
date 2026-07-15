package rfq

import (
	"context"
	"regexp"
	"strings"

	"github.com/rs/zerolog"
)

// intakeMaxTurns caps how many clarifying questions the assistant asks before
// creating the RFQ with whatever information is present.
const intakeMaxTurns = 2

// IntakeDecision is one step of RFQ intake: either ask another question or
// create the RFQ now. On create, Title/Text feed the existing CreateAndMatch.
type IntakeDecision struct {
	Action  string // "ask" | "create"
	Message string // the clarifying question (when Action == "ask")
	Title   string // RFQ title (when Action == "create")
	Text    string // RFQ raw text (when Action == "create")
}

// IntakeAgent turns the running conversation into the next IntakeDecision.
type IntakeAgent interface {
	Decide(ctx context.Context, msgs []ChatMessage) (IntakeDecision, error)
}

// IntakeStreamer is implemented by intake agents that can stream the visible
// "message" text while deciding. onDelta receives decoded message fragments;
// "create" decisions produce no deltas (their fields are internal).
type IntakeStreamer interface {
	DecideStream(ctx context.Context, msgs []ChatMessage, onDelta func(string)) (IntakeDecision, error)
}

// BuildIntake picks an intake agent from a resolved config, mirroring
// BuildExtractor. With no API key it uses the deterministic heuristic.
func BuildIntake(cfg ProviderConfig, logger zerolog.Logger) IntakeAgent {
	prompt := cfg.IntakePrompt
	if prompt == "" {
		prompt = DefaultIntakePrompt
	}
	switch {
	case cfg.APIKey == "":
		return HeuristicIntake{}
	case cfg.Provider == "openai_compatible":
		return llmIntake{chat: newOpenAIChat(cfg.BaseURL, cfg.Model, cfg.APIKey, logger), fallback: HeuristicIntake{}, logger: logger, system: prompt}
	default: // anthropic
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		return llmIntake{chat: newAnthropicChat(cfg.APIKey, model, logger), fallback: HeuristicIntake{}, logger: logger, system: prompt}
	}
}

// HeuristicIntake decides ask-vs-create with no network calls, reusing the
// heuristic extractor to detect the product/category and quantity slots.
type HeuristicIntake struct{}

var reExplicitCreate = regexp.MustCompile(`(?i)\b(just create|go ahead|create it|submit it|post it|that'?s all|that is all|no more)\b`)

const (
	askProduct  = "What product or material are you looking to source?"
	askQuantity = "About how many do you need, and where should it be delivered?"
)

// Decide implements IntakeAgent.
func (HeuristicIntake) Decide(ctx context.Context, msgs []ChatMessage) (IntakeDecision, error) {
	userText := joinUser(msgs)
	asked := countAssistant(msgs)

	if last := lastUser(msgs); reExplicitCreate.MatchString(last) {
		return createDecision(userText), nil
	}

	req, _ := HeuristicExtractor{}.Extract(ctx, "", userText, nil)
	// A recognized category is the real "product named" signal. Keywords are any
	// non-stopword token (almost always present), so they can't gate askProduct.
	hasProduct := req.Category != ""
	hasQty := req.Quantity != ""

	switch {
	case hasProduct && hasQty:
		return createDecision(userText), nil
	case asked >= intakeMaxTurns:
		return createDecision(userText), nil
	case !hasProduct:
		return IntakeDecision{Action: "ask", Message: askProduct}, nil
	default: // has product, missing quantity
		return IntakeDecision{Action: "ask", Message: askQuantity}, nil
	}
}

// createDecision builds a create decision from the accumulated user text,
// guaranteeing a non-empty Title and Text (both feed CreateAndMatch).
func createDecision(userText string) IntakeDecision {
	text := strings.TrimSpace(userText)
	title := deriveTitle(text)
	if text == "" {
		text = title // never emit an empty RFQ body
	}
	return IntakeDecision{Action: "create", Title: title, Text: text}
}

// deriveTitle makes a short RFQ title from the conversation (first line, capped),
// falling back to a generic label so CreateAndMatch never sees an empty title.
func deriveTitle(userText string) string {
	line := userText
	if i := strings.IndexAny(line, ".\n"); i > 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "RFQ"
	}
	const max = 60
	if len(line) > max {
		line = strings.TrimSpace(line[:max])
	}
	return line
}

func joinUser(msgs []ChatMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "user" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(m.Content)
		}
	}
	return strings.TrimSpace(b.String())
}

func countAssistant(msgs []ChatMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role == "assistant" {
			n++
		}
	}
	return n
}

func lastUser(msgs []ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

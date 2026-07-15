package rfq

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
)

func TestLLMIntake_RecordsFallbackOnChatError(t *testing.T) {
	h := NewHealthTracker(20)
	li := llmIntake{chat: fakeChat{err: errors.New("boom")}, fallback: HeuristicIntake{}, logger: zerolog.Nop(), health: h}
	_, _ = li.Decide(context.Background(), []ChatMessage{{Role: "user", Content: "paper bags"}})
	if !h.Snapshot().Degraded {
		t.Fatalf("chat error should record a fallback")
	}
}

func TestLLMIntake_RecordsSuccessOnValidDecision(t *testing.T) {
	h := NewHealthTracker(20)
	li := llmIntake{chat: fakeChat{reply: `{"action":"create","title":"Paper bags","rfq_text":"100 paper bags"}`}, fallback: HeuristicIntake{}, logger: zerolog.Nop(), health: h}
	dec, _ := li.Decide(context.Background(), []ChatMessage{{Role: "user", Content: "100 paper bags"}})
	if dec.Action != "create" {
		t.Fatalf("expected create, got %q", dec.Action)
	}
	if h.Snapshot().Degraded {
		t.Fatalf("valid decision should record success, not fallback")
	}
}

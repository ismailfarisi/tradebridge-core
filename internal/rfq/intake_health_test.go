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
	// Seed a fallback first so Degraded starts true; only a genuine
	// RecordSuccess() call after Decide can flip it back to false. This
	// rules out the tautological pass where Degraded is false merely
	// because nothing was ever recorded.
	h.RecordFallback(errors.New("prior"))
	if !h.Snapshot().Degraded {
		t.Fatalf("setup: expected seeded fallback to leave tracker degraded")
	}
	li := llmIntake{chat: fakeChat{reply: `{"action":"create","title":"Paper bags","rfq_text":"100 paper bags"}`}, fallback: HeuristicIntake{}, logger: zerolog.Nop(), health: h}
	dec, _ := li.Decide(context.Background(), []ChatMessage{{Role: "user", Content: "100 paper bags"}})
	if dec.Action != "create" {
		t.Fatalf("expected create, got %q", dec.Action)
	}
	if h.Snapshot().Degraded {
		t.Fatalf("valid create decision should record success, clearing the seeded degraded state")
	}
}

func TestLLMIntake_RecordsSuccessOnValidAsk(t *testing.T) {
	h := NewHealthTracker(20)
	// Same seeding trick as above: prove RecordSuccess() actually ran on
	// the ask path, not just that nothing was recorded.
	h.RecordFallback(errors.New("prior"))
	if !h.Snapshot().Degraded {
		t.Fatalf("setup: expected seeded fallback to leave tracker degraded")
	}
	li := llmIntake{chat: fakeChat{reply: `{"action":"ask","message":"How many do you need?"}`}, fallback: HeuristicIntake{}, logger: zerolog.Nop(), health: h}
	dec, _ := li.Decide(context.Background(), []ChatMessage{{Role: "user", Content: "paper bags"}})
	if dec.Action != "ask" {
		t.Fatalf("expected ask, got %q", dec.Action)
	}
	if h.Snapshot().Degraded {
		t.Fatalf("valid ask decision should record success, clearing the seeded degraded state")
	}
}

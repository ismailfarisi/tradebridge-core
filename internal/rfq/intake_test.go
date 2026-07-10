package rfq

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestHeuristicIntake_AsksForProductThenQuantityThenCreates(t *testing.T) {
	h := HeuristicIntake{}
	ctx := context.Background()

	// Turn 1: vague opener with no product/qty → ask for product.
	d, err := h.Decide(ctx, []ChatMessage{{Role: "user", Content: "hi I want to buy something"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "ask" {
		t.Fatalf("turn1 action = %q, want ask", d.Action)
	}
	if d.Message != askProduct {
		t.Fatalf("turn1 message = %q, want askProduct %q", d.Message, askProduct)
	}

	// Turn 2: product now known, quantity still missing → ask for quantity.
	d, err = h.Decide(ctx, []ChatMessage{
		{Role: "user", Content: "hi I want to buy something"},
		{Role: "assistant", Content: d.Message},
		{Role: "user", Content: "cartons for packaging"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "ask" {
		t.Fatalf("turn2 action = %q, want ask", d.Action)
	}
	if d.Message != askQuantity {
		t.Fatalf("turn2 message = %q, want askQuantity %q", d.Message, askQuantity)
	}

	// Turn 3: product + quantity present → create.
	d, err = h.Decide(ctx, []ChatMessage{
		{Role: "user", Content: "hi I want to buy something"},
		{Role: "assistant", Content: "q1"},
		{Role: "user", Content: "cartons for packaging"},
		{Role: "assistant", Content: "q2"},
		{Role: "user", Content: "500 cartons delivered to Dubai"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" {
		t.Fatalf("turn3 action = %q, want create", d.Action)
	}
	if d.Title == "" || d.Text == "" {
		t.Fatalf("create decision needs title+text, got title=%q text=%q", d.Title, d.Text)
	}
	if !strings.Contains(d.Text, "cartons") {
		t.Fatalf("text should carry the conversation, got %q", d.Text)
	}
}

func TestHeuristicIntake_ExplicitCreateShortCircuits(t *testing.T) {
	d, err := HeuristicIntake{}.Decide(context.Background(), []ChatMessage{
		{Role: "user", Content: "widgets"},
		{Role: "assistant", Content: "how many?"},
		{Role: "user", Content: "just create it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" {
		t.Fatalf("action = %q, want create on explicit request", d.Action)
	}
}

func TestHeuristicIntake_CapsAtMaxTurns(t *testing.T) {
	// Two assistant questions already asked; still nothing extractable → create anyway.
	d, err := HeuristicIntake{}.Decide(context.Background(), []ChatMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "q1"},
		{Role: "user", Content: "still thinking"},
		{Role: "assistant", Content: "q2"},
		{Role: "user", Content: "maybe some stuff"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" {
		t.Fatalf("action = %q, want create at turn cap", d.Action)
	}
}

type fakeChat struct {
	reply string
	err   error
}

func (f fakeChat) complete(_ context.Context, _ string, _ []ChatMessage) (string, error) {
	return f.reply, f.err
}

func newTestLLMIntake(reply string, err error) llmIntake {
	return llmIntake{chat: fakeChat{reply: reply, err: err}, fallback: HeuristicIntake{}, logger: zerolog.Nop()}
}

func TestLLMIntake_ParsesAsk(t *testing.T) {
	d, err := newTestLLMIntake(`{"action":"ask","message":"What's the target price?"}`, nil).
		Decide(context.Background(), []ChatMessage{{Role: "user", Content: "cartons"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "ask" || d.Message != "What's the target price?" {
		t.Fatalf("got %+v", d)
	}
}

func TestLLMIntake_ParsesCreate(t *testing.T) {
	d, err := newTestLLMIntake(`{"action":"create","title":"Cartons","rfq_text":"500 cartons to Dubai"}`, nil).
		Decide(context.Background(), []ChatMessage{{Role: "user", Content: "500 cartons to Dubai"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" || d.Title != "Cartons" || d.Text != "500 cartons to Dubai" {
		t.Fatalf("got %+v", d)
	}
}

func TestLLMIntake_FallsBackOnError(t *testing.T) {
	// Provider error → heuristic decides (product+qty present → create).
	d, err := newTestLLMIntake("", context.DeadlineExceeded).
		Decide(context.Background(), []ChatMessage{{Role: "user", Content: "500 cartons for packaging to Dubai"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "create" {
		t.Fatalf("expected heuristic create on fallback, got %+v", d)
	}
}

func TestLLMIntake_FallsBackOnGarbage(t *testing.T) {
	d, err := newTestLLMIntake("not json at all", nil).
		Decide(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != "ask" { // heuristic asks for product on a vague opener
		t.Fatalf("expected heuristic ask on garbage, got %+v", d)
	}
}

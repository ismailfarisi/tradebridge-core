package rfq

import (
	"context"
	"strings"
	"testing"
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

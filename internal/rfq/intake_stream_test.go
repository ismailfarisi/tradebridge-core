package rfq

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// chunkedChat streams `text` in 3-byte chunks; complete returns it whole.
type chunkedChat struct {
	text string
	err  error
}

func (f chunkedChat) complete(context.Context, string, []ChatMessage) (string, error) {
	return f.text, f.err
}

func (f chunkedChat) completeStream(_ context.Context, _ string, _ []ChatMessage, onDelta func(string)) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	for i := 0; i < len(f.text); i += 3 {
		end := i + 3
		if end > len(f.text) {
			end = len(f.text)
		}
		onDelta(f.text[i:end])
	}
	return f.text, nil
}

func TestLLMIntake_DecideStream_AskStreamsMessage(t *testing.T) {
	l := llmIntake{
		chat:     chunkedChat{text: `{"action":"ask","message":"How many units?"}`},
		fallback: HeuristicIntake{},
		logger:   zerolog.Nop(),
		system:   DefaultIntakePrompt,
	}
	var deltas strings.Builder
	dec, err := l.DecideStream(context.Background(),
		[]ChatMessage{{Role: "user", Content: "steel pipes"}},
		func(s string) { deltas.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != "ask" || dec.Message != "How many units?" {
		t.Fatalf("dec = %+v", dec)
	}
	if deltas.String() != "How many units?" {
		t.Fatalf("deltas = %q", deltas.String())
	}
}

func TestLLMIntake_DecideStream_CreateEmitsNoDeltas(t *testing.T) {
	l := llmIntake{
		chat:     chunkedChat{text: `{"action":"create","title":"Steel pipes","rfq_text":"500 steel pipes to Dubai"}`},
		fallback: HeuristicIntake{},
		logger:   zerolog.Nop(),
		system:   DefaultIntakePrompt,
	}
	var deltas strings.Builder
	dec, err := l.DecideStream(context.Background(),
		[]ChatMessage{{Role: "user", Content: "500 steel pipes to Dubai"}},
		func(s string) { deltas.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != "create" || dec.Title != "Steel pipes" {
		t.Fatalf("dec = %+v", dec)
	}
	if deltas.String() != "" {
		t.Fatalf("deltas = %q, want empty (create fields are internal)", deltas.String())
	}
}

func TestLLMIntake_DecideStream_ErrorFallsBackToHeuristic(t *testing.T) {
	l := llmIntake{
		chat:     chunkedChat{err: errors.New("boom")},
		fallback: HeuristicIntake{},
		logger:   zerolog.Nop(),
		system:   DefaultIntakePrompt,
	}
	dec, err := l.DecideStream(context.Background(),
		[]ChatMessage{{Role: "user", Content: "hi I want to buy something"}},
		func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != "ask" || dec.Message != askProduct {
		t.Fatalf("dec = %+v, want heuristic askProduct", dec)
	}
}

func TestLLMIntake_StreamAndNonStreamEquivalent(t *testing.T) {
	raw := `{"action":"ask","message":"What is the delivery city?"}`
	l := llmIntake{chat: chunkedChat{text: raw}, fallback: HeuristicIntake{}, logger: zerolog.Nop(), system: DefaultIntakePrompt}
	msgs := []ChatMessage{{Role: "user", Content: "bolts"}}
	d1, err1 := l.Decide(context.Background(), msgs)
	d2, err2 := l.DecideStream(context.Background(), msgs, func(string) {})
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if d1 != d2 {
		t.Fatalf("decisions differ: %+v vs %+v", d1, d2)
	}
}

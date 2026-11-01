package negotiation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/rfq"
)

func samplePhraseInput() PhraseInput {
	return PhraseInput{
		Tone: "collaborative", Currency: "AED", SellerPriceCents: 110_000, OfferCents: 92_600,
		DeliveryDays: i32(14), TermsDays: i32(30), Round: 2, Competitors: 1,
		TradeOff: &TradeOff{Issue: IssueTerms, From: 60, To: 30},
		Template: "Thanks for the revised quote. We can commit today at AED 926 with delivery within 14 days and payment within 30 days.",
	}
}

func TestVerifyMessage(t *testing.T) {
	in := samplePhraseInput()
	ok := []string{
		"Hi, I'm the buyer's AI purchasing agent. We can commit today at AED 926, delivery within 14 days, and we'll pay in 30 days rather than 60.",
		"We're weighing 1 other offer, but we'd like to go with you: AED 926.00 with delivery in 14 days on net 30 terms (instead of net 60).",
		"Thanks! We can do AED 926 with delivery in 14 days and pay net 30 days.",
		in.Template,
	}
	for _, s := range ok {
		if err := VerifyMessage(s, in); err != nil {
			t.Errorf("should pass: %q: %v", s, err)
		}
	}
	bad := map[string]string{
		"wrong price":       "We can commit today at AED 950 with delivery within 14 days and payment within 30 days.",
		"other money":       "AED 926 with delivery in 14 days and net 30 — that's AED 174 below your ask.",
		"leaks their price": "You asked AED 1,100; we can do AED 926 with delivery in 14 days and net 30.",
		"invented figure":   "We can do AED 926 with delivery in 14 days, net 30, and 5% more volume next quarter.",
		"missing terms":     "We can do AED 926 with delivery in 14 days.",
		"missing delivery":  "We can do AED 926 paying net 30.",
		"missing price":     "We can deliver within 14 days and pay net 30.",
		"false final":       "This is our final offer: AED 926, delivery in 14 days, net 30.",
		"empty":             "",
		"dollar amount":     "AED 926, delivery in 14 days, net 30, plus $30 for freight.",
	}
	for name, s := range bad {
		if err := VerifyMessage(s, in); err == nil {
			t.Errorf("%s should be rejected: %q", name, s)
		}
	}
	in.Final = true
	if err := VerifyMessage("This is our final offer: AED 926, delivery in 14 days, net 30.", in); err != nil {
		t.Errorf("final is allowed on the last round: %v", err)
	}
	cash := samplePhraseInput()
	cash.TermsDays, cash.TradeOff = i32(0), nil
	if err := VerifyMessage("AED 926, delivery within 14 days, payment in cash on delivery.", cash); err != nil {
		t.Errorf("cash terms: %v", err)
	}
	if err := VerifyMessage("AED 926, delivery within 14 days.", cash); err == nil {
		t.Error("cash terms must be stated")
	}
}

type fakePhraser struct {
	text  string
	err   error
	block bool
	calls int
	got   PhraseInput
}

func (f *fakePhraser) Phrase(ctx context.Context, in PhraseInput) (string, error) {
	f.calls++
	f.got = in
	if f.block {
		<-ctx.Done()
		return "", ctx.Err()
	}
	return f.text, f.err
}

func TestEnginePhrase_UsesVerifiedLLMTextElseTemplate(t *testing.T) {
	in := samplePhraseInput()
	e := &Engine{logger: zerolog.Nop()}

	if msg, src := e.phrase(context.Background(), in); src != MessageTemplate || msg != in.Template {
		t.Fatalf("no phraser: want template, got %s %q", src, msg)
	}

	good := "I'm the buyer's AI purchasing agent. We can commit today at AED 926 with delivery within 14 days, paying net 30 instead of 60."
	fp := &fakePhraser{text: "  \"" + good + "\"  "}
	e.SetPhraser(fp, time.Second)
	if msg, src := e.phrase(context.Background(), in); src != MessageLLM || msg != good {
		t.Fatalf("verified text: got %s %q", src, msg)
	}
	if fp.got.OfferCents != in.OfferCents || fp.got.TradeOff == nil {
		t.Fatalf("phraser must receive the decided move: %+v", fp.got)
	}

	for name, f := range map[string]*fakePhraser{
		"mismatch": {text: "We can commit today at AED 999 with delivery within 14 days and net 30."},
		"error":    {err: errors.New("boom")},
		"timeout":  {block: true},
	} {
		e.SetPhraser(f, 20*time.Millisecond)
		start := time.Now()
		msg, src := e.phrase(context.Background(), in)
		if src != MessageTemplate || msg != in.Template {
			t.Fatalf("%s: want template fallback, got %s %q", name, src, msg)
		}
		if time.Since(start) > time.Second {
			t.Fatalf("%s: fallback took too long", name)
		}
	}
}

type staticResolver struct{ cfg rfq.ProviderConfig }

func (s staticResolver) ExtractionConfig(context.Context, string) (rfq.ProviderConfig, error) {
	return s.cfg, nil
}

type fakeCompleter struct{ system, user string }

func (f *fakeCompleter) Complete(_ context.Context, system, user string) (string, error) {
	f.system, f.user = system, user
	return "ok", nil
}

func TestLLMPhraser_DisabledWithoutKeyAndPassesOnlyTheMove(t *testing.T) {
	p := NewLLMPhraser(staticResolver{}, zerolog.Nop())
	if _, err := p.Phrase(context.Background(), samplePhraseInput()); !errors.Is(err, ErrNoLLM) {
		t.Fatalf("no key: want ErrNoLLM, got %v", err)
	}
	fc := &fakeCompleter{}
	p = NewLLMPhraser(staticResolver{cfg: rfq.ProviderConfig{APIKey: "k"}}, zerolog.Nop())
	p.build = func(rfq.ProviderConfig, zerolog.Logger) rfq.Completer { return fc }
	if _, err := p.Phrase(context.Background(), samplePhraseInput()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"price_text": "AED 926"`, `"payment_terms": "net 30 days"`, `"delivery_within_days": 14`, `"from": "net 60 days"`} {
		if !strings.Contains(fc.user, want) {
			t.Fatalf("prompt missing %s:\n%s", want, fc.user)
		}
	}
	if strings.Contains(fc.user, "1,100") {
		t.Fatal("the supplier's price must not be handed to the model")
	}
	if !strings.Contains(fc.system, "AI negotiation agent") {
		t.Fatal("system prompt should require AI disclosure")
	}
}

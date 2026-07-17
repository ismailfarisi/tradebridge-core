package thread

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
)

func TestChannel(t *testing.T) {
	if got := channel("q1"); got != "thread:q1" {
		t.Fatalf("channel(q1) = %q, want thread:q1", got)
	}
}

// TestPublisher_NilRedisIsNoop verifies that a Publisher built with a nil
// Redis client never panics and simply drops the publish — Redis-backed
// delivery is covered by the E2E environment, not here.
func TestPublisher_NilRedisIsNoop(t *testing.T) {
	p := NewPublisher(nil, zerolog.Nop())
	p.PublishMessage(context.Background(), "q1", []byte(`{"id":"m1"}`))
	p.PublishVersion(context.Background(), "q1", []byte(`{"id":"v1"}`))
}

// TestPublisher_NilPublisherIsNoop verifies a nil *Publisher (a service that
// never had one wired, e.g. some unit tests) is also safe to call.
func TestPublisher_NilPublisherIsNoop(t *testing.T) {
	var p *Publisher
	p.PublishMessage(context.Background(), "q1", []byte(`{"id":"m1"}`))
	p.PublishVersion(context.Background(), "q1", []byte(`{"id":"v1"}`))
}

// TestEnvelope_MarshalShape asserts the envelope carries the right kind and
// omits the field that doesn't apply.
func TestEnvelope_MarshalShape(t *testing.T) {
	env := Envelope{Kind: "message", QuoteID: "q1", Message: json.RawMessage(`{"id":"m1"}`)}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["kind"] != "message" {
		t.Fatalf("kind = %v, want message", m["kind"])
	}
	if m["quote_id"] != "q1" {
		t.Fatalf("quote_id = %v, want q1", m["quote_id"])
	}
	if _, ok := m["version"]; ok {
		t.Fatalf("version should be omitted for a message envelope, got %v", m["version"])
	}

	verEnv := Envelope{Kind: "version", QuoteID: "q1", Version: json.RawMessage(`{"id":"v1"}`)}
	vb, err := json.Marshal(verEnv)
	if err != nil {
		t.Fatal(err)
	}
	var vm map[string]any
	if err := json.Unmarshal(vb, &vm); err != nil {
		t.Fatal(err)
	}
	if vm["kind"] != "version" {
		t.Fatalf("kind = %v, want version", vm["kind"])
	}
	if _, ok := vm["message"]; ok {
		t.Fatalf("message should be omitted for a version envelope, got %v", vm["message"])
	}
}

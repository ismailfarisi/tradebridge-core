package rfq

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/rs/zerolog"
)

// sseHandler writes pre-canned SSE lines with flushes between events.
func sseHandler(t *testing.T, events []string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("no flusher")
		}
		for _, ev := range events {
			fmt.Fprint(w, ev)
			fl.Flush()
		}
	}
}

func TestOpenAIChat_CompleteStream_DashScopeFormat(t *testing.T) {
	events := []string{
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"action\\\":\\\"ask\\\",\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"\\\"message\\\":\\\"Hi there\\\"}\"}}]}\n\n",
		"data: [DONE]\n\n",
	}
	srv := httptest.NewServer(sseHandler(t, events))
	defer srv.Close()

	chat := newOpenAIChat(srv.URL, "qwen-plus", "test-key", zerolog.Nop())
	var got []string
	full, err := chat.completeStream(context.Background(), "sys",
		[]ChatMessage{{Role: "user", Content: "hello"}},
		func(s string) { got = append(got, s) })
	if err != nil {
		t.Fatal(err)
	}
	want := `{"action":"ask","message":"Hi there"}`
	if full != want {
		t.Fatalf("full = %q, want %q", full, want)
	}
	if strings.Join(got, "") != want {
		t.Fatalf("deltas joined = %q, want %q", strings.Join(got, ""), want)
	}
	if len(got) < 2 {
		t.Fatalf("expected multiple deltas, got %d", len(got))
	}
}

func TestOpenAIChat_CompleteStream_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	chat := newOpenAIChat(srv.URL, "m", "k", zerolog.Nop())
	_, err := chat.completeStream(context.Background(), "sys", []ChatMessage{{Role: "user", Content: "x"}}, func(string) {})
	if err == nil {
		t.Fatal("want error on non-200")
	}
}

func TestAnthropicChat_CompleteStream(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-haiku-4-5\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":5,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"world\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":3}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
	srv := httptest.NewServer(sseHandler(t, events))
	defer srv.Close()

	chat := newAnthropicChat("test-key", "claude-haiku-4-5", zerolog.Nop(), option.WithBaseURL(srv.URL))
	var got []string
	full, err := chat.completeStream(context.Background(), "sys",
		[]ChatMessage{{Role: "user", Content: "hi"}},
		func(s string) { got = append(got, s) })
	if err != nil {
		t.Fatal(err)
	}
	if full != "Hello world" {
		t.Fatalf("full = %q", full)
	}
	if strings.Join(got, "") != "Hello world" {
		t.Fatalf("deltas = %v", got)
	}
}

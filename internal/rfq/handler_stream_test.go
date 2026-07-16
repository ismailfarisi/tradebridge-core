package rfq

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

// heuristicResolver returns an empty config → BuildIntake yields HeuristicIntake.
type heuristicResolver struct{}

func (heuristicResolver) ExtractionConfig(context.Context, string) (ProviderConfig, error) {
	return ProviderConfig{}, nil
}

func TestChatStreamHandler_HeuristicAskSSE(t *testing.T) {
	app := fiber.New()
	svc := NewService(nil, nil, heuristicResolver{}, nil, zerolog.Nop(), 0, nil)
	NewHandler(svc, nil).Register(app, func(c *fiber.Ctx) error { return c.Next() })

	req := httptest.NewRequest("POST", "/v1/rfq-chat/stream",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi I want to buy something"}]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "event: delta") {
		t.Fatalf("missing delta event in %q", s)
	}
	if !strings.Contains(s, askProduct) {
		t.Fatalf("missing heuristic question in %q", s)
	}
	if !strings.Contains(s, "event: done") || !strings.Contains(s, `"done":false`) {
		t.Fatalf("missing done event in %q", s)
	}
	// delta must precede done
	if strings.Index(s, "event: delta") > strings.Index(s, "event: done") {
		t.Fatalf("delta after done: %q", s)
	}
}

func TestChatStreamHandler_EmptyMessages400(t *testing.T) {
	app := fiber.New()
	svc := NewService(nil, nil, heuristicResolver{}, nil, zerolog.Nop(), 0, nil)
	NewHandler(svc, nil).Register(app, func(c *fiber.Ctx) error { return c.Next() })

	req := httptest.NewRequest("POST", "/v1/rfq-chat/stream", strings.NewReader(`{"messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

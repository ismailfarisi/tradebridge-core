# LLM Connection Test + Fallback Health Banner — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give platform admins a one-click "Test connection" for the saved LLM provider config, and a banner that surfaces when RFQ intake has silently fallen back to the scripted heuristic.

**Architecture:** Backend adds a thread-safe in-memory `HealthTracker` (ring of the last 20 intake outcomes) recorded inside the provider-agnostic `llmIntake`, plus a `PingProvider` one-shot call. A shared tracker instance is created in `router.go` and injected into both `rfq.Service` (records) and `settings.Service` (reads + tests). Two new admin routes expose test + health; the admin web page adds a Test button and a degraded banner.

**Tech Stack:** Go (Fiber, zerolog), Next.js/React (TypeScript), vitest (web).

## Global Constraints

- Health tracking is **intake only** — extraction is not tracked (shares the same config; "Test connection" proves the config). Do NOT modify `BuildExtractor`, `openai.go`, or `claude.go` for health.
- A **nil `*HealthTracker` must be a valid no-op** (all methods guard `h == nil`) so existing tests/callers that pass `nil` don't panic.
- Window size is fixed at **20**; no config knob, no persistence, no per-company tracking (YAGNI).
- `degraded` = the single most recent recorded outcome was a fallback (one success clears it).
- The pure-heuristic agent (returned when `APIKey == ""`) is **never** recorded — that "not configured" state is shown by `has_key: false`.
- `ctx.Err()` (caller-went-away) branches must **not** record a fallback.
- New backend routes live under the existing platform-admin guard group `/admin/v1/settings`.

---

### Task 1: HealthTracker (ring buffer + snapshot)

**Files:**
- Create: `internal/rfq/health.go`
- Test: `internal/rfq/health_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type HealthSnapshot struct { Window int; RecentFallbacks int; LastError string; LastFallbackAt *time.Time; Degraded bool }` (JSON tags: `window`, `recent_fallbacks`, `last_error`, `last_fallback_at`, `degraded`)
  - `func NewHealthTracker(window int) *HealthTracker`
  - `func (h *HealthTracker) RecordSuccess()`
  - `func (h *HealthTracker) RecordFallback(err error)`
  - `func (h *HealthTracker) Snapshot() HealthSnapshot`

- [ ] **Step 1: Write the failing test**

```go
package rfq

import (
	"errors"
	"sync"
	"testing"
)

func TestHealthTracker_EmptyIsHealthy(t *testing.T) {
	h := NewHealthTracker(20)
	s := h.Snapshot()
	if s.Degraded || s.RecentFallbacks != 0 || s.Window != 20 {
		t.Fatalf("empty tracker not healthy: %+v", s)
	}
}

func TestHealthTracker_FallbackThenSuccessClearsDegraded(t *testing.T) {
	h := NewHealthTracker(20)
	h.RecordFallback(errors.New("boom"))
	s := h.Snapshot()
	if !s.Degraded || s.RecentFallbacks != 1 || s.LastError != "boom" || s.LastFallbackAt == nil {
		t.Fatalf("after fallback: %+v", s)
	}
	h.RecordSuccess()
	s = h.Snapshot()
	if s.Degraded { // last outcome was success
		t.Fatalf("success should clear degraded: %+v", s)
	}
	if s.RecentFallbacks != 1 { // fallback still in the window
		t.Fatalf("fallback should remain in window: %+v", s)
	}
}

func TestHealthTracker_RingEvictsOldOutcomes(t *testing.T) {
	h := NewHealthTracker(3)
	h.RecordFallback(errors.New("old"))
	h.RecordSuccess()
	h.RecordSuccess()
	h.RecordSuccess() // evicts the fallback
	s := h.Snapshot()
	if s.RecentFallbacks != 0 {
		t.Fatalf("old fallback should be evicted: %+v", s)
	}
}

func TestHealthTracker_NilIsNoop(t *testing.T) {
	var h *HealthTracker
	h.RecordSuccess()
	h.RecordFallback(errors.New("x"))
	if s := h.Snapshot(); s.Degraded {
		t.Fatalf("nil tracker should be healthy no-op: %+v", s)
	}
}

func TestHealthTracker_ConcurrentAccess(t *testing.T) {
	h := NewHealthTracker(20)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				h.RecordSuccess()
			} else {
				h.RecordFallback(errors.New("e"))
			}
			_ = h.Snapshot()
		}(i)
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rfq/ -run TestHealthTracker -v`
Expected: FAIL — `undefined: NewHealthTracker` (compile error).

- [ ] **Step 3: Write minimal implementation**

Create `internal/rfq/health.go`:

```go
package rfq

import (
	"sync"
	"time"
)

// HealthSnapshot is a point-in-time view of recent LLM intake outcomes.
type HealthSnapshot struct {
	Window          int        `json:"window"`
	RecentFallbacks int        `json:"recent_fallbacks"`
	LastError       string     `json:"last_error"`
	LastFallbackAt  *time.Time `json:"last_fallback_at"`
	Degraded        bool       `json:"degraded"`
}

// HealthTracker records the last N intake outcomes (LLM success vs heuristic
// fallback) in a ring, so the admin UI can surface silent degradation. Safe for
// concurrent use. A nil *HealthTracker is a valid no-op recorder.
type HealthTracker struct {
	mu       sync.Mutex
	window   int
	outcomes []bool // true = fell back to heuristic
	lastErr  string
	lastAt   *time.Time
}

// NewHealthTracker returns a tracker over the last `window` outcomes (min 1,
// defaults to 20 for non-positive input).
func NewHealthTracker(window int) *HealthTracker {
	if window <= 0 {
		window = 20
	}
	return &HealthTracker{window: window}
}

// RecordSuccess notes one successful model-driven decision.
func (h *HealthTracker) RecordSuccess() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.push(false)
}

// RecordFallback notes one degradation to the heuristic, keeping the error text.
func (h *HealthTracker) RecordFallback(err error) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.push(true)
	if err != nil {
		msg := err.Error()
		const max = 300
		if len(msg) > max {
			msg = msg[:max]
		}
		h.lastErr = msg
	}
	t := time.Now()
	h.lastAt = &t
}

// push appends an outcome, evicting the oldest when the window is full.
// The caller holds the lock.
func (h *HealthTracker) push(fellBack bool) {
	h.outcomes = append(h.outcomes, fellBack)
	if len(h.outcomes) > h.window {
		h.outcomes = h.outcomes[len(h.outcomes)-h.window:]
	}
}

// Snapshot returns the current view. Degraded means the most recent outcome was
// a fallback; a single subsequent success clears it.
func (h *HealthTracker) Snapshot() HealthSnapshot {
	if h == nil {
		return HealthSnapshot{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	recent := 0
	for _, fb := range h.outcomes {
		if fb {
			recent++
		}
	}
	degraded := len(h.outcomes) > 0 && h.outcomes[len(h.outcomes)-1]
	var lastAt *time.Time
	if h.lastAt != nil {
		t := *h.lastAt
		lastAt = &t
	}
	return HealthSnapshot{
		Window:          h.window,
		RecentFallbacks: recent,
		LastError:       h.lastErr,
		LastFallbackAt:  lastAt,
		Degraded:        degraded,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rfq/ -run TestHealthTracker -race -v`
Expected: PASS (all 5).

- [ ] **Step 5: Commit**

```bash
git add internal/rfq/health.go internal/rfq/health_test.go
git commit -m "feat(rfq): add in-memory HealthTracker for intake fallback"
```

---

### Task 2: httpStatusError carries status code + body snippet

**Files:**
- Modify: `internal/rfq/intake_llm.go` (type at ~323-325; non-200 sites in `openAIChat.complete` ~243 and `completeStream` ~291; imports)
- Test: `internal/rfq/intake_llm_status_test.go` (create)

**Interfaces:**
- Consumes: nothing new.
- Produces: `httpStatusError{code int; snippet string}` with `Error()` → `"openai chat <code>: <snippet>"` when snippet present. (Unexported — consumed only within package + surfaced via error strings.)

- [ ] **Step 1: Write the failing test**

Create `internal/rfq/intake_llm_status_test.go`:

```go
package rfq

import (
	"strings"
	"testing"
)

func TestHTTPStatusError_IncludesCodeAndSnippet(t *testing.T) {
	e := &httpStatusError{code: 404, snippet: `{"error":{"message":"Model not exist."}}`}
	got := e.Error()
	if !strings.Contains(got, "404") || !strings.Contains(got, "Model not exist.") {
		t.Fatalf("error missing code/snippet: %q", got)
	}
}

func TestHTTPStatusError_NoSnippetStillHasCode(t *testing.T) {
	e := &httpStatusError{code: 500}
	if !strings.Contains(e.Error(), "500") {
		t.Fatalf("error missing code: %q", e.Error())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rfq/ -run TestHTTPStatusError -v`
Expected: FAIL — `unknown field 'snippet'` (compile error).

- [ ] **Step 3: Implement the change**

In `internal/rfq/intake_llm.go`, replace the type + `Error()` (currently at the bottom of the file):

```go
// httpStatusError carries a non-200 status (and a short response snippet) so
// llmIntake falls back to heuristic AND the reason is legible in logs/UI.
type httpStatusError struct {
	code    int
	snippet string
}

func (e *httpStatusError) Error() string {
	if e.snippet != "" {
		return fmt.Sprintf("openai chat %d: %s", e.code, e.snippet)
	}
	return fmt.Sprintf("openai chat non-200 status %d", e.code)
}
```

In the same file, at **both** non-200 sites (in `complete` and `completeStream`), replace:

```go
	if resp.StatusCode != http.StatusOK {
		return "", &httpStatusError{code: resp.StatusCode}
	}
```

with:

```go
	if resp.StatusCode != http.StatusOK {
		snip, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return "", &httpStatusError{code: resp.StatusCode, snippet: strings.TrimSpace(string(snip))}
	}
```

Add `"fmt"` and `"io"` to the import block of `internal/rfq/intake_llm.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rfq/ -run TestHTTPStatusError -v && go build ./...`
Expected: PASS + build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/rfq/intake_llm.go internal/rfq/intake_llm_status_test.go
git commit -m "feat(rfq): surface status code + body snippet in openai chat errors"
```

---

### Task 3: PingProvider (one-shot connectivity check)

**Files:**
- Create: `internal/rfq/ping.go`
- Test: `internal/rfq/ping_test.go`

**Interfaces:**
- Consumes: `ProviderConfig`, `newOpenAIChat`, `newAnthropicChat`, `ChatMessage`.
- Produces: `func PingProvider(ctx context.Context, cfg ProviderConfig, logger zerolog.Logger) error`

- [ ] **Step 1: Write the failing test**

Create `internal/rfq/ping_test.go`:

```go
package rfq

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestPingProvider_NoKeyReturnsError(t *testing.T) {
	err := PingProvider(context.Background(), ProviderConfig{Provider: "openai_compatible"}, zerolog.Nop())
	if err == nil || !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("expected no-API-key error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rfq/ -run TestPingProvider -v`
Expected: FAIL — `undefined: PingProvider`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/rfq/ping.go`:

```go
package rfq

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"
)

// PingProvider makes ONE minimal chat call against the resolved config to prove
// the provider + model + key actually work. Returns nil on a 200 reply, else a
// descriptive error. An empty APIKey is reported as an error: that config runs
// in heuristic mode, not against a model.
func PingProvider(ctx context.Context, cfg ProviderConfig, logger zerolog.Logger) error {
	if cfg.APIKey == "" {
		return errors.New("no API key configured")
	}
	var chat llmChat
	switch cfg.Provider {
	case "openai_compatible":
		chat = newOpenAIChat(cfg.BaseURL, cfg.Model, cfg.APIKey, logger)
	default: // anthropic
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		chat = newAnthropicChat(cfg.APIKey, model, logger)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := chat.complete(ctx, "Reply with the single word: ok", []ChatMessage{{Role: "user", Content: "ping"}})
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/rfq/ -run TestPingProvider -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rfq/ping.go internal/rfq/ping_test.go
git commit -m "feat(rfq): add PingProvider one-shot connectivity check"
```

---

### Task 4: Record intake outcomes into the tracker (wiring)

**Files:**
- Modify: `internal/rfq/intake.go` (`BuildIntake` signature + both `llmIntake{...}` literals)
- Modify: `internal/rfq/intake_llm.go` (add `health` field; record at fallback/success sites; add `"errors"` import)
- Modify: `internal/rfq/service.go` (`Service` gains `health *HealthTracker`; `NewService` param; `intakeFor` passes it)
- Modify: `internal/httpapi/router.go` (create tracker, pass to `rfq.NewService`)
- Modify: `internal/rfq/intake_test.go:189,196` (add `nil` third arg to `BuildIntake`)
- Test: `internal/rfq/intake_health_test.go` (create)

**Interfaces:**
- Consumes: `NewHealthTracker`, `HealthTracker`, `BuildIntake`.
- Produces:
  - `func BuildIntake(cfg ProviderConfig, logger zerolog.Logger, health *HealthTracker) IntakeAgent`
  - `func NewService(q *sqlc.Queries, pub *eventbus.Publisher, resolver ExtractorResolver, loader *attachments.Loader, logger zerolog.Logger, ttlDays int, health *HealthTracker) *Service`
  - `llmIntake` gains field `health *HealthTracker`.

- [ ] **Step 1: Write the failing test**

Create `internal/rfq/intake_health_test.go` (reuses the existing `fakeChat` from `intake_test.go`):

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/rfq/ -run TestLLMIntake_Records -v`
Expected: FAIL — `unknown field 'health' in struct literal`.

- [ ] **Step 3: Implement the wiring**

In `internal/rfq/intake_llm.go`, add `"errors"` to imports and add the field + record calls:

```go
type llmIntake struct {
	chat     llmChat
	fallback IntakeAgent
	logger   zerolog.Logger
	system   string
	health   *HealthTracker
}
```

In `Decide`, the chat-error branch (after the `ctx.Err()` guard):

```go
		l.logger.Warn().Err(err).Msg("intake LLM call failed; using heuristic")
		l.health.RecordFallback(err)
		return l.fallback.Decide(ctx, msgs)
```

In `DecideStream`, the stream-error branch (after the `ctx.Err()` guard):

```go
		l.logger.Warn().Err(err).Msg("intake LLM stream failed; using heuristic")
		l.health.RecordFallback(err)
		return l.fallback.Decide(ctx, msgs)
```

Replace `decisionFromRaw` body to record success/fallback:

```go
func (l llmIntake) decisionFromRaw(ctx context.Context, raw string, msgs []ChatMessage) (IntakeDecision, error) {
	dec, ok := parseDecision(raw)
	if !ok {
		l.logger.Warn().Str("raw", raw).Msg("intake LLM returned unparseable JSON; using heuristic")
		l.health.RecordFallback(errors.New("model returned unparseable JSON"))
		return l.fallback.Decide(ctx, msgs)
	}
	if dec.Action == "create" {
		l.health.RecordSuccess()
		title := strings.TrimSpace(dec.Title)
		text := strings.TrimSpace(dec.RFQText)
		if text == "" {
			text = joinUser(msgs)
		}
		if title == "" {
			title = deriveTitle(text)
		}
		return IntakeDecision{Action: "create", Title: title, Text: text}, nil
	}
	q := strings.TrimSpace(dec.Message)
	if q == "" {
		l.health.RecordFallback(errors.New("model returned empty question"))
		return l.fallback.Decide(ctx, msgs)
	}
	l.health.RecordSuccess()
	return IntakeDecision{Action: "ask", Message: q}, nil
}
```

In `internal/rfq/intake.go`, change `BuildIntake` and both literals:

```go
func BuildIntake(cfg ProviderConfig, logger zerolog.Logger, health *HealthTracker) IntakeAgent {
	prompt := cfg.IntakePrompt
	if prompt == "" {
		prompt = DefaultIntakePrompt
	}
	switch {
	case cfg.APIKey == "":
		return HeuristicIntake{}
	case cfg.Provider == "openai_compatible":
		return llmIntake{chat: newOpenAIChat(cfg.BaseURL, cfg.Model, cfg.APIKey, logger), fallback: HeuristicIntake{}, logger: logger, system: prompt, health: health}
	default: // anthropic
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		return llmIntake{chat: newAnthropicChat(cfg.APIKey, model, logger), fallback: HeuristicIntake{}, logger: logger, system: prompt, health: health}
	}
}
```

In `internal/rfq/service.go`: add `health *HealthTracker` to the `Service` struct, add the param to `NewService` (append after `ttlDays int`) and set `health: health` in the returned struct, and change `intakeFor`'s last line:

```go
	return BuildIntake(cfg, s.logger, s.health)
```

In `internal/httpapi/router.go`, just before `settingsSvc := ...`:

```go
	llmHealth := rfq.NewHealthTracker(20)
```

and update the `rfq.NewService(...)` call to pass `llmHealth` as the final argument:

```go
	rfq.NewHandler(rfq.NewService(q, pub, settingsSvc, attLoader, cfg.Logger, cfg.RFQTTLDays, llmHealth), q).Register(f, guard...)
```

In `internal/rfq/intake_test.go`, update the two `BuildIntake(...)` calls (lines ~189, ~196) to pass `nil` as the third argument, e.g.:

```go
	li, ok := BuildIntake(ProviderConfig{Provider: "anthropic", APIKey: "k"}, zerolog.Nop(), nil).(llmIntake)
```
```go
	li2, _ := BuildIntake(ProviderConfig{Provider: "anthropic", APIKey: "k", IntakePrompt: "X"}, zerolog.Nop(), nil).(llmIntake)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rfq/... -race && go build ./...`
Expected: PASS (new `TestLLMIntake_Records*` + all existing rfq tests) + build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/rfq/intake.go internal/rfq/intake_llm.go internal/rfq/service.go internal/httpapi/router.go internal/rfq/intake_test.go internal/rfq/intake_health_test.go
git commit -m "feat(rfq): record intake LLM fallback/success into HealthTracker"
```

---

### Task 5: settings backend — TestConnection, Health, routes, router wiring

**Files:**
- Modify: `internal/settings/service.go` (`Service` gains `health *rfq.HealthTracker`; `NewService` param; `TestResult`, `toTestResult` helper, `TestConnection`, `Health`; add `"time"` import)
- Modify: `internal/settings/handler.go` (`POST /test`, `GET /health` routes + handlers)
- Modify: `internal/httpapi/router.go` (pass `llmHealth` to `settings.NewService`)
- Test: `internal/settings/testresult_test.go` (create)

**Interfaces:**
- Consumes: `rfq.PingProvider`, `rfq.HealthTracker`, `rfq.HealthSnapshot`, `Service.ExtractionConfig`.
- Produces:
  - `type TestResult struct { Ok bool; Model string; LatencyMs int64; Error string }` (JSON: `ok`, `model`, `latency_ms`, `error`)
  - `func NewService(q *sqlc.Queries, box *cryptobox.Box, env rfq.ProviderConfig, health *rfq.HealthTracker) *Service`
  - `func (s *Service) TestConnection(ctx context.Context) (TestResult, error)`
  - `func (s *Service) Health() rfq.HealthSnapshot`

- [ ] **Step 1: Write the failing test**

Create `internal/settings/testresult_test.go`:

```go
package settings

import (
	"errors"
	"testing"
	"time"
)

func TestToTestResult_Success(t *testing.T) {
	r := toTestResult("qwen3.7-plus", 120*time.Millisecond, nil)
	if !r.Ok || r.Model != "qwen3.7-plus" || r.Error != "" || r.LatencyMs != 120 {
		t.Fatalf("unexpected: %+v", r)
	}
}

func TestToTestResult_Failure(t *testing.T) {
	r := toTestResult("bad", 5*time.Millisecond, errors.New("openai chat 404: Model not exist."))
	if r.Ok || r.Error == "" || r.Model != "bad" {
		t.Fatalf("unexpected: %+v", r)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/settings/ -run TestToTestResult -v`
Expected: FAIL — `undefined: toTestResult`.

- [ ] **Step 3: Implement service methods**

In `internal/settings/service.go`, add `"time"` to imports, extend the struct + constructor, and add the methods:

```go
type Service struct {
	q      *sqlc.Queries
	box    *cryptobox.Box
	env    rfq.ProviderConfig
	health *rfq.HealthTracker
}

func NewService(q *sqlc.Queries, box *cryptobox.Box, env rfq.ProviderConfig, health *rfq.HealthTracker) *Service {
	return &Service{q: q, box: box, env: env, health: health}
}

// TestResult is the outcome of a one-shot provider connectivity check.
type TestResult struct {
	Ok        bool   `json:"ok"`
	Model     string `json:"model"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error"`
}

// toTestResult maps a ping outcome into the API shape (pure; unit-tested).
func toTestResult(model string, latency time.Duration, perr error) TestResult {
	r := TestResult{Model: model, LatencyMs: latency.Milliseconds()}
	if perr != nil {
		r.Error = perr.Error()
		return r
	}
	r.Ok = true
	return r
}

// TestConnection runs one real call against the SAVED provider config.
func (s *Service) TestConnection(ctx context.Context) (TestResult, error) {
	cfg, err := s.ExtractionConfig(ctx, "")
	if err != nil {
		return TestResult{}, err
	}
	start := time.Now()
	perr := rfq.PingProvider(ctx, cfg, zerolog.Nop())
	return toTestResult(cfg.Model, time.Since(start), perr), nil
}

// Health returns the current intake-fallback health snapshot.
func (s *Service) Health() rfq.HealthSnapshot {
	return s.health.Snapshot()
}
```

Add `"github.com/rs/zerolog"` to the import block if not already present.

- [ ] **Step 4: Add the routes**

In `internal/settings/handler.go`, extend `Register` and add handlers:

```go
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/admin/v1/settings", guard...)
	g.Get("/", h.get)
	g.Put("/", h.update)
	g.Post("/test", h.test)
	g.Get("/health", h.health)
}

func (h *Handler) test(c *fiber.Ctx) error {
	res, err := h.svc.TestConnection(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *Handler) health(c *fiber.Ctx) error {
	return c.JSON(h.svc.Health())
}
```

- [ ] **Step 5: Wire the tracker in the router**

In `internal/httpapi/router.go`, change the settings construction to pass the tracker created in Task 4:

```go
	settingsSvc := settings.NewService(q, box, envExtraction, llmHealth)
```

- [ ] **Step 6: Run tests + build**

Run: `go test ./internal/settings/... && go build ./... && go vet ./internal/settings/... ./internal/httpapi/...`
Expected: PASS + clean build.

- [ ] **Step 7: Commit**

```bash
git add internal/settings/service.go internal/settings/handler.go internal/httpapi/router.go internal/settings/testresult_test.go
git commit -m "feat(settings): add /test connection check + /health fallback snapshot"
```

---

### Task 6: Admin web — Test button + degraded banner

**Files:**
- Modify: `web/lib/admin.ts` (types + `testAdminSettings`, `getAdminHealth`)
- Modify: `web/app/admin/page.tsx` (Test button + result line; health poll + banner)

**Interfaces:**
- Consumes: backend `POST /admin/v1/settings/test` → `{ ok, model, latency_ms, error }`; `GET /admin/v1/settings/health` → `{ window, recent_fallbacks, last_error, last_fallback_at, degraded }`; existing `req`, `useAdminToken`.
- Produces: `testAdminSettings(token)`, `getAdminHealth(token)`, types `TestResult`, `LlmHealth`.

- [ ] **Step 1: Add the client functions**

In `web/lib/admin.ts`, add near the other exports:

```typescript
export type TestResult = { ok: boolean; model: string; latency_ms: number; error: string };
export type LlmHealth = {
  window: number;
  recent_fallbacks: number;
  last_error: string;
  last_fallback_at: string | null;
  degraded: boolean;
};

export const testAdminSettings = (token: string) =>
  req<TestResult>("POST", "/admin/v1/settings/test", { token });
export const getAdminHealth = (token: string) =>
  req<LlmHealth>("GET", "/admin/v1/settings/health", { token });
```

- [ ] **Step 2: Add the Test button + banner to the page**

In `web/app/admin/page.tsx`:

Add imports:

```typescript
import { getAdminSettings, updateAdminSettings, testAdminSettings, getAdminHealth, useAdminToken, type AdminSettings, type TestResult, type LlmHealth } from "@/lib/admin";
```

Add state (near the other `useState` hooks):

```typescript
  const [health, setHealth] = useState<LlmHealth | null>(null);
  const [test, setTest] = useState<TestResult | null>(null);
  const [testing, setTesting] = useState(false);
```

Add a health poll effect (after the existing settings-loading effect):

```typescript
  useEffect(() => {
    if (!token) return;
    let alive = true;
    const tick = () => getAdminHealth(token).then((h) => { if (alive) setHealth(h); }).catch(() => {});
    tick();
    const id = setInterval(tick, 30000);
    return () => { alive = false; clearInterval(id); };
  }, [token]);
```

Add the test handler:

```typescript
  const runTest = async () => {
    if (!token) return;
    setTesting(true); setTest(null);
    try { setTest(await testAdminSettings(token)); }
    catch (e) { setTest({ ok: false, model: "", latency_ms: 0, error: e instanceof Error ? e.message : "test failed" }); }
    finally { setTesting(false); getAdminHealth(token).then(setHealth).catch(() => {}); }
  };
```

Render the banner at the very top of the returned card/section (only when degraded):

```tsx
      {health?.degraded && (
        <div className="err" style={{ marginBottom: "1rem", padding: "0.75rem 1rem", borderRadius: 8 }}>
          ⚠️ <strong>AI is in scripted fallback mode.</strong> Recent model calls failed
          ({health.recent_fallbacks} of the last {health.window}).{" "}
          {health.last_error ? <>Last error: <code>{health.last_error}</code>.</> : null}{" "}
          Check the model settings below and use <strong>Test connection</strong>.
        </div>
      )}
```

Replace the Save button row so Test sits beside Save and shows its result:

```tsx
      <div className="row" style={{ gap: "0.5rem", alignItems: "center" }}>
        <button className="btn btn-primary" disabled={busy}>{busy ? "Saving…" : "Save"}</button>
        <button type="button" className="btn btn-ghost" disabled={testing || busy} onClick={runTest}>
          {testing ? "Testing…" : "Test connection"}
        </button>
        {test && (test.ok
          ? <span className="ok small">✅ Connected — {test.model} ({test.latency_ms} ms)</span>
          : <span className="err small">❌ {test.error}</span>)}
      </div>
```

(If the Test button sits inside the `<form>`, keep `type="button"` so it does not submit/save.)

- [ ] **Step 3: Type-check + existing web tests**

Run: `cd web && npx tsc --noEmit && npm test`
Expected: no type errors; existing vitest suite passes.

- [ ] **Step 4: Production build**

Run: `cd web && npm run build`
Expected: build succeeds, `/admin` route compiles. (Do NOT pipe through `head` — SIGPIPE corrupts `.next`.)

- [ ] **Step 5: Commit**

```bash
git add web/lib/admin.ts web/app/admin/page.tsx
git commit -m "feat(admin-web): Test connection button + AI fallback banner"
```

---

## Live Verification (after Task 6, before publish)

Per the `verify` skill — drive the real endpoints (Core API on :8080, admin creds from `.env`):

1. `TOKEN=$(curl -s -X POST localhost:8080/admin/v1/login -d '{"email":"…","password":"…"}' -H 'content-type: application/json' | jq -r .token)`
2. **Test (good):** `curl -s -X POST localhost:8080/admin/v1/settings/test -H "authorization: Bearer $TOKEN"` → `{"ok":true,"model":"qwen3.7-plus",…}`.
3. **Test (bad):** temporarily PUT model `Qwen3.7-Plus` → test → `{"ok":false,"error":"openai chat 404: …Model not exist.…"}`; restore to `qwen3.7-plus`.
4. **Health degraded:** with the bad model set, drive one buyer RFQ chat (or `GET /admin/v1/settings/health` after a failing intake) → `degraded:true, recent_fallbacks>0`; after restoring + one good intake → `degraded:false`.
5. Load `switeaz.com/admin` and confirm the banner shows/clears and the Test button reports ✅/❌.

Then `publish` (core web rebuild + core api restart, since Go + web changed).

## Self-Review

- **Spec coverage:** Test-connection (saved cfg) → Tasks 3+5+6; HealthTracker → Task 1; intake record wiring + no-key/ctx exclusions → Task 4; actionable error text → Task 2; routes under admin guard → Task 5; banner + poll → Task 6; intake-only scope + nil-safe tracker → Global Constraints + Tasks 1/4. All covered.
- **Placeholder scan:** none — every code step is complete.
- **Type consistency:** `HealthSnapshot`/`HealthTracker`/`NewHealthTracker` (Task 1) used verbatim in Tasks 4–6; `BuildIntake(cfg, logger, health)` and `rfq.NewService(…, llmHealth)` signatures match between Tasks 4 and 5; `TestResult` JSON tags (`ok/model/latency_ms/error`) match between Task 5 backend and Task 6 client; `LlmHealth` fields match `HealthSnapshot` JSON tags.

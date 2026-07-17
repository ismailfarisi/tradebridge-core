# Sprint 5 · Plan 2 — Buyer Thread SSE Implementation Plan

> **For agentic workers:** TDD where practical, commit per task. Mirror `internal/gateway/stream.go`
> (SSE writer + Redis subscribe, minus seq-replay) and `internal/rfq/handler.go` (SSE under the tenant
> guard, consumed via fetch — see `web/lib/sse.ts`).

**Goal:** Give the buyer↔seller quote thread live updates. A `thread:<quoteID>` Redis channel carries a
compact envelope on every new thread message or quote version; a new authed SSE endpoint live-tails it;
the thread REST payload gains the version history.

**Architecture:** New `internal/thread` package: a nil-safe `Publisher` (publishes to `thread:<quoteID>`)
and an SSE `Handler`. The message/version creation points (quotes `AddMessage`/`Counter`, gateway
`quote.counter`) call the publisher. No durable replay — the page loads the thread over REST first and
re-fetches on reconnect (a thread is ephemeral while open).

**Tech Stack:** Go, Fiber, Redis (go-redis), fasthttp stream writer.

## Global Constraints

- Merge to `master` LOCALLY ONLY, never push. No new migration.
- DB tests → `tradebridge_core_test` only (derive URL secret-free as in Plan 1).
- `go build ./...` clean. The publisher is **nil-safe** (Redis nil → no-op) so tests/dev without Redis pass.
- SSE endpoint is under the existing tenant guard (Authorization header; the frontend uses fetch+ReadableStream, not EventSource).

## File Structure

- Create: `internal/thread/publisher.go` (Publisher + envelope types), `internal/thread/publisher_test.go`
- Create: `internal/thread/stream.go` (SSE Handler: `GET /v1/quotes/:id/thread/stream`)
- Modify: `internal/quotes/service.go` (add `*thread.Publisher`; publish on AddMessage + Counter)
- Modify: `internal/quotes/handler.go` (add `GET /v1/quotes/:id/thread` returning messages+versions)
- Modify: `internal/gateway/commands.go` (publish on `quote.counter`; gateway gets a `*thread.Publisher`)
- Modify: `internal/gateway/gateway.go` + `cmd/gateway/main.go` (construct + pass the publisher)
- Modify: `internal/httpapi/router.go` (construct `thread.Publisher` from `cfg.Redis`; pass to quotes;
  register the thread SSE handler)

## Envelope

```go
package thread

// Envelope is the compact live-update pushed to thread:<quoteID>. kind is
// "message" or "version"; only the matching field is populated.
type Envelope struct {
	Kind    string          `json:"kind"`    // "message" | "version"
	QuoteID string          `json:"quote_id"`
	Message json.RawMessage `json:"message,omitempty"`
	Version json.RawMessage `json:"version,omitempty"`
}

func channel(quoteID string) string { return "thread:" + quoteID }
```

---

### Task 1: thread.Publisher (nil-safe)

**Files:** `internal/thread/publisher.go`, `internal/thread/publisher_test.go`.

```go
type Publisher struct {
	redis  *redis.Client // may be nil → publishing is a no-op
	logger zerolog.Logger
}
func NewPublisher(rdb *redis.Client, logger zerolog.Logger) *Publisher { return &Publisher{rdb, logger} }

// PublishMessage / PublishVersion marshal the row to JSON and publish an
// Envelope to thread:<quoteID>. Best-effort: a nil client or publish error is
// logged, never returned (the DB row is the source of truth).
func (p *Publisher) PublishMessage(ctx context.Context, quoteID string, messageJSON []byte) { ... }
func (p *Publisher) PublishVersion(ctx context.Context, quoteID string, versionJSON []byte) { ... }
```

- [ ] **Step 1: Failing test** — a `NewPublisher(nil, zerolog.Nop())` `PublishMessage`/`PublishVersion`
  do not panic and are no-ops; the `channel(id)` helper returns `"thread:"+id`; the `Envelope` marshals
  with the right `kind`. (Redis-backed delivery is covered by the E2E; here assert nil-safety + shape.)
- [ ] **Step 2–4: fail → implement → pass.**
- [ ] **Step 5: Commit** — `feat(thread): nil-safe thread event publisher`.

---

### Task 2: thread SSE stream handler

**Files:** `internal/thread/stream.go`.

```go
type Handler struct { q *sqlc.Queries; redis *redis.Client; logger zerolog.Logger }
func NewHandler(q *sqlc.Queries, rdb *redis.Client, logger zerolog.Logger) *Handler { ... }
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Add("GET", "/v1/quotes/:id/thread/stream", append(guard, h.stream)...)
}
```
`stream`:
1. Load the quote (`GetQuote`); `pgx.ErrNoRows` → 404. Party check: `me := middleware.CompanyID(c)`; must
   be the quote's seller OR the buyer on the quote's RFQ (mirror `quotes.mustParty`); else 404.
2. Set SSE headers (`text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`,
   `X-Accel-Buffering: no`).
3. `SetBodyStreamWriter` (mirror `internal/gateway/stream.go`): subscribe to `thread:<quoteID>` (skip if
   `h.redis == nil` → just keepalive), write a `: connected` comment, then loop: on a Redis message write
   `event: thread\ndata: <payload>\n\n` + flush; on a keepalive ticker write a comment. Return on flush error.

- [ ] **Step 1: Light test** — build a minimal Fiber app with a fake guard injecting a company id (locals
  key from `pkg/middleware/tenant.go`); seed a quote via a shared `q`. Assert a NON-party company id →
  404. For a party, assert the response `Content-Type` is `text/event-stream` (use `app.Test(req, <shortMs>)`
  so the streaming request returns after the timeout; the header is set before streaming). If `app.Test`
  blocks on the stream, assert the party/non-party split at the handler's pre-stream guard by extracting
  it into a helper `authorizeThreadParty(ctx, q, quoteID, companyID) error` and unit-testing that.
- [ ] **Step 2–4: fail → implement → pass.**
- [ ] **Step 5: Commit** — `feat(thread): buyer/seller thread SSE stream (live-tail)`.

---

### Task 3: publish on writes + thread REST + wiring

**Files:** `internal/quotes/service.go`, `internal/quotes/handler.go`, `internal/gateway/commands.go`,
`internal/gateway/gateway.go`, `cmd/gateway/main.go`, `internal/httpapi/router.go`.

- Add `thread *thread.Publisher` to `quotes.Service` (final constructor param; update router + test
  constructors). In `AddMessage` and `Counter`, after `CreateQuoteMessage`, call
  `s.thread.PublishMessage(ctx, quoteID, mustJSON(messageView(msg)))` (nil-safe).
- Add `thread *thread.Publisher` to the gateway `Handler` (final param of `NewHandler`; pass a publisher
  built from `app.Redis` in `cmd/gateway/main.go`). In the `quote.counter` side effect, after recording
  the seller message + version, `PublishMessage` for the message and `PublishVersion` for the new version.
- **Thread REST:** add `GET /v1/quotes/:id/thread` to the quotes handler (party-only via `mustParty`):
  returns `{quote: quoteView(head), messages: [...], versions: [...]}` using `ListQuoteMessages` +
  `ListQuoteVersions`. (Keeps the existing `/messages` endpoint; this is the combined thread load.)
- **Router:** `threadPub := thread.NewPublisher(cfg.Redis, cfg.Logger)`; pass to `quotes.NewService(...)`;
  `thread.NewHandler(q, cfg.Redis, cfg.Logger).Register(f, guard...)`.

- [ ] **Step 1:** Wire everything; `go build ./...` clean.
- [ ] **Step 2: Test** — extend a quotes DB test: `GET /v1/quotes/:id/thread` (or the service-level
  equivalent) returns the head quote + messages + versions after a submit + counter. Assert versions has
  the expected rows. (Publishing itself is nil-safe and covered by the E2E with Redis.)
- [ ] **Step 3:** `go test ./internal/quotes/ ./internal/thread/ ./internal/gateway/ -count=1` pass.
- [ ] **Step 4: Commit** — `feat(thread): publish on message/version writes + combined thread REST + wiring`.

---

## Self-Review checklist

- `go build ./...` clean; full suite `go test ./... -p 1 -count=1` — 0 failures.
- Publisher is nil-safe (Redis nil → no panic, no-op).
- Thread SSE is party-only (non-party → 404); sets `text/event-stream`; live-tails `thread:<quoteID>`.
- Every thread write (buyer message, buyer counter, seller counter message, new version) publishes an envelope.
- `GET /v1/quotes/:id/thread` returns messages + versions for a party; no cross-tenant leak.
- No `%widgets%` supplier_profiles fixtures in new tests.

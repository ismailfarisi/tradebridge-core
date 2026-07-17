# Sprint 4 · Plan 1 — Order Lifecycle Implementation Plan

> **For agentic workers:** Implement task-by-task with TDD. Steps use `- [ ]`. Follow existing
> patterns in `internal/trust` (service+handler split) and `internal/quotes` (tx via pool).

**Goal:** Give orders a real lifecycle — `created → confirmed → fulfilled → completed` (+ `cancelled`,
`disputed`) — with role-based transition authorization, a durable event timeline, list endpoints for
both dashboards, and a counterparty notification on every transition.

**Architecture:** New `internal/orders/service.go` holds the state machine + authz and runs each
transition in a pgx transaction (status update + `order_events` insert), notifying the counterparty
after commit via the S1 `notify` engine. The existing `internal/orders/handler.go` gains transition +
list routes and a timeline on GET. Migration 019 adds a status CHECK and the `order_events` table.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5, golang-migrate, zerolog.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- DB/integration tests run against `tradebridge_core_test` ONLY. Derive the URL secret-free:
  `TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- Apply migration 019 to the test DB before running DB tests:
  `export PATH="$HOME/.local/bin:$PATH"; migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up`
- Regenerate sqlc after editing `pkg/db/query/*.sql`: `sqlc generate` (run from repo root; config `sqlc.yaml`).
- DB tests are non-hermetic (shared persistent test DB). Use per-run-unique names/ids and `t.Cleanup`
  to stay idempotent across repeated `go test` runs (see `internal/trust/queries_test.go`).
- Module path: `github.com/tradebridge/core`. ULIDs via `gen_ulid()` DB default.

## File Structure

- Create: `pkg/db/migrations/019_order_lifecycle.up.sql`, `019_order_lifecycle.down.sql`
- Create: `pkg/db/query/orders.sql` (new order/order_events queries; `CreateOrder`/`GetOrder` stay in `quotes.sql`)
- Create: `internal/orders/service.go` (state machine + authz + transactional transition + list + timeline)
- Create: `internal/orders/service_test.go` (state-machine unit tests, no DB)
- Create: `internal/orders/orders_db_test.go` (DB-backed transition/list/timeline tests)
- Modify: `internal/orders/handler.go` (transition + list routes; timeline on GET; new constructor)
- Modify: `internal/notify/notify.go` (new Type constants), `internal/notify/handler.go` (`validEventType`)
- Modify: `internal/httpapi/router.go:245` (orders handler construction with service + notifier + pool)

## Migration 019

**`019_order_lifecycle.up.sql`:**
```sql
-- Order lifecycle: constrain status to the state machine and record every
-- transition in an append-only timeline used by both dashboards and as audit.
ALTER TABLE orders
    ADD CONSTRAINT orders_status_check
    CHECK (status IN ('created','confirmed','fulfilled','completed','cancelled','disputed'));

CREATE TABLE order_events (
    id               TEXT PRIMARY KEY DEFAULT gen_ulid(),
    order_id         TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    from_status      TEXT NOT NULL,
    to_status        TEXT NOT NULL,
    actor            TEXT NOT NULL,            -- 'buyer' | 'seller' | 'admin'
    actor_company_id TEXT REFERENCES companies(id), -- NULL for admin actor
    note             TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_order_events_order ON order_events (order_id, created_at);
```

**`019_order_lifecycle.down.sql`:**
```sql
DROP TABLE IF EXISTS order_events;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
```

## Queries — `pkg/db/query/orders.sql`

```sql
-- name: SetOrderStatus :one
UPDATE orders SET status = @status WHERE id = @id RETURNING *;

-- name: ListOrdersByCompany :many
SELECT * FROM orders
WHERE buyer_company_id = @company_id OR seller_company_id = @company_id
ORDER BY created_at DESC;

-- name: CreateOrderEvent :one
INSERT INTO order_events (order_id, from_status, to_status, actor, actor_company_id, note)
VALUES (@order_id, @from_status, @to_status, @actor, sqlc.narg(actor_company_id), @note)
RETURNING *;

-- name: ListOrderEvents :many
SELECT * FROM order_events WHERE order_id = @order_id ORDER BY created_at ASC;
```

Run `sqlc generate` and confirm `SetOrderStatus`, `ListOrdersByCompany`, `CreateOrderEvent`,
`ListOrderEvents` appear in `pkg/db/sqlc/`.

## Notify additions

In `internal/notify/notify.go` add to the `const` block:
```go
	// TypeOrderConfirmed fires when a seller confirms an order.
	TypeOrderConfirmed = "order.confirmed"
	// TypeOrderFulfilled fires when a seller marks an order fulfilled/shipped.
	TypeOrderFulfilled = "order.fulfilled"
	// TypeOrderCompleted fires when a buyer marks an order completed.
	TypeOrderCompleted = "order.completed"
	// TypeOrderCancelled fires when either party cancels an order.
	TypeOrderCancelled = "order.cancelled"
```
In `internal/notify/handler.go` `validEventType`, extend the case list to include
`TypeOrderConfirmed, TypeOrderFulfilled, TypeOrderCompleted, TypeOrderCancelled` so users can toggle them.

---

### Task 1: State machine + authz (no DB)

**Files:** Create `internal/orders/service.go` (machine part), `internal/orders/service_test.go`.

**Interfaces (Produces):**
- `func CanTransition(from, to string) bool`
- `func RoleFor(to string) string` — returns `"seller"`, `"buyer"`, or `"either"`.
- `var PartyTargets = map[string]bool{...}` — the states a party may drive via the endpoint.
- `func actorRole(order sqlc.Order, companyID string) (string, bool)` — `"buyer"`/`"seller"` and ok.

- [ ] **Step 1: Write failing tests** — `internal/orders/service_test.go`:
```go
package orders

import "testing"

func TestCanTransition(t *testing.T) {
	ok := [][2]string{
		{"created", "confirmed"}, {"created", "cancelled"}, {"created", "disputed"},
		{"confirmed", "fulfilled"}, {"confirmed", "cancelled"}, {"confirmed", "disputed"},
		{"fulfilled", "completed"}, {"fulfilled", "disputed"},
		{"disputed", "completed"}, {"disputed", "cancelled"},
	}
	for _, p := range ok {
		if !CanTransition(p[0], p[1]) {
			t.Errorf("expected %s->%s allowed", p[0], p[1])
		}
	}
	bad := [][2]string{
		{"created", "fulfilled"}, {"created", "completed"}, {"fulfilled", "cancelled"},
		{"completed", "confirmed"}, {"cancelled", "confirmed"}, {"fulfilled", "confirmed"},
	}
	for _, p := range bad {
		if CanTransition(p[0], p[1]) {
			t.Errorf("expected %s->%s disallowed", p[0], p[1])
		}
	}
}

func TestRoleFor(t *testing.T) {
	cases := map[string]string{
		"confirmed": "seller", "fulfilled": "seller",
		"completed": "buyer", "cancelled": "either", "disputed": "either",
	}
	for to, want := range cases {
		if got := RoleFor(to); got != want {
			t.Errorf("RoleFor(%s)=%s want %s", to, got, want)
		}
	}
}

func TestPartyTargetsExcludesDisputed(t *testing.T) {
	if PartyTargets["disputed"] {
		t.Error("disputed must not be a party-driven target (goes through dispute endpoint)")
	}
	for _, s := range []string{"confirmed", "fulfilled", "completed", "cancelled"} {
		if !PartyTargets[s] {
			t.Errorf("%s should be a party target", s)
		}
	}
}
```

- [ ] **Step 2: Run, verify fail** — `go test ./internal/orders/ -run 'TestCanTransition|TestRoleFor|TestPartyTargets' -v` → build/undefined fail.

- [ ] **Step 3: Implement the machine** in `internal/orders/service.go` (top of file):
```go
package orders

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// ErrNotFound is returned when an order is missing or not visible to the caller.
var ErrNotFound = errors.New("order not found")

// ConflictError signals an illegal state transition (HTTP 409).
type ConflictError struct{ Msg string }

func (e ConflictError) Error() string { return e.Msg }

// ForbiddenError signals the caller's role may not drive this transition (403).
type ForbiddenError struct{ Msg string }

func (e ForbiddenError) Error() string { return e.Msg }

// allowedTransitions is the order state machine. disputed is reachable from any
// active state (via the dispute endpoint, Plan 4) and resolved by an admin.
var allowedTransitions = map[string]map[string]bool{
	"created":   {"confirmed": true, "cancelled": true, "disputed": true},
	"confirmed": {"fulfilled": true, "cancelled": true, "disputed": true},
	"fulfilled": {"completed": true, "disputed": true},
	"disputed":  {"completed": true, "cancelled": true},
}

// PartyTargets are the states a buyer/seller may drive via POST .../transitions.
// disputed is excluded — it must go through the dispute endpoint so a dispute
// row is created alongside the transition.
var PartyTargets = map[string]bool{
	"confirmed": true, "fulfilled": true, "completed": true, "cancelled": true,
}

// CanTransition reports whether from->to is a legal edge in the machine.
func CanTransition(from, to string) bool { return allowedTransitions[from][to] }

// RoleFor returns which party role may drive a transition to `to`:
// "seller", "buyer", or "either".
func RoleFor(to string) string {
	switch to {
	case "confirmed", "fulfilled":
		return "seller"
	case "completed":
		return "buyer"
	default: // cancelled, disputed
		return "either"
	}
}

// actorRole returns "buyer" or "seller" for companyID relative to the order,
// and false if the company is not a party to it.
func actorRole(o sqlc.Order, companyID string) (string, bool) {
	switch companyID {
	case o.BuyerCompanyID:
		return "buyer", true
	case o.SellerCompanyID:
		return "seller", true
	default:
		return "", false
	}
}
```

- [ ] **Step 4: Run, verify pass** — same command → PASS.
- [ ] **Step 5: Commit** — `feat(orders): order state machine + role authorization`.

---

### Task 2: Transactional transition + list + timeline service (DB)

**Files:** extend `internal/orders/service.go`; create `internal/orders/orders_db_test.go`.

**Interfaces:**
- Consumes: sqlc `GetOrder`, `SetOrderStatus`, `CreateOrderEvent`, `ListOrderEvents`,
  `ListOrdersByCompany`; `notify.Notifier`.
- Produces:
  - `type Service struct{...}` + `func NewService(q *sqlc.Queries, pool *pgxpool.Pool, notifier *notify.Notifier, logger zerolog.Logger) *Service`
  - `func (s *Service) Transition(ctx, orderID, actorCompanyID, to, note string) (sqlc.Order, error)`
  - `func (s *Service) List(ctx, companyID string) ([]sqlc.Order, error)`
  - `func (s *Service) GetWithTimeline(ctx, orderID, companyID string) (sqlc.Order, []sqlc.OrderEvent, error)`

**Transition logic:**
1. `GetOrder`; `pgx.ErrNoRows` → `ErrNotFound`.
2. `actorRole(order, actorCompanyID)`; not a party → `ErrNotFound` (don't leak).
3. `to` not in `PartyTargets` → `ConflictError{"unsupported transition target"}`.
4. `!CanTransition(order.Status, to)` → `ConflictError{"cannot move from <status> to <to>"}`.
5. Role check: `want := RoleFor(to)`; if `want != "either" && want != role` → `ForbiddenError`.
6. Tx: `SetOrderStatus{ID, Status:to}`, then `CreateOrderEvent{OrderID, FromStatus:order.Status,
   ToStatus:to, Actor:role, ActorCompanyID:&actorCompanyID, Note:note}`; commit.
7. After commit: notify the **counterparty** (the party ≠ actor) with the matching typed event
   (map `to`→Type/Title/Body/CTA). Notifier may be nil (guard).

Notification mapping helper:
```go
func notifyEventFor(to, orderID string) (notify.Event, bool) {
	m := map[string]struct{ typ, title, body string }{
		"confirmed": {notify.TypeOrderConfirmed, "Order confirmed", "The seller confirmed your order."},
		"fulfilled": {notify.TypeOrderFulfilled, "Order fulfilled", "The seller marked your order fulfilled."},
		"completed": {notify.TypeOrderCompleted, "Order completed", "The order was marked completed."},
		"cancelled": {notify.TypeOrderCancelled, "Order cancelled", "The order was cancelled."},
	}
	e, ok := m[to]
	if !ok {
		return notify.Event{}, false
	}
	return notify.Event{
		Type: e.typ, Title: e.title, Body: e.body,
		Data:     map[string]any{"order_id": orderID},
		CTALabel: "View order", CTAPath: "/orders/" + orderID,
	}, true
}
```
(Notify the counterparty only. For `cancelled`, notify the party who did NOT cancel.)

- [ ] **Step 1: Write failing DB tests** — `internal/orders/orders_db_test.go`. Use the harness
  pattern from `internal/trust/queries_test.go` (`testQueriesOrSkip`, `makeCompany`). Seed helper
  that creates buyer+seller companies, an RFQ (needs `CreatedBy` user — create a user with a unique
  email), a quote, and an order (via `CreateOrder`). Tests:
  - `TestTransitionHappyPath`: seller confirms `created→confirmed`; buyer completes only after
    `fulfilled`; assert order status + that `ListOrderEvents` grew by one with correct actor.
  - `TestTransitionWrongRole`: buyer tries `→confirmed` → `ForbiddenError`.
  - `TestTransitionIllegal`: `created→completed` → `ConflictError`.
  - `TestTransitionNonParty`: a third company → `ErrNotFound`.
  - `TestListOrdersByCompany`: returns the order for both buyer and seller company ids.
  - `TestCancelBeforeFulfilledOnly`: `fulfilled→cancelled` rejected as `ConflictError`.
  Build the seed with a nil notifier (`NewService(q, pool, nil, zerolog.Nop())`).

- [ ] **Step 2: Run, verify fail** — `go test ./internal/orders/ -run Transition -v` (with TEST_DATABASE_URL set + migration applied).
- [ ] **Step 3: Implement** the `Service`, `Transition`, `List`, `GetWithTimeline` per the logic above.
      Use `s.pool.Begin`/`WithTx`/`Commit`, `defer tx.Rollback` (mirror `internal/quotes/service.go` Accept).
- [ ] **Step 4: Run, verify pass.**
- [ ] **Step 5: Commit** — `feat(orders): transactional transitions with timeline + notifications`.

---

### Task 3: HTTP routes — transition, list, timeline

**Files:** Modify `internal/orders/handler.go`; add handler tests to `internal/orders/orders_db_test.go`
(or a new `handler_test.go` using `httptest` + the app router if practical — otherwise service-level
tests above suffice; a light handler test that asserts status codes via `app.Test` is preferred).

**New handler shape:**
```go
type Handler struct {
	svc *Service
	q   *sqlc.Queries
}

func NewHandler(svc *Service, q *sqlc.Queries) *Handler { return &Handler{svc: svc, q: q} }

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/orders", guard...)
	g.Get("/", h.list)
	g.Get("/:id", h.get)
	g.Post("/:id/transitions", h.transition)
}
```
- `list`: `h.svc.List(ctx, middleware.CompanyID(c))` → `c.JSON(fiber.Map{"orders": orders})`.
- `get`: `h.svc.GetWithTimeline(ctx, id, companyID)`; `ErrNotFound`→404; return
  `fiber.Map{"order": order, "events": events}`.
- `transition`: parse `{to, note}`; call `h.svc.Transition`; map `ConflictError`→409,
  `ForbiddenError`→403, `ErrNotFound`→404; success → `c.JSON(fiber.Map{"order": order})`.
  Error body: `fiber.NewError(status, err.Error())` (Fiber renders text — matches the web `call<T>` helper).

- [ ] **Step 1: Write failing handler test** — build the app via `httpapi.New` is heavy; instead
  construct a minimal Fiber app in-test mounting `NewHandler(svc,q).Register(app, fakeGuard)` where
  `fakeGuard` injects a company id into locals the way `TenantGuard` does (set the same locals key
  `middleware.CompanyID` reads — check `pkg/middleware/tenant.go` for the locals key name and set it).
  Assert: POST transition happy path 200; wrong role 403; illegal 409. (If the locals wiring is
  awkward, cover these at the service level in Task 2 and keep this a compile/smoke test.)
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** the handler methods.
- [ ] **Step 4: Run, verify pass.**
- [ ] **Step 5: Wire the router** — edit `internal/httpapi/router.go:245`, replacing
  `orders.NewHandler(q).Register(f, guard...)` with:
```go
	orders.NewHandler(orders.NewService(q, cfg.Pool, notifier, cfg.Logger), q).Register(f, guard...)
```
- [ ] **Step 6: Build + full package test** — `go build ./... && go test ./internal/orders/ ./internal/notify/ -v`.
- [ ] **Step 7: Commit** — `feat(orders): transition/list/timeline HTTP routes + router wiring`.

---

## Self-Review checklist (run before marking Plan 1 done)

- `go build ./...` clean; `go vet ./internal/orders/...`.
- Full suite: `go test ./...` (DB tests run with TEST_DATABASE_URL + migration 019 applied) — 0 failures.
- Migration 019 applies AND rolls back cleanly on the test DB (`migrate ... down 1` then `up`).
- No transition endpoint accepts `to="disputed"` (guarded by `PartyTargets`).
- Counterparty (not the actor) is the notification recipient.
- `orders.status` CHECK includes all six states; existing `created` rows still valid.

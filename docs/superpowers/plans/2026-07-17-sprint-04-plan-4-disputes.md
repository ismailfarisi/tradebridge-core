# Sprint 4 · Plan 4 — Disputes + Admin Resolution API Implementation Plan

> **For agentic workers:** TDD, commit per task. Follow `internal/orders` (state machine + tx),
> `internal/trust/admin.go` (admin handler), `internal/reviews` (party derivation).

**Goal:** Either party to an active order can raise a dispute, which moves the order to `disputed` and
locks out party transitions. A platform admin sees a dispute queue and resolves each one — moving the
order to `completed` or `cancelled`, logging the action to `audit_logs`, and notifying both parties.

**Architecture:** New `internal/disputes` package: `Service` (raise inside a tx: create dispute + move
order to disputed + order_event; admin resolve inside a tx: resolve dispute + move order + audit log) +
a party `Handler` + an admin `Handler`. `internal/orders/service.go` gains a guard so a party cannot
transition a `disputed` order (only admin resolution can). Migration 022 adds `disputes`.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5, golang-migrate.

## Global Constraints

- Merge to `master` LOCALLY ONLY, never push.
- DB tests → `tradebridge_core_test` only. Derive URL secret-free:
  `TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- Apply migration 022 to the test DB before DB tests. Builds on 019–021.
- `sqlc generate` after query edits; `go build ./...` stays clean.
- sqlc struct: `disputes`→`sqlc.Dispute`. `CreateAuditLogParams{CompanyID, Actor, Action, TargetID *string, Detail []byte}`.
- `middleware.AdminID(c)` returns the resolving admin's id.
- DB-test fixtures MUST NOT add `%widgets%`-matching `supplier_profiles` rows (avoid the shared match-pool
  flake). Disputes tests don't need supplier profiles — don't create any.

## File Structure

- Create: `pkg/db/migrations/022_disputes.up.sql`, `022_disputes.down.sql`
- Create: `pkg/db/query/disputes.sql`
- Create: `internal/disputes/service.go`, `internal/disputes/handler.go`, `internal/disputes/admin.go`
- Create: `internal/disputes/service_test.go` (DB)
- Modify: `internal/orders/service.go` (guard: no party transition out of `disputed`) + its test
- Modify: `internal/notify/notify.go` (+`TypeDisputeOpened`,`TypeDisputeResolved`), `internal/notify/handler.go`
- Modify: `internal/httpapi/router.go` (register party + admin dispute handlers)

## Migration 022

**`022_disputes.up.sql`:**
```sql
-- Disputes: a party escalates an active order; an admin resolves it. At most one
-- OPEN dispute per order (partial-unique). Resolution moves the order terminal.
CREATE TABLE disputes (
    id                   TEXT PRIMARY KEY DEFAULT gen_ulid(),
    order_id             TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    raised_by_company_id TEXT NOT NULL REFERENCES companies(id),
    reason               TEXT NOT NULL CHECK (reason IN ('not_delivered','not_as_described','payment','other')),
    detail               TEXT NOT NULL DEFAULT '',
    status               TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','rejected')),
    resolution           TEXT NOT NULL DEFAULT '',
    resolved_by_admin_id TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at          TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_disputes_one_open ON disputes (order_id) WHERE status = 'open';
CREATE INDEX idx_disputes_status ON disputes (status, created_at DESC);
```

**`022_disputes.down.sql`:**
```sql
DROP TABLE IF EXISTS disputes;
```

## Queries — `pkg/db/query/disputes.sql`

```sql
-- name: CreateDispute :one
INSERT INTO disputes (order_id, raised_by_company_id, reason, detail)
VALUES (@order_id, @raised_by_company_id, @reason, @detail)
RETURNING *;

-- name: GetDispute :one
SELECT * FROM disputes WHERE id = @id;

-- name: ListDisputesForOrder :many
SELECT * FROM disputes WHERE order_id = @order_id ORDER BY created_at DESC;

-- name: ListDisputes :many
SELECT * FROM disputes
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY created_at DESC;

-- name: ResolveDispute :one
-- Conditional on still-open so a double-resolve is a no-op (0 rows -> conflict).
UPDATE disputes
SET status = @status, resolution = @resolution,
    resolved_by_admin_id = @resolved_by_admin_id, resolved_at = NOW()
WHERE id = @id AND status = 'open'
RETURNING *;
```

## Notify additions

`internal/notify/notify.go`: add `TypeDisputeOpened = "dispute.opened"` and
`TypeDisputeResolved = "dispute.resolved"`. Add both to `validEventType`.

---

### Task 1: Orders guard — no party transition out of `disputed`

**Files:** `internal/orders/service.go`, `internal/orders/orders_db_test.go`.

A `disputed` order must only move via admin resolution, never via the party transition endpoint. The
machine currently allows `disputed → completed|cancelled`, and `completed`/`cancelled` are party targets,
so without a guard a party could POST a transition on a disputed order.

- [ ] **Step 1: Write failing test** in `orders_db_test.go`:
```go
func TestTransitionBlockedWhenDisputed(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()
	order, buyerID, sellerID := seedOrder(t, q, "disputedlock")
	// Force the order into disputed directly (dispute service does this in Plan 4).
	_, err := q.SetOrderStatus(ctx, sqlc.SetOrderStatusParams{ID: order.ID, Status: "disputed", FromStatus: order.Status})
	require.NoError(t, err)
	// Neither party may drive any transition while disputed.
	_, err = svc.Transition(ctx, order.ID, buyerID, "completed", "")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)
	_, err = svc.Transition(ctx, order.ID, sellerID, "cancelled", "")
	require.ErrorAs(t, err, &conflict)
}
```
- [ ] **Step 2: Run, verify fail** (currently a party CAN complete a disputed order).
- [ ] **Step 3: Implement** — in `Transition`, immediately after the non-party check (before `PartyTargets`):
```go
	if order.Status == "disputed" {
		return sqlc.Order{}, ConflictError{Msg: "order is under dispute; resolution is admin-only"}
	}
```
- [ ] **Step 4: Run, verify pass.** Re-run the whole orders package.
- [ ] **Step 5: Commit** — `fix(orders): block party transitions while an order is disputed`.

---

### Task 2: Dispute service — raise + get + list-for-order (DB)

**Files:** `internal/disputes/service.go`, `internal/disputes/service_test.go`.

**Interfaces (Produces):**
- `type Service struct{...}` + `func NewService(q *sqlc.Queries, pool *pgxpool.Pool, notifier *notify.Notifier, logger zerolog.Logger) *Service`
- Errors: `ErrNotFound`, `ConflictError{Msg}`, `ValidationError{Msg}`.
- `func (s *Service) Raise(ctx, orderID, raisedByCompanyID, reason, detail string) (sqlc.Dispute, error)`
- `func (s *Service) ListForOrder(ctx, orderID, companyID string) ([]sqlc.Dispute, error)` (party-only)

**Raise logic:**
1. `reason` not in `{not_delivered,not_as_described,payment,other}` → `ValidationError`.
2. `GetOrder`; no-rows → `ErrNotFound`.
3. Party check: `raisedByCompanyID` is buyer or seller of the order; else `ErrNotFound`. Capture `role`.
4. Order must be active: `order.Status` in `{created,confirmed,fulfilled}` else `ConflictError{"order is not in a disputable state"}`.
5. Tx:
   - `CreateDispute{OrderID, RaisedByCompanyID, Reason, Detail}` — a 23505 unique-violation
     (`*pgconn.PgError.Code=="23505"`) → `ConflictError{"a dispute is already open for this order"}`.
   - `SetOrderStatus{ID, Status:"disputed", FromStatus:order.Status}` (0 rows → `ConflictError` concurrent).
   - `CreateOrderEvent{OrderID, FromStatus:order.Status, ToStatus:"disputed", Actor:role, ActorCompanyID:&raisedByCompanyID, Note:reason}`.
   - Commit.
6. After commit: notify the counterparty with `TypeDisputeOpened` (`CTAPath:"/orders/"+orderID`).

**ListForOrder:** load order, party check (else `ErrNotFound`), `ListDisputesForOrder`.

- [ ] **Step 1: Write failing DB tests** — harness like `internal/orders/orders_db_test.go` (local
  `seedOrder`). Do NOT create supplier_profiles. Tests:
  - `TestRaiseHappyPath`: buyer raises on a `created` order → dispute row `status=open`, `reason` set;
    `GetOrder` now `disputed`; an order_event `to_status=disputed` exists.
  - `TestRaiseNonParty`: stranger → `ErrNotFound`.
  - `TestRaiseInvalidReason`: reason `"nonsense"` → `ValidationError`.
  - `TestRaiseNotDisputableState`: move order to `completed` first, then raise → `ConflictError`.
  - `TestRaiseDuplicateOpen`: raise twice → 2nd `ConflictError`.
  - `TestListForOrderPartyOnly`: party sees the dispute; stranger → `ErrNotFound`.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, verify pass.**
- [ ] **Step 5: Commit** — `feat(disputes): raise a dispute (moves order to disputed) + list for order`.

---

### Task 3: Admin resolution (DB)

**Files:** extend `internal/disputes/service.go`; add tests to `service_test.go`.

**Interfaces (Produces):**
- `func (s *Service) ListForAdmin(ctx, status string) ([]sqlc.Dispute, error)` — `status==""` → all (pass a nil narg).
- `func (s *Service) Resolve(ctx, disputeID, adminID, outcome, orderTo, resolution string) (sqlc.Dispute, error)`

**Resolve logic:**
1. `outcome` not in `{resolved,rejected}` → `ValidationError`; `orderTo` not in `{completed,cancelled}` → `ValidationError`.
2. `GetDispute`; no-rows → `ErrNotFound`.
3. Tx:
   - `ResolveDispute{ID, Status:outcome, Resolution:resolution, ResolvedByAdminID:&adminID}` — 0 rows
     (`pgx.ErrNoRows`) → `ConflictError{"dispute is not open"}`.
   - Load the order (`GetOrder(dispute.OrderID)` via `qtx`). Move it:
     `SetOrderStatus{ID:order.ID, Status:orderTo, FromStatus:"disputed"}` (0 rows → `ConflictError`).
   - `CreateOrderEvent{OrderID:order.ID, FromStatus:"disputed", ToStatus:orderTo, Actor:"admin", ActorCompanyID:nil, Note:resolution}`.
   - `CreateAuditLog{CompanyID:order.BuyerCompanyID, Actor:adminID, Action:"dispute.resolved",
     TargetID:&disputeID, Detail: json.Marshal(map[string]any{"outcome":outcome,"order_to":orderTo,"resolution":resolution})}`.
   - Commit.
4. After commit: notify BOTH parties with `TypeDisputeResolved`.

- [ ] **Step 1: Write failing tests:**
  - `TestResolveMovesOrderAndLogsAudit`: raise a dispute, `Resolve(id, "admin-x", "resolved", "completed", "shipped, closed")`;
    assert dispute `status=resolved`, `resolved_by_admin_id=="admin-x"`, order now `completed`, an
    `order_events` row `from=disputed,to=completed,actor=admin`, and an `audit_logs` row with
    `action="dispute.resolved"` and `target_id==disputeID` (query audit_logs directly via pool or a raw SELECT).
  - `TestResolveRejectedCancelsOrder`: `Resolve(..., "rejected", "cancelled", ...)` → order `cancelled`.
  - `TestResolveInvalidOutcome`: outcome `"maybe"` → `ValidationError`; orderTo `"fulfilled"` → `ValidationError`.
  - `TestResolveAlreadyResolved`: resolve twice → 2nd `ConflictError`.
  - `TestListForAdminFiltersByStatus`: raise 2 disputes on different orders, resolve one, then
    `ListForAdmin("open")` returns only the open one; `ListForAdmin("")` returns both. (Use unique tags;
    the shared DB may hold prior rows — assert the seeded ids are present/absent rather than exact counts.)
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, verify pass.**
- [ ] **Step 5: Commit** — `feat(disputes): admin resolution moves order terminal + audit log`.

---

### Task 4: HTTP routes (party + admin) + router wiring

**Files:** `internal/disputes/handler.go`, `internal/disputes/admin.go`, `internal/httpapi/router.go`.

**Party handler:**
```go
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/orders", guard...)
	g.Post("/:id/disputes", h.raise)
	g.Get("/:id/disputes", h.list)
}
```
- `raise`: parse `{reason, detail}`; `h.svc.Raise(ctx, id, middleware.CompanyID(c), reason, detail)`;
  map `ValidationError`→400, `ConflictError`→409, `ErrNotFound`→404; 200 `{dispute}`.
- `list`: `h.svc.ListForOrder(...)`; 404 non-party; `{disputes}`.

**Admin handler** (`admin.go`):
```go
func (h *AdminHandler) RegisterAdmin(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1", adminGuard)
	g.Get("/disputes", h.list)
	g.Post("/disputes/:id/resolve", h.resolve)
}
```
- `list`: `status := c.Query("status")`; `h.svc.ListForAdmin(ctx, status)`; `{disputes}`.
- `resolve`: parse `{outcome, order_to, resolution}`; `adminID := middleware.AdminID(c)`;
  `h.svc.Resolve(ctx, c.Params("id"), adminID, outcome, orderTo, resolution)`;
  map `ValidationError`→400, `ConflictError`→409, `ErrNotFound`→404; 200 `{dispute}`.

- [ ] **Step 1: Light handler test** (fake guard for the party route; for admin, a fake adminGuard that
  sets the AdminID local — check `pkg/middleware/admin.go` for the locals key). Assert raise→200,
  raise-on-completed→409. If awkward, keep service tests authoritative + a compile/smoke test.
- [ ] **Step 2–4: fail → implement → pass.**
- [ ] **Step 5: Wire router** — build once and register:
```go
	disputesSvc := disputes.NewService(q, cfg.Pool, notifier, cfg.Logger)
	disputes.NewHandler(disputesSvc).Register(f, guard...)
```
  and inside the admin block (`if cfg.AdminJWTSecret != ""`):
```go
	disputes.NewAdminHandler(disputesSvc).RegisterAdmin(f, adminGuard)
```
- [ ] **Step 6:** `go build ./... && go test ./internal/disputes/ ./internal/orders/ -count=1 -v`.
- [ ] **Step 7: Commit** — `feat(disputes): party + admin HTTP routes + router wiring`.

---

## Self-Review checklist

- `go build ./...` clean; full suite `go test ./... -p 1 -count=1` (migrations ≤022) — 0 failures.
- Migration 022 round-trips (down/up); partial-unique blocks a 2nd open dispute per order.
- A party cannot transition a `disputed` order (Task 1 guard) — only admin resolution moves it.
- Raise is atomic (dispute + order→disputed + event) and rejects non-party (404) / non-active-state (409) / bad reason (400).
- Resolve is atomic (dispute + order→terminal + event + audit_logs) and idempotent-safe (2nd resolve → 409).
- Audit row written with `action="dispute.resolved"`, `target_id=<disputeID>`, and outcome detail.
- Both parties notified on resolution; counterparty notified on open.
- No dispute test creates `%widgets%` supplier_profiles rows.

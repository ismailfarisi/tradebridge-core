# Sprint 8 — Admin Console & Support Operations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give an operator one internal console to manage companies (search/view/suspend), work the existing review queues, browse a unified audit log, and impersonate a user read-only — all audited.

**Architecture:** All new routes mount inside the existing `PlatformAdminGuard` realm (`/admin/v1/*`, fail-closed on `ADMIN_JWT_SECRET`). One Core migration adds `companies.status`. A new `internal/adminconsole` package owns company management; a new `internal/audit` package owns the audit helper + viewer. Two small middleware additions (`RequireActiveCompany`, `EnforceReadOnly`) and impersonation token issuance extend the existing tenant-auth rails. Web adds a unified `/admin/*` console over the existing `AdminShell`.

**Tech Stack:** Go 1.x / Fiber v2 / pgx v5 / sqlc / golang-migrate; Next.js 16 / React 19 / TS strict / vitest (node env, lib-only).

## Global Constraints

- All admin routes stay under `/admin/v1/*` behind the existing `PlatformAdminGuard`. Nothing new is publicly reachable.
- `companies.status` ∈ exactly `{active, suspended}` (DB CHECK).
- Impersonation token TTL = **30 minutes**, carries `ReadOnly=true`; `EnforceReadOnly` returns HTTP **403** for any method other than GET/HEAD.
- Every admin mutation writes exactly one `audit_logs` row: `actor = "admin:<email>"`, `company_id = <target company>`. Action names are exactly: `company.suspend`, `company.unsuspend`, `company.verification`, `dispute.resolve`, `report.resolve`, `support.impersonate`.
- Audit writes are best-effort: a failure is logged (zerolog `Warn`), never returned — auditing never fails the operation it records.
- Tests use `tradebridge_core_test`, NEVER live `tradebridge_core`. Full Go suite runs with `-p 1` (shared-DB ordering flake).
- Web vitest is node-env, `lib/**/*.test.ts` only. Testable logic lives in `lib`, not components.
- Work merges to `master` **locally, never pushed**. Commit trailers on every commit:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01JrKi3VYFWQJPRvZGwScMqh`.
- Regenerate sqlc after any `pkg/db/query/*.sql` change: `make sqlc` (or `sqlc generate`). Never hand-edit `pkg/db/sqlc/*`.

## File Structure

- `pkg/db/migrations/028_company_status.{up,down}.sql` — new column + index.
- `pkg/db/query/companies.sql` — add `GetCompanyStatus`, `SetCompanyStatus`, `SearchCompaniesForAdmin`, `AdminCompanyCounts`.
- `pkg/db/query/audit.sql` — new: `ListAuditLogs`.
- `pkg/middleware/tenant.go` — add `RequireActiveCompany`, `EnforceReadOnly`.
- `pkg/middleware/auth.go` — add `ReadOnly`, `ActingAdmin` to `Claims`.
- `internal/identity/token.go` — add `IssueImpersonation`.
- `internal/identity/handler.go` — reject suspended company at login.
- `internal/audit/{audit.go,handler.go,audit_test.go}` — new package.
- `internal/adminconsole/{service.go,handler.go,handler_test.go}` — new package.
- `internal/impersonation/{handler.go,handler_test.go}` — new package (admin impersonate endpoint).
- `internal/trust/admin.go`, `internal/trust/reports.go`, `internal/disputes/admin.go` — backfill audit.
- `internal/httpapi/router.go` — wire guard additions + new handlers.
- `web/lib/admin.ts` + `web/lib/admin.test.ts`; `web/components/AdminShell.tsx`; new pages under `web/app/admin/`.
- `test/integration/e2e_08_admin_ops_test.go` — E2E #8.

---

### Task 1: Company status + suspend enforcement (migration 028)

**Files:**
- Create: `pkg/db/migrations/028_company_status.up.sql`, `pkg/db/migrations/028_company_status.down.sql`
- Modify: `pkg/db/query/companies.sql` (add two queries)
- Modify: `pkg/middleware/tenant.go` (add `RequireActiveCompany`)
- Test: `pkg/middleware/tenant_test.go` (add `TestRequireActiveCompany`)
- Modify: `internal/identity/handler.go` (login rejects suspended)
- Modify: `internal/httpapi/router.go` (add `RequireActiveCompany` to the tenant guard slice)

**Interfaces:**
- Produces: `q.GetCompanyStatus(ctx, id string) (string, error)`, `q.SetCompanyStatus(ctx, SetCompanyStatusParams{ID, Status}) error`, `middleware.RequireActiveCompany(status func(context.Context, string) (string, error)) fiber.Handler`. `companies.status` column now exists so `GetCompanyByID` (SELECT *) returns `Status`.

- [ ] **Step 1: Write the migration**

Create `pkg/db/migrations/028_company_status.up.sql`:
```sql
-- Company lifecycle status. 'suspended' companies cannot log in and are blocked
-- on every authed request by RequireActiveCompany (admin suspend action).
ALTER TABLE companies
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active', 'suspended'));

CREATE INDEX idx_companies_suspended ON companies (status) WHERE status = 'suspended';
```

Create `pkg/db/migrations/028_company_status.down.sql`:
```sql
DROP INDEX IF EXISTS idx_companies_suspended;
ALTER TABLE companies DROP COLUMN IF EXISTS status;
```

- [ ] **Step 2: Add the queries** to `pkg/db/query/companies.sql`:
```sql
-- name: GetCompanyStatus :one
SELECT status FROM companies WHERE id = $1;

-- name: SetCompanyStatus :exec
UPDATE companies SET status = $2 WHERE id = $1;
```

- [ ] **Step 3: Regenerate sqlc** — Run: `make sqlc` (or `sqlc generate`). Expected: `pkg/db/sqlc` compiles with `GetCompanyStatus`, `SetCompanyStatus`, and `Company.Status`.

- [ ] **Step 4: Write the failing middleware test** in `pkg/middleware/tenant_test.go`:
```go
package middleware

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRequireActiveCompany(t *testing.T) {
	newApp := func(status string, statusErr error) *fiber.App {
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error { c.Locals(LocalCompanyID, "co_1"); return c.Next() })
		app.Use(RequireActiveCompany(func(context.Context, string) (string, error) {
			return status, statusErr
		}))
		app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })
		return app
	}

	t.Run("active passes", func(t *testing.T) {
		res, _ := newApp("active", nil).Test(httptest.NewRequest("GET", "/x", nil))
		if res.StatusCode != 200 {
			t.Fatalf("want 200, got %d", res.StatusCode)
		}
	})
	t.Run("suspended blocked 403", func(t *testing.T) {
		res, _ := newApp("suspended", nil).Test(httptest.NewRequest("GET", "/x", nil))
		if res.StatusCode != 403 {
			t.Fatalf("want 403, got %d", res.StatusCode)
		}
	})
}
```

- [ ] **Step 5: Run test to verify it fails** — Run: `go test ./pkg/middleware/ -run TestRequireActiveCompany`. Expected: FAIL (`RequireActiveCompany` undefined).

- [ ] **Step 6: Implement `RequireActiveCompany`** in `pkg/middleware/tenant.go` (append after `RequireRole`):
```go
// RequireActiveCompany must run after TenantGuard. It rejects requests from a
// suspended company so an admin suspend takes effect immediately, not at token
// expiry. `status` is injected (typically q.GetCompanyStatus) so this package
// stays free of a sqlc dependency. One indexed PK lookup per authed request.
func RequireActiveCompany(status func(ctx context.Context, companyID string) (string, error)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		st, err := status(c.Context(), CompanyID(c))
		if err != nil {
			return err
		}
		if st == "suspended" {
			return fiber.NewError(fiber.StatusForbidden, "company suspended")
		}
		return c.Next()
	}
}
```
Add `"context"` to the imports of `pkg/middleware/tenant.go`.

- [ ] **Step 7: Run test to verify it passes** — Run: `go test ./pkg/middleware/ -run TestRequireActiveCompany`. Expected: PASS.

- [ ] **Step 8: Reject suspended company at login.** In `internal/identity/handler.go` `login`, after the password check passes and before `return h.issue(...)`, add:
```go
	if st, err := h.q.GetCompanyStatus(c.Context(), user.CompanyID); err == nil && st == "suspended" {
		return fiber.NewError(fiber.StatusForbidden, "company suspended")
	}
```
(`user.CompanyID` is on the `GetUserByEmail` row.)

- [ ] **Step 9: Wire `RequireActiveCompany` into the tenant guard slice.** In `internal/httpapi/router.go`, change the guard definition from:
```go
	guard := []fiber.Handler{
		middleware.JWT(cfg.JWTSecret),
		middleware.TenantGuard(),
	}
```
to:
```go
	guard := []fiber.Handler{
		middleware.JWT(cfg.JWTSecret),
		middleware.TenantGuard(),
		middleware.RequireActiveCompany(q.GetCompanyStatus),
	}
```

- [ ] **Step 10: Build + full package tests** — Run: `go build ./... && go test ./pkg/middleware/ ./internal/identity/`. Expected: PASS. (End-to-end login-403 + authed-403 are asserted in Task 6's E2E.)

- [ ] **Step 11: Commit**
```bash
git add pkg/db/migrations/028_company_status.up.sql pkg/db/migrations/028_company_status.down.sql pkg/db/query/companies.sql pkg/db/sqlc pkg/middleware/tenant.go pkg/middleware/tenant_test.go internal/identity/handler.go internal/httpapi/router.go
git commit -m "feat(core): company suspend status + immediate enforcement (S8)"
```

---

### Task 2: Audit helper + viewer + backfill

**Files:**
- Create: `internal/audit/audit.go`, `internal/audit/handler.go`, `internal/audit/audit_test.go`
- Create: `pkg/db/query/audit.sql` (add `ListAuditLogs`)
- Modify: `internal/trust/admin.go` (audit verification decision)
- Modify: `internal/trust/reports.go` (audit report resolve)
- Modify: `internal/disputes/admin.go` (audit dispute resolve)
- Modify: `internal/httpapi/router.go` (construct `*audit.Auditor`, thread it in, register viewer)

**Interfaces:**
- Consumes: `sqlc.CreateAuditLogParams{CompanyID string, Actor string, Action string, TargetID *string, Detail []byte}`, `sqlc.AuditLog`, `middleware.AdminEmail(c) string`.
- Produces: `audit.New(q auditWriter, logger zerolog.Logger) *audit.Auditor`; method `(*Auditor).Admin(ctx, adminEmail, action, targetCompanyID string, detail map[string]any)`; `audit.NewAdminHandler(q *sqlc.Queries).Register(app, adminGuard)` serving `GET /admin/v1/audit`; `q.ListAuditLogs(ctx, ListAuditLogsParams{CompanyID, Actor, Action *string, Lim int32}) ([]sqlc.AuditLog, error)`.

- [ ] **Step 1: Add the viewer query** to a new `pkg/db/query/audit.sql`:
```sql
-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE (sqlc.narg(company_id)::text IS NULL OR company_id = sqlc.narg(company_id))
  AND (sqlc.narg(actor)::text      IS NULL OR actor = sqlc.narg(actor))
  AND (sqlc.narg(action)::text     IS NULL OR action = sqlc.narg(action))
ORDER BY created_at DESC
LIMIT sqlc.arg(lim);
```

- [ ] **Step 2: Regenerate sqlc** — Run: `make sqlc`. Expected: `ListAuditLogs` + `ListAuditLogsParams{CompanyID *string, Actor *string, Action *string, Lim int32}`.

- [ ] **Step 3: Write the failing audit-helper test** in `internal/audit/audit_test.go`:
```go
package audit

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type fakeWriter struct {
	last sqlc.CreateAuditLogParams
	n    int
	err  error
}

func (f *fakeWriter) CreateAuditLog(_ context.Context, arg sqlc.CreateAuditLogParams) (sqlc.AuditLog, error) {
	f.last = arg
	f.n++
	return sqlc.AuditLog{}, f.err
}

func TestAdminWritesRow(t *testing.T) {
	fw := &fakeWriter{}
	New(fw, zerolog.Nop()).Admin(context.Background(), "ops@x.com", "company.suspend", "co_1", map[string]any{"reason": "fraud"})
	if fw.n != 1 {
		t.Fatalf("want 1 write, got %d", fw.n)
	}
	if fw.last.Actor != "admin:ops@x.com" {
		t.Fatalf("actor = %q", fw.last.Actor)
	}
	if fw.last.CompanyID != "co_1" || fw.last.Action != "company.suspend" {
		t.Fatalf("bad row: %+v", fw.last)
	}
	if fw.last.TargetID == nil || *fw.last.TargetID != "co_1" {
		t.Fatalf("target = %v", fw.last.TargetID)
	}
}

func TestAdminSwallowsWriteError(t *testing.T) {
	fw := &fakeWriter{err: context.DeadlineExceeded}
	// Must not panic and must not surface the error (best-effort).
	New(fw, zerolog.Nop()).Admin(context.Background(), "ops@x.com", "x", "co_1", nil)
	if fw.n != 1 {
		t.Fatalf("want attempted write")
	}
}
```

- [ ] **Step 4: Run test to verify it fails** — Run: `go test ./internal/audit/`. Expected: FAIL (package/`New` undefined).

- [ ] **Step 5: Implement the helper** in `internal/audit/audit.go`:
```go
// Package audit writes and reads the append-only audit_logs trail. The trail is
// shared: the gateway writes agent-command rows; Admin writes platform-admin
// action rows. The viewer (handler.go) reads both.
package audit

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type auditWriter interface {
	CreateAuditLog(ctx context.Context, arg sqlc.CreateAuditLogParams) (sqlc.AuditLog, error)
}

// Auditor records platform-admin actions. Construct one per process.
type Auditor struct {
	q      auditWriter
	logger zerolog.Logger
}

// New builds an Auditor. *sqlc.Queries satisfies auditWriter.
func New(q auditWriter, logger zerolog.Logger) *Auditor {
	return &Auditor{q: q, logger: logger}
}

// Admin writes one append-only row for a platform-admin action against a target
// company. Best-effort: a write failure is logged, never returned.
func (a *Auditor) Admin(ctx context.Context, adminEmail, action, targetCompanyID string, detail map[string]any) {
	payload, err := json.Marshal(detail)
	if err != nil || payload == nil {
		payload = []byte("{}")
	}
	target := targetCompanyID
	if _, err := a.q.CreateAuditLog(ctx, sqlc.CreateAuditLogParams{
		CompanyID: targetCompanyID,
		Actor:     "admin:" + adminEmail,
		Action:    action,
		TargetID:  &target,
		Detail:    payload,
	}); err != nil {
		a.logger.Warn().Err(err).Str("action", action).Msg("admin audit write failed")
	}
}
```

- [ ] **Step 6: Run test to verify it passes** — Run: `go test ./internal/audit/`. Expected: PASS.

- [ ] **Step 7: Implement the viewer handler** in `internal/audit/handler.go`:
```go
package audit

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// AdminHandler serves the read-only audit-log viewer.
type AdminHandler struct {
	q *sqlc.Queries
}

func NewAdminHandler(q *sqlc.Queries) *AdminHandler {
	return &AdminHandler{q: q}
}

// Register mounts GET /admin/v1/audit behind the platform-admin guard.
func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler) {
	app.Get("/admin/v1/audit", adminGuard, h.list)
}

func (h *AdminHandler) list(c *fiber.Ctx) error {
	optional := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	limit := 100
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && n > 0 {
		if n > 500 {
			n = 500
		}
		limit = n
	}
	rows, err := h.q.ListAuditLogs(c.Context(), sqlc.ListAuditLogsParams{
		CompanyID: optional(c.Query("company_id")),
		Actor:     optional(c.Query("actor")),
		Action:    optional(c.Query("action")),
		Lim:       int32(limit),
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"entries": rows, "count": len(rows)})
}
```

- [ ] **Step 8: Backfill audit into verification decision.** In `internal/trust/admin.go`: add an `auditor *audit.Auditor` field to `AdminHandler`, update `NewAdminHandler(svc *Service, q *sqlc.Queries, auditor *audit.Auditor)`, and at the end of `setStatus` (after `GetCompanyVerification` succeeds, before `return c.JSON`) add:
```go
	h.auditor.Admin(c.Context(), middleware.AdminEmail(c), "company.verification", companyID,
		map[string]any{"status": req.Status, "reason": req.Reason})
```
Add imports `"github.com/tradebridge/core/internal/audit"` and `"github.com/tradebridge/core/pkg/middleware"` to `internal/trust/admin.go`.

- [ ] **Step 9: Backfill audit into report resolve.** In `internal/trust/reports.go`: add `auditor *audit.Auditor` to `ReportsHandler`, update `NewReportsHandler(q *sqlc.Queries, auditor *audit.Auditor)`, and in `resolve`, after `ResolveReport` succeeds, before `return c.JSON(rep)`:
```go
	h.auditor.Admin(c.Context(), middleware.AdminEmail(c), "report.resolve", rep.ReportedCompanyID,
		map[string]any{"report_id": rep.ID, "status": rep.Status})
```
Add the `audit` import (middleware is already imported).

- [ ] **Step 10: Backfill audit into dispute resolve.** In `internal/disputes/admin.go`: add `auditor *audit.Auditor` to `AdminHandler`, update `NewAdminHandler(svc *Service, auditor *audit.Auditor)`, and in `resolve`, after `svc.Resolve` succeeds, before `return c.JSON`:
```go
	h.auditor.Admin(c.Context(), middleware.AdminEmail(c), "dispute.resolve", dispute.RaisedByCompanyID,
		map[string]any{"dispute_id": dispute.ID, "outcome": req.Outcome})
```
Add the `audit` import (middleware already imported).

- [ ] **Step 11: Wire the router.** In `internal/httpapi/router.go`, inside the `if cfg.AdminJWTSecret != ""` block, before the handler registrations, construct the auditor and pass it through; register the viewer:
```go
		auditor := audit.New(q, cfg.Logger)
		// ...existing admin.NewHandler / settings / entitlements registrations...
		trust.NewAdminHandler(trustSvc, q, auditor).Register(f, adminGuard)
		audit.NewAdminHandler(q).Register(f, adminGuard)
		disputes.NewAdminHandler(disputesSvc, auditor).RegisterAdmin(f, adminGuard)
```
Update the `reports` handler construction (find where `NewReportsHandler` is called for the admin queue) to pass `auditor`, and add `"github.com/tradebridge/core/internal/audit"` to the router imports. Update any other `NewReportsHandler` / `trust.NewAdminHandler` / `disputes.NewAdminHandler` call sites (including tests) to the new signatures.

- [ ] **Step 12: Fix the existing handler tests** for the new constructor signatures. In `internal/trust/admin_test.go`, `internal/trust/reports_test.go`, `internal/disputes/handler_test.go` (and any other callers), pass `audit.New(<test queries>, zerolog.Nop())` (or a small fake) as the new `auditor` argument. The audit rows land in the same test DB; existing assertions are unaffected.

- [ ] **Step 13: Build + test** — Run: `go build ./... && go test ./internal/audit/ ./internal/trust/ ./internal/disputes/`. Expected: PASS.

- [ ] **Step 14: Commit**
```bash
git add pkg/db/query/audit.sql pkg/db/sqlc internal/audit internal/trust/admin.go internal/trust/reports.go internal/disputes/admin.go internal/httpapi/router.go internal/trust/admin_test.go internal/trust/reports_test.go internal/disputes/handler_test.go
git commit -m "feat(core): audit helper, viewer, and admin-action backfill (S8)"
```

---

### Task 3: Admin companies management API

**Files:**
- Create: `internal/adminconsole/service.go`, `internal/adminconsole/handler.go`, `internal/adminconsole/handler_test.go`
- Modify: `pkg/db/query/companies.sql` (add `SearchCompaniesForAdmin`, `AdminCompanyCounts`)
- Modify: `internal/httpapi/router.go` (register the handler)

**Interfaces:**
- Consumes: `q.GetCompanyByID`, `q.GetCompanyStatus`, `q.SetCompanyStatus`, `q.GetCompanyVerification`, `q.ListUsersByCompany`, `entitlements.Service.PlanCode(ctx, companyID) string`, `audit.Auditor`, `middleware.AdminEmail`.
- Produces: `adminconsole.NewHandler(q *sqlc.Queries, ent *entitlements.Service, auditor *audit.Auditor).Register(app, adminGuard)` serving `GET /admin/v1/companies`, `GET /admin/v1/companies/:id`, `POST /admin/v1/companies/:id/suspend`, `POST /admin/v1/companies/:id/unsuspend`.

- [ ] **Step 1: Add the queries** to `pkg/db/query/companies.sql`:
```sql
-- name: SearchCompaniesForAdmin :many
SELECT id, name, type, status, created_at
FROM companies
WHERE (sqlc.narg(q)::text IS NULL
       OR name ILIKE '%' || sqlc.narg(q) || '%'
       OR id = sqlc.narg(q))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY created_at DESC
LIMIT sqlc.arg(lim);

-- name: AdminCompanyCounts :one
SELECT
  (SELECT COUNT(*) FROM rfqs   WHERE buyer_company_id  = $1) AS rfqs,
  (SELECT COUNT(*) FROM quotes WHERE seller_company_id = $1) AS quotes,
  (SELECT COUNT(*) FROM orders WHERE buyer_company_id  = $1 OR seller_company_id = $1) AS orders;
```
NOTE: verify the exact column names (`rfqs.buyer_company_id`, `quotes.seller_company_id`, `orders.buyer_company_id/seller_company_id`) against the current schema in `pkg/db/migrations/` before regenerating; adjust to match. If a table/column differs, use the real name — do not invent.

- [ ] **Step 2: Regenerate sqlc** — Run: `make sqlc`. Expected: `SearchCompaniesForAdmin`, `AdminCompanyCounts` generated.

- [ ] **Step 3: Write the service** in `internal/adminconsole/service.go`:
```go
// Package adminconsole owns platform-admin company management: search, detail,
// and suspend/unsuspend. It mounts under the existing PlatformAdminGuard realm.
package adminconsole

import (
	"context"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Service struct {
	q   *sqlc.Queries
	ent *entitlements.Service
}

func NewService(q *sqlc.Queries, ent *entitlements.Service) *Service {
	return &Service{q: q, ent: ent}
}

type CompanyDetail struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Type               string        `json:"type"`
	Status             string        `json:"status"`
	VerificationStatus string        `json:"verification_status"`
	PlanCode           string        `json:"plan_code"`
	Users              []sqlc.User   `json:"users"`
	Counts             CompanyCounts `json:"counts"`
}

type CompanyCounts struct {
	RFQs   int64 `json:"rfqs"`
	Quotes int64 `json:"quotes"`
	Orders int64 `json:"orders"`
}

func (s *Service) Detail(ctx context.Context, id string) (CompanyDetail, error) {
	co, err := s.q.GetCompanyByID(ctx, id)
	if err != nil {
		return CompanyDetail{}, err
	}
	d := CompanyDetail{ID: co.ID, Name: co.Name, Type: co.Type, Status: co.Status, PlanCode: s.ent.PlanCode(ctx, id)}
	if v, err := s.q.GetCompanyVerification(ctx, id); err == nil {
		d.VerificationStatus = v.VerificationStatus
	}
	if users, err := s.q.ListUsersByCompany(ctx, id); err == nil {
		d.Users = users
	}
	if cnt, err := s.q.AdminCompanyCounts(ctx, id); err == nil {
		d.Counts = CompanyCounts{RFQs: cnt.Rfqs, Quotes: cnt.Quotes, Orders: cnt.Orders}
	}
	return d, nil
}
```
NOTE: confirm `GetCompanyVerification`'s returned field is `.VerificationStatus` and `ListUsersByCompany`'s row type is `sqlc.User`; adjust to the generated types if they differ.

- [ ] **Step 4: Write the failing handler test** in `internal/adminconsole/handler_test.go` following the package's existing admin-handler test pattern (see `internal/trust/admin_test.go` for the `doJSON` + test-app + admin-identity helpers). Cover: (a) search returns a seeded company; (b) detail returns status/plan/counts; (c) `POST .../suspend` flips status to `suspended` and writes one `company.suspend` audit row; (d) `POST .../unsuspend` flips it back. Use the shared integration harness that builds an app with `PlatformAdminGuard` stubbed to inject an admin email (as `stubAdminIdentity` does in disputes tests). Assert the audit row via `q.ListAuditLogs`.

- [ ] **Step 5: Run test to verify it fails** — Run: `go test ./internal/adminconsole/`. Expected: FAIL (handler undefined).

- [ ] **Step 6: Implement the handler** in `internal/adminconsole/handler.go`:
```go
package adminconsole

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/audit"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	svc     *Service
	q       *sqlc.Queries
	auditor *audit.Auditor
}

func NewHandler(q *sqlc.Queries, ent *entitlementsService, auditor *audit.Auditor) *Handler {
	return &Handler{svc: NewService(q, ent), q: q, auditor: auditor}
}

func (h *Handler) Register(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1/companies", adminGuard)
	g.Get("/", h.search)
	g.Get("/:id", h.detail)
	g.Post("/:id/suspend", h.suspend)
	g.Post("/:id/unsuspend", h.unsuspend)
}

func (h *Handler) search(c *fiber.Ctx) error {
	optional := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	limit := 50
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && n > 0 {
		if n > 200 {
			n = 200
		}
		limit = n
	}
	rows, err := h.q.SearchCompaniesForAdmin(c.Context(), sqlc.SearchCompaniesForAdminParams{
		Q:      optional(c.Query("q")),
		Status: optional(c.Query("status")),
		Lim:    int32(limit),
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"companies": rows, "count": len(rows)})
}

func (h *Handler) detail(c *fiber.Ctx) error {
	d, err := h.svc.Detail(c.Context(), c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "company not found")
	}
	return c.JSON(d)
}

type suspendReq struct {
	Reason string `json:"reason"`
}

func (h *Handler) setStatus(c *fiber.Ctx, status, action string) error {
	id := c.Params("id")
	var req suspendReq
	_ = c.BodyParser(&req)
	if err := h.q.SetCompanyStatus(c.Context(), sqlc.SetCompanyStatusParams{ID: id, Status: status}); err != nil {
		return err
	}
	h.auditor.Admin(c.Context(), middleware.AdminEmail(c), action, id, map[string]any{"reason": req.Reason})
	return c.JSON(fiber.Map{"company_id": id, "status": status})
}

func (h *Handler) suspend(c *fiber.Ctx) error   { return h.setStatus(c, "suspended", "company.suspend") }
func (h *Handler) unsuspend(c *fiber.Ctx) error { return h.setStatus(c, "active", "company.unsuspend") }
```
Replace `entitlementsService` with the real `*entitlements.Service` type — either import it directly in `handler.go` (add `"github.com/tradebridge/core/internal/entitlements"` and use `*entitlements.Service`) or keep the `NewService` signature. Use `*entitlements.Service` directly; the placeholder alias is only to show the shape.

- [ ] **Step 7: Register in the router.** In `internal/httpapi/router.go` admin block:
```go
		adminconsole.NewHandler(q, entSvc, auditor).Register(f, adminGuard)
```
Add `"github.com/tradebridge/core/internal/adminconsole"` to imports.

- [ ] **Step 8: Run test to verify it passes** — Run: `go test ./internal/adminconsole/`. Expected: PASS.

- [ ] **Step 9: Build** — Run: `go build ./...`. Expected: clean.

- [ ] **Step 10: Commit**
```bash
git add pkg/db/query/companies.sql pkg/db/sqlc internal/adminconsole internal/httpapi/router.go
git commit -m "feat(core): admin companies management API — search/detail/suspend (S8)"
```

---

### Task 4: Read-only support impersonation

**Files:**
- Modify: `pkg/middleware/auth.go` (add `ReadOnly`, `ActingAdmin` to `Claims`)
- Modify: `pkg/middleware/tenant.go` (add `EnforceReadOnly`)
- Test: `pkg/middleware/tenant_test.go` (add `TestEnforceReadOnly`)
- Modify: `internal/identity/token.go` (add `IssueImpersonation`)
- Create: `internal/impersonation/handler.go`, `internal/impersonation/handler_test.go`
- Modify: `internal/httpapi/router.go` (add `EnforceReadOnly` to guard slice; register impersonation handler; needs a company-user lookup query)
- Possibly add: `pkg/db/query/users.sql` `GetOwnerUserByCompany` (if not present)

**Interfaces:**
- Consumes: `middleware.Claims`, `identity.TokenService`, `q.ListUsersByCompany` (or a new `GetOwnerUserByCompany`), `audit.Auditor`, `middleware.AdminEmail`.
- Produces: `Claims.ReadOnly bool`, `Claims.ActingAdmin string`; `identity.TokenService.IssueImpersonation(companyID, userID, role, adminEmail string) (string, error)`; `middleware.EnforceReadOnly() fiber.Handler`; `POST /admin/v1/impersonate`.

- [ ] **Step 1: Extend `Claims`** in `pkg/middleware/auth.go`:
```go
type Claims struct {
	CompanyID   string `json:"company_id"`
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	ReadOnly    bool   `json:"ro,omitempty"`
	ActingAdmin string `json:"imp,omitempty"`
	jwt.RegisteredClaims
}
```
(Existing tokens omit `ro`/`imp`; they deserialize to zero values — backward compatible.)

- [ ] **Step 2: Write the failing `EnforceReadOnly` test** in `pkg/middleware/tenant_test.go`:
```go
func TestEnforceReadOnly(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &Claims{CompanyID: "co_1", ReadOnly: true})
		return c.Next()
	})
	app.Use(EnforceReadOnly())
	app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Post("/x", func(c *fiber.Ctx) error { return c.SendString("wrote") })

	get, _ := app.Test(httptest.NewRequest("GET", "/x", nil))
	if get.StatusCode != 200 {
		t.Fatalf("GET want 200, got %d", get.StatusCode)
	}
	post, _ := app.Test(httptest.NewRequest("POST", "/x", nil))
	if post.StatusCode != 403 {
		t.Fatalf("POST want 403, got %d", post.StatusCode)
	}
}
```

- [ ] **Step 3: Run test to verify it fails** — Run: `go test ./pkg/middleware/ -run TestEnforceReadOnly`. Expected: FAIL (`EnforceReadOnly` undefined).

- [ ] **Step 4: Implement `EnforceReadOnly`** in `pkg/middleware/tenant.go`:
```go
// EnforceReadOnly must run after JWT (which stashes *Claims). When the token is
// a read-only impersonation session, it blocks any state-changing method so a
// support operator can look but never act. GET/HEAD pass.
func EnforceReadOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*Claims)
		if ok && claims.ReadOnly {
			switch c.Method() {
			case fiber.MethodGet, fiber.MethodHead:
			default:
				return fiber.NewError(fiber.StatusForbidden, "read-only session")
			}
		}
		return c.Next()
	}
}
```

- [ ] **Step 5: Run test to verify it passes** — Run: `go test ./pkg/middleware/ -run TestEnforceReadOnly`. Expected: PASS.

- [ ] **Step 6: Add `IssueImpersonation`** to `internal/identity/token.go`:
```go
// ImpersonationTTL bounds a support impersonation session.
const ImpersonationTTL = 30 * time.Minute

// IssueImpersonation mints a short-TTL, read-only company token for support. The
// token carries ReadOnly=true (EnforceReadOnly blocks writes) and records the
// acting admin. TTL is fixed at ImpersonationTTL, independent of login TTL.
func (s *TokenService) IssueImpersonation(companyID, userID, role, adminEmail string) (string, error) {
	claims := &middleware.Claims{
		CompanyID:   companyID,
		UserID:      userID,
		Role:        role,
		ReadOnly:    true,
		ActingAdmin: adminEmail,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ImpersonationTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}
```

- [ ] **Step 7: Ensure a company→user lookup exists.** `ListUsersByCompany` already returns the company's users; the impersonation handler will pick the first `owner` (fallback: first user). No new query required. (If you prefer a dedicated query, add `GetOwnerUserByCompany` to `pkg/db/query/users.sql` selecting `WHERE company_id = $1 ORDER BY (role = 'owner') DESC, created_at ASC LIMIT 1` and regenerate — optional.)

- [ ] **Step 8: Write the failing impersonation handler test** in `internal/impersonation/handler_test.go`, following the shared admin-handler test harness (as in Task 3). Seed a company with an owner user. Assert: `POST /admin/v1/impersonate {company_id}` → 200 with a non-empty `token`, `user_id` set; a `support.impersonate` audit row exists for the company; parsing the returned token yields `ReadOnly==true` and `ActingAdmin` == the admin email. Company with no users → 404.

- [ ] **Step 9: Run test to verify it fails** — Run: `go test ./internal/impersonation/`. Expected: FAIL.

- [ ] **Step 10: Implement the handler** in `internal/impersonation/handler.go`:
```go
// Package impersonation issues scoped, read-only, audited support tokens that
// let an operator view a company exactly as its users would — but never write.
package impersonation

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/audit"
	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	q       *sqlc.Queries
	tokens  *identity.TokenService
	auditor *audit.Auditor
}

func NewHandler(q *sqlc.Queries, tokens *identity.TokenService, auditor *audit.Auditor) *Handler {
	return &Handler{q: q, tokens: tokens, auditor: auditor}
}

func (h *Handler) Register(app *fiber.App, adminGuard fiber.Handler) {
	app.Post("/admin/v1/impersonate", adminGuard, h.impersonate)
}

type impersonateReq struct {
	CompanyID string `json:"company_id"`
}

func (h *Handler) impersonate(c *fiber.Ctx) error {
	var req impersonateReq
	if err := c.BodyParser(&req); err != nil || req.CompanyID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "company_id required")
	}
	users, err := h.q.ListUsersByCompany(c.Context(), req.CompanyID)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return fiber.NewError(fiber.StatusNotFound, "company has no users")
	}
	target := users[0]
	for _, u := range users {
		if u.Role == "owner" {
			target = u
			break
		}
	}
	adminEmail := middleware.AdminEmail(c)
	tok, err := h.tokens.IssueImpersonation(req.CompanyID, target.ID, target.Role, adminEmail)
	if err != nil {
		return err
	}
	h.auditor.Admin(c.Context(), adminEmail, "support.impersonate", req.CompanyID,
		map[string]any{"user_id": target.ID})
	return c.JSON(fiber.Map{
		"token":      tok,
		"company_id": req.CompanyID,
		"user_id":    target.ID,
		"expires_in": int(identity.ImpersonationTTL.Seconds()),
	})
}
```

- [ ] **Step 11: Wire the router.** In `internal/httpapi/router.go`: add `middleware.EnforceReadOnly()` to the tenant `guard` slice (after `RequireActiveCompany`), and in the admin block register the impersonation handler (it needs `tokens` — the `identity.NewTokenService` value already built as `tokens`):
```go
		impersonation.NewHandler(q, tokens, auditor).Register(f, adminGuard)
```
Add `"github.com/tradebridge/core/internal/impersonation"` to imports. Final guard slice:
```go
	guard := []fiber.Handler{
		middleware.JWT(cfg.JWTSecret),
		middleware.TenantGuard(),
		middleware.RequireActiveCompany(q.GetCompanyStatus),
		middleware.EnforceReadOnly(),
	}
```

- [ ] **Step 12: Run tests + build** — Run: `go test ./pkg/middleware/ ./internal/identity/ ./internal/impersonation/ && go build ./...`. Expected: PASS.

- [ ] **Step 13: Commit**
```bash
git add pkg/middleware/auth.go pkg/middleware/tenant.go pkg/middleware/tenant_test.go internal/identity/token.go internal/impersonation internal/httpapi/router.go
git commit -m "feat(core): read-only support impersonation (scoped, audited) (S8)"
```

---

### Task 5: Unified admin console web

**Files:**
- Modify: `web/lib/admin.ts` (typed clients + a pure helper) + `web/lib/admin.test.ts`
- Modify: `web/components/AdminShell.tsx` (nav + wider main for tables)
- Create: `web/app/admin/companies/page.tsx`, `web/app/admin/companies/[id]/page.tsx`, `web/app/admin/verifications/page.tsx`, `web/app/admin/reports/page.tsx`, `web/app/admin/audit/page.tsx`
- Create: `web/lib/impersonation.ts` (hand-off helper) + optional read-only banner wiring

**Interfaces:**
- Consumes: `/admin/v1/companies`, `/admin/v1/companies/:id`, `/admin/v1/companies/:id/{suspend,unsuspend}`, `/admin/v1/verifications`, `/admin/v1/companies/:id/verification`, `/admin/v1/reports`, `/admin/v1/reports/:id`, `/admin/v1/audit`, `/admin/v1/impersonate`.
- Produces: typed clients in `lib/admin.ts`; console pages.

- [ ] **Step 1: Add typed clients + a pure helper** to `web/lib/admin.ts` (mirror the existing `req<T>` pattern already in the file):
```ts
export type AdminCompany = { id: string; name: string; type: string; status: string; created_at: string };
export type AdminCompanyDetail = AdminCompany & {
  verification_status: string;
  plan_code: string;
  users: { id: string; email: string; role: string }[];
  counts: { rfqs: number; quotes: number; orders: number };
};
export type AuditEntry = { id: string; company_id: string; actor: string; action: string; target_id: string | null; detail: unknown; created_at: string };

export const listCompanies = (token: string, q = "", status = "") =>
  req<{ companies: AdminCompany[]; count: number }>("GET", `/admin/v1/companies/?${adminQuery({ q, status })}`, { token });
export const getCompany = (token: string, id: string) =>
  req<AdminCompanyDetail>("GET", `/admin/v1/companies/${id}`, { token });
export const suspendCompany = (token: string, id: string, reason: string) =>
  req<{ company_id: string; status: string }>("POST", `/admin/v1/companies/${id}/suspend`, { token, body: { reason } });
export const unsuspendCompany = (token: string, id: string) =>
  req<{ company_id: string; status: string }>("POST", `/admin/v1/companies/${id}/unsuspend`, { token });
export const listAudit = (token: string, filters: { company_id?: string; actor?: string; action?: string } = {}) =>
  req<{ entries: AuditEntry[]; count: number }>("GET", `/admin/v1/audit?${adminQuery(filters)}`, { token });
export const impersonate = (token: string, companyId: string) =>
  req<{ token: string; company_id: string; user_id: string; expires_in: number }>("POST", "/admin/v1/impersonate", { token, body: { company_id: companyId } });

// adminQuery builds a query string from defined, non-empty params only.
export function adminQuery(params: Record<string, string | undefined>): string {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v) p.set(k, v);
  }
  return p.toString();
}
```
(Reuse the existing `listVerifications`/`resolveVerification`/`listReports`/`resolveReport` clients if already present from prior sprints; otherwise add them following the same shape against `/admin/v1/verifications`, `/admin/v1/companies/:id/verification`, `/admin/v1/reports`, `/admin/v1/reports/:id`.)

- [ ] **Step 2: Write the failing vitest** in `web/lib/admin.test.ts` for `adminQuery` (pure, node-env):
```ts
import { describe, it, expect } from "vitest";
import { adminQuery } from "./admin";

describe("adminQuery", () => {
  it("omits empty and undefined params", () => {
    expect(adminQuery({ q: "acme", status: "", actor: undefined })).toBe("q=acme");
  });
  it("encodes multiple params", () => {
    const s = new URLSearchParams(adminQuery({ q: "a b", action: "company.suspend" }));
    expect(s.get("q")).toBe("a b");
    expect(s.get("action")).toBe("company.suspend");
  });
  it("returns empty string when nothing set", () => {
    expect(adminQuery({ q: "", status: undefined })).toBe("");
  });
});
```

- [ ] **Step 3: Run test to verify it fails** — Run (in `web/`): `npx vitest run lib/admin.test.ts`. Expected: FAIL (`adminQuery` not exported) — add to the existing test file if one exists.

- [ ] **Step 4: Confirm the clients compile the test** — after Step 1 the export exists. Run: `npx vitest run lib/admin.test.ts`. Expected: PASS.

- [ ] **Step 5: Extend `AdminShell` nav** in `web/components/AdminShell.tsx` — replace the `NAV` array with:
```tsx
const NAV = [
  { href: "/admin/companies", label: "Companies" },
  { href: "/admin/verifications", label: "Verifications" },
  { href: "/admin/disputes", label: "Disputes" },
  { href: "/admin/reports", label: "Reports" },
  { href: "/admin/audit", label: "Audit" },
  { href: "/admin", label: "Settings" },
];
```
Widen the `<main>` for table-heavy screens: change `maxWidth: 760` to `maxWidth: 1100`.

- [ ] **Step 6: Build the console pages** (all `"use client"`, wrapped in `<AdminShell>`, using `useAdminToken()` + the `lib/admin.ts` clients, styled with the existing CSS utility classes `.card .tbl .btn .btn-sm .badge .field .input .muted .small .row .col`):
  - `web/app/admin/companies/page.tsx` — search box (`q`) + status filter → `listCompanies` → table of results linking to `/admin/companies/[id]`; status shown as a badge (`badge-warning` when `suspended`).
  - `web/app/admin/companies/[id]/page.tsx` — `getCompany` detail: name/type/status/plan/verification, counts row, users table; **Suspend/Unsuspend** button (prompts for reason on suspend) calling `suspendCompany`/`unsuspendCompany` then refetching; **Impersonate** button (Step 7).
  - `web/app/admin/verifications/page.tsx` — `listVerifications` queue with approve/reject calling `resolveVerification`.
  - `web/app/admin/reports/page.tsx` — `listReports` queue with resolve actions calling `resolveReport`.
  - `web/app/admin/audit/page.tsx` — filter inputs (company_id/actor/action) → `listAudit` → table (time, actor, action, target, detail JSON).

- [ ] **Step 7: Impersonation hand-off** — create `web/lib/impersonation.ts`:
```ts
const IMP_TOKEN_KEY = "tb_token";      // the buyer/seller app's auth-token key
const IMP_FLAG_KEY = "tb_impersonating";

// startImpersonation stashes the read-only token where the main app reads its
// auth token, marks the session as impersonating (so the app shows a banner),
// and opens the app. The server blocks all writes regardless.
export function startImpersonation(token: string, companyName: string) {
  localStorage.setItem(IMP_TOKEN_KEY, token);
  localStorage.setItem(IMP_FLAG_KEY, companyName);
  window.open("/dashboard", "_blank");
}
```
NOTE: confirm the buyer/seller app's actual auth-token localStorage key (check `web/lib/auth.ts` `useToken`) and use that exact key for `IMP_TOKEN_KEY`. On the company-detail page, the Impersonate button calls `impersonate(adminToken, id)` then `startImpersonation(res.token, company.name)`.

- [ ] **Step 8: Read-only banner** — in the main app shell (`web/components/AppShell.tsx` or the layout that renders authed pages), read `localStorage.getItem("tb_impersonating")`; when set, render a persistent top banner: "Viewing as {name} — read-only (support session)" with a "Leave" button that clears both keys and returns to `/admin/companies`. Keep it minimal; server-side `EnforceReadOnly` is the real guard.

- [ ] **Step 9: Typecheck + build + vitest** — Run (in `web/`): `npx tsc --noEmit && npx vitest run && npm run build`. Expected: all clean. (Do NOT pipe `npm run build` through `head`/`tail` — SIGPIPE corrupts `.next`.)

- [ ] **Step 10: Commit**
```bash
git add web/lib/admin.ts web/lib/admin.test.ts web/lib/impersonation.ts web/components/AdminShell.tsx web/components/AppShell.tsx web/app/admin
git commit -m "feat(web): unified admin console — companies, queues, audit, impersonation (S8)"
```

---

### Task 6: E2E #8 — admin operations, all audited

**Files:**
- Create: `test/integration/e2e_08_admin_ops_test.go`

**Interfaces:**
- Consumes the full HTTP API through the same harness the other `test/integration/e2e_0N_*.go` files use (real router, `tradebridge_core_test` DB, admin realm enabled with a known `ADMIN_JWT_SECRET`, a seeded platform admin, and a helper to obtain an admin token via `POST /admin/v1/login`).

- [ ] **Step 1: Study the existing E2E harness** — read `test/integration/e2e_07_activation_test.go` and one earlier E2E for the exact bootstrap: how the router/app is built, how the admin realm is enabled and an admin token minted, and the seed helpers (create company, user, verification, order, dispute). Reuse them; do not invent a new harness.

- [ ] **Step 2: Write the E2E** in `test/integration/e2e_08_admin_ops_test.go`. Through the real API:
  1. Seed a company with a **pending** verification and an **open** dispute (reuse the S3/S4 seed helpers). Obtain an admin token.
  2. **Suspend then unsuspend** the company via `POST /admin/v1/companies/:id/suspend` and `/unsuspend`; assert 200 each and that a fresh login for that company returns **403** while suspended and **200** after unsuspend. Assert `company.suspend` + `company.unsuspend` audit rows.
  3. **Approve the verification** via `POST /admin/v1/companies/:id/verification {status:"verified"}`; assert 200 and a `company.verification` audit row.
  4. **Resolve the dispute** via `POST /admin/v1/disputes/:id/resolve`; assert 200 and a `dispute.resolve` audit row.
  5. **Impersonate**: `POST /admin/v1/impersonate {company_id}` → 200 + token. With that token: a mutating call (e.g. `POST /v1/rfqs`) → **403**; a read call (e.g. `GET /v1/me` or `GET /v1/dashboard/buyer`) → **200**. Assert a `support.impersonate` audit row.
  6. `GET /admin/v1/audit?company_id=<id>` → assert it contains all five action names above (`company.suspend`, `company.unsuspend`, `company.verification`, `dispute.resolve`, `support.impersonate`), each `actor` prefixed `admin:`.

- [ ] **Step 3: Run the E2E** — Run: `go test ./test/integration/ -run E2E08 -p 1 -v`. Expected: PASS. (Use the actual test function name; match the file's build tags/`TestMain` conventions of the other E2Es.)

- [ ] **Step 4: Full Go suite** — Run: `go test ./... -p 1`. Expected: green.

- [ ] **Step 5: Commit**
```bash
git add test/integration/e2e_08_admin_ops_test.go
git commit -m "test(core): E2E #8 — admin ops (suspend/verify/dispute/impersonate), all audited (S8)"
```

---

## Self-Review

- **Spec coverage:** users/companies mgmt+suspend (T1, T3) · unified console over existing queues (T5) · audit viewer + admin-action logging (T2) · read-only impersonation (T4) · E2E #8 (T6). Deferred items (analytics, feature flags, helpdesk, refunds, user-level suspend) are intentionally absent. ✓
- **Type consistency:** `RequireActiveCompany(func(ctx,string)(string,error))` matches `q.GetCompanyStatus`; `audit.New(auditWriter, zerolog.Logger)` + `(*Auditor).Admin(ctx, adminEmail, action, targetCompanyID, map[string]any)` used identically in T2/T3/T4; action-name strings match the Global Constraints list and the E2E assertions. ✓
- **Placeholder scan:** the two `entitlementsService`/`IMP_TOKEN_KEY` spots and the query-column/verification-field NOTEs are explicit "confirm against real schema/types" instructions, not gaps — flagged because the exact generated type or localStorage key must be read from the repo, not guessed. ✓

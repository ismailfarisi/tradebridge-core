# Sprint 9 — Scale, Security & Compliance Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship GDPR data-subject rights (export + soft-delete/anonymize), launch legal pages + cookie consent, a bounded Core cache for hot reads, an agent autonomy work-queue with backpressure/retry, and a security review with Critical/High findings fixed.

**Architecture:** New `internal/dsr` (export + delete) and `internal/cache` (fail-open Redis JSON cache) in Core, both mounting on existing rails (the S8 `guard` slice, the S8 `audit.Auditor`). Account deletion sets `companies.deleted_at` (migration 029) and the S8 suspension guard is extended to also block deleted companies. Web adds an Account page + legal pages + a cookie-consent banner. The agent gains a bounded work-queue between its SSE `CoreConnector` and `src/runtime.ts` dispatch.

**Tech Stack:** Go 1.x / Fiber v2 / pgx v5 / sqlc / golang-migrate / go-redis v9; Next.js 16 / React 19 / TS strict / vitest; Bun / TypeORM (SQLite) for the agent.

## Global Constraints

- Export/delete routes are tenant-authed under `/v1/account/*` (compose the S8 `guard` slice). Legal pages are public.
- `companies.deleted_at TIMESTAMPTZ` nullable. Login AND `RequireActiveCompany` reject a company whose `deleted_at` is set (403), reusing the S8 rail.
- Deletion anonymizes IN ONE TX: `companies.name` → `'Deleted company ' || left(id,8)`; `users.email` → `'deleted+' || id || '@deleted.invalid'`, `password_hash` → NULL; `supplier_profiles.description` / `location` → NULL; revoke agent credentials. Transactional rows (orders/invoices/quotes/disputes/reviews/rfqs) are RETAINED. Idempotent (no-op if already deleted).
- Deletion requires the acting user to be role `owner` AND to re-confirm their password (`identity.CheckPassword`); else 403. Audited via S8 `audit.Auditor` → action `account.delete`, actor = the acting user id, target company = the company.
- `internal/cache` is FAIL-OPEN: nil or unreachable Redis → cache miss / no-op, never a request-breaking error.
- Agent queue: bounded max-concurrency, retry-with-backoff on transient failure, idempotency via the existing `ProcessedEvent` ledger; a burst or crash never double-quotes or drops an event.
- Legal pages show a visible "DRAFT — not legal advice, pending review" banner.
- Tests: Core → `tradebridge_core_test` (NEVER live `tradebridge_core`); full Go suite `-p 1`. Agent → `bun test`. Web → vitest node-env, `lib/**/*.test.ts` only. Regenerate sqlc with `sqlc generate` after any `pkg/db/query/*.sql` change (no `make` binary here); never hand-edit `pkg/db/sqlc/*`. NEVER pipe `npm run build` through head/tail.
- Both repos merge to `master` **locally, never pushed**. Commit trailers on every commit:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01JrKi3VYFWQJPRvZGwScMqh`.

## File Structure

- `pkg/db/migrations/029_company_deleted_at.{up,down}.sql`
- `pkg/db/query/companies.sql` — add `GetCompanyGate`, `SoftDeleteCompany`, `AnonymizeCompany`, `AnonymizeCompanyUsers`, `ScrubSupplierProfile`.
- `pkg/db/query/dsr.sql` — export `:many` queries not already present.
- `pkg/middleware/tenant.go` — extend `RequireActiveCompany` to also block deleted.
- `internal/identity/handler.go` — login blocks deleted.
- `internal/dsr/{service.go,handler.go,service_test.go,handler_test.go}`
- `internal/cache/{cache.go,cache_test.go}` + apply in `internal/discovery/`, `internal/suppliers/` (public storefront), `internal/dashboard/`.
- `internal/httpapi/router.go` — wire DSR handler + cache.
- `web/lib/{account.ts,account.test.ts,consent.ts,consent.test.ts}`; `web/components/CookieConsent.tsx`; `web/app/(app)/account/page.tsx`; `web/app/(marketing)/legal/{terms,privacy,cookies}/page.tsx`; `web/app/layout.tsx` (mount consent + footer links).
- `tradebridge-agent/src/queue/{workqueue.ts,workqueue.test.ts}` + wiring in `src/runtime.ts`.
- `docs/superpowers/specs/2026-07-18-sprint-09-security-findings.md` (Task 7 output).
- `test/integration/e2e_09_dsr_test.go`

---

### Task 1: Migration 029 — company deleted_at + delete enforcement

**Files:**
- Create: `pkg/db/migrations/029_company_deleted_at.{up,down}.sql`
- Modify: `pkg/db/query/companies.sql` (add `GetCompanyGate`)
- Modify: `pkg/middleware/tenant.go` + `pkg/middleware/tenant_test.go`
- Modify: `internal/identity/handler.go` (login blocks deleted)
- Modify: `internal/httpapi/router.go` (guard wiring for the new gate func)

**Interfaces:**
- Produces: `q.GetCompanyGate(ctx, id) (GetCompanyGateRow{Status string, DeletedAt *time.Time}, error)`; `middleware.RequireActiveCompany(gate func(ctx, companyID string) (status string, deleted bool, err error)) fiber.Handler` (SIGNATURE CHANGE from S8 — now returns deleted too).

- [ ] **Step 1: Migration** `029_company_deleted_at.up.sql`:
```sql
ALTER TABLE companies ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_companies_deleted ON companies (deleted_at) WHERE deleted_at IS NOT NULL;
```
`029_company_deleted_at.down.sql`:
```sql
DROP INDEX IF EXISTS idx_companies_deleted;
ALTER TABLE companies DROP COLUMN IF EXISTS deleted_at;
```

- [ ] **Step 2: Query** — add to `pkg/db/query/companies.sql`:
```sql
-- name: GetCompanyGate :one
SELECT status, deleted_at FROM companies WHERE id = $1;
```

- [ ] **Step 3: Regenerate sqlc** — `sqlc generate`. Expect `GetCompanyGateRow{Status string, DeletedAt *time.Time}` (pointer because nullable). Confirm the field name/type; adjust later code if sqlc emits `pgtype.Timestamptz` instead of `*time.Time` (the repo overrides timestamptz→time.Time, so nullable → `*time.Time`).

- [ ] **Step 4: Failing middleware test** — update `TestRequireActiveCompany` in `pkg/middleware/tenant_test.go` to the new signature and add a deleted case:
```go
func TestRequireActiveCompany(t *testing.T) {
	newApp := func(status string, deleted bool) *fiber.App {
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error { c.Locals(LocalCompanyID, "co_1"); return c.Next() })
		app.Use(RequireActiveCompany(func(context.Context, string) (string, bool, error) {
			return status, deleted, nil
		}))
		app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })
		return app
	}
	cases := []struct{ name, status string; deleted bool; want int }{
		{"active", "active", false, 200},
		{"suspended", "suspended", false, 403},
		{"deleted", "active", true, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := newApp(tc.status, tc.deleted).Test(httptest.NewRequest("GET", "/x", nil))
			if res.StatusCode != tc.want {
				t.Fatalf("%s: want %d got %d", tc.name, tc.want, res.StatusCode)
			}
		})
	}
}
```

- [ ] **Step 5: Run test — fails** (signature mismatch). `go test ./pkg/middleware/ -run TestRequireActiveCompany`.

- [ ] **Step 6: Implement** — replace `RequireActiveCompany` in `pkg/middleware/tenant.go`:
```go
// RequireActiveCompany must run after TenantGuard. It rejects a suspended OR
// deleted company so admin suspension and account deletion take effect
// immediately. `gate` is injected (a closure over q.GetCompanyGate) so this
// package stays free of a sqlc dependency.
func RequireActiveCompany(gate func(ctx context.Context, companyID string) (status string, deleted bool, err error)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		status, deleted, err := gate(c.Context(), CompanyID(c))
		if err != nil {
			return err
		}
		if deleted {
			return fiber.NewError(fiber.StatusForbidden, "company deleted")
		}
		if status == "suspended" {
			return fiber.NewError(fiber.StatusForbidden, "company suspended")
		}
		return c.Next()
	}
}
```

- [ ] **Step 7: Run test — passes.**

- [ ] **Step 8: Router wiring** — in `internal/httpapi/router.go`, replace `middleware.RequireActiveCompany(q.GetCompanyStatus)` (3rd guard element) with a closure over `GetCompanyGate`:
```go
		middleware.RequireActiveCompany(func(ctx context.Context, cid string) (string, bool, error) {
			g, err := q.GetCompanyGate(ctx, cid)
			if err != nil {
				return "", false, err
			}
			return g.Status, g.DeletedAt != nil, nil
		}),
```
Ensure `"context"` is imported in router.go.

- [ ] **Step 9: Login blocks deleted** — in `internal/identity/handler.go` `login`, the S8 suspend check currently reads `GetCompanyStatus`. Change it to `GetCompanyGate` and block both:
```go
	if g, err := h.q.GetCompanyGate(c.Context(), user.CompanyID); err == nil {
		if g.DeletedAt != nil {
			return fiber.NewError(fiber.StatusForbidden, "account deleted")
		}
		if g.Status == "suspended" {
			return fiber.NewError(fiber.StatusForbidden, "company suspended")
		}
	}
```

- [ ] **Step 10: Build + tests** — `go build ./... && go test ./pkg/middleware/ ./internal/identity/`. (End-to-end delete→login-403 is in Task 8.)

- [ ] **Step 11: Commit**
```bash
git add pkg/db/migrations/029_company_deleted_at.up.sql pkg/db/migrations/029_company_deleted_at.down.sql pkg/db/query/companies.sql pkg/db/sqlc pkg/middleware/tenant.go pkg/middleware/tenant_test.go internal/identity/handler.go internal/httpapi/router.go
git commit -m "feat(core): company deleted_at + delete/suspend gate (S9)"
```

---

### Task 2: DSR data export (`internal/dsr` — export half)

**Files:**
- Create: `pkg/db/query/dsr.sql` (export queries not already present)
- Create: `internal/dsr/service.go` (Export), `internal/dsr/handler.go` (`GET /v1/account/export`), `internal/dsr/service_test.go`
- Modify: `internal/httpapi/router.go` (register)

**Interfaces:**
- Consumes: existing `...ByCompany` queries (`ListUsersByCompany`, `ListCatalogByCompany`, `ListOrdersByCompany`, `GetSupplierProfileByCompany`, `GetSubscriptionByCompany`, `GetCompanyByID`, etc.); `middleware.CompanyID`/`Role`.
- Produces: `dsr.NewService(q *sqlc.Queries) *Service`; `(*Service).Export(ctx, companyID string) (Bundle, error)` where `Bundle = map[string]any`; `dsr.NewHandler(svc *Service) *Handler` with `.Register(app, guard...)` mounting `GET /v1/account/export`.

- [ ] **Step 1: Inventory existing per-company queries** — grep `pkg/db/query/*.sql` for `ByCompany`/`ByBuyer`/`BySeller` `:many` queries covering: users, supplier profile, catalog, rfqs, quotes, orders, invoices, reviews, disputes, subscriptions, activation events. For each section with NO existing query, add one to `pkg/db/query/dsr.sql`, e.g.:
```sql
-- name: ExportRFQsByCompany :many
SELECT * FROM rfqs WHERE buyer_company_id = $1 ORDER BY created_at;

-- name: ExportQuotesByCompany :many
SELECT * FROM quotes WHERE seller_company_id = $1 ORDER BY created_at;

-- name: ExportOrdersByCompany :many
SELECT * FROM orders WHERE buyer_company_id = $1 OR seller_company_id = $1 ORDER BY created_at;

-- name: ExportDisputesByCompany :many
SELECT * FROM disputes WHERE raised_by_company_id = $1 ORDER BY created_at;

-- name: ExportReviewsByCompany :many
SELECT * FROM reviews WHERE reviewer_company_id = $1 OR reviewee_company_id = $1 ORDER BY created_at;
```
Confirm exact column names against the migrations before adding (do NOT invent columns; reuse an existing `...ByCompany` query instead of adding a duplicate — sqlc errors on duplicate names). Then `sqlc generate`.

- [ ] **Step 2: Failing service test** `internal/dsr/service_test.go` — using the package's DB test harness (see `internal/adminconsole/handler_test.go` / `internal/dashboard/*_test.go` for connecting a test `*sqlc.Queries` to `tradebridge_core_test`): seed a company with a user + one RFQ; call `Export`; assert the bundle has non-empty `company`, `users`, and `rfqs` sections and that the RFQ id appears. (If the package has no harness yet, mirror the one in `internal/adminconsole/handler_test.go`.)

- [ ] **Step 3: Run — fails** (`Export` undefined). `go test ./internal/dsr/`.

- [ ] **Step 4: Implement `Export`** `internal/dsr/service.go`:
```go
// Package dsr implements GDPR data-subject rights: full data export and
// soft-delete/anonymize account deletion.
package dsr

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service {
	return &Service{q: q}
}

// Bundle is the exported data, one entry per data section.
type Bundle map[string]any

// Export assembles the company's full data footprint. Each section is
// best-effort: a section that errors is recorded as an empty slice rather than
// failing the whole export (export must never leave the subject unable to get
// their data). The company row itself is required.
func (s *Service) Export(ctx context.Context, companyID string) (Bundle, error) {
	b := Bundle{}
	company, err := s.q.GetCompanyByID(ctx, companyID)
	if err != nil {
		return nil, err
	}
	b["company"] = company
	if v, err := s.q.ListUsersByCompany(ctx, companyID); err == nil {
		b["users"] = v
	}
	if v, err := s.q.ExportRFQsByCompany(ctx, companyID); err == nil {
		b["rfqs"] = v
	}
	// ...one block per section, following the same (err == nil) pattern...
	return b, nil
}
```
Fill in a block for every section from the Step-1 inventory (users, supplier_profile, catalog, rfqs, quotes, orders, invoices, reviews, disputes, subscriptions, activation_events). Password hashes must NOT be in the export: `ListUsersByCompany` returns `sqlc.User` which includes `PasswordHash` — map users to a `[]map[string]any` or a small DTO dropping `PasswordHash` before adding to the bundle (same lesson as S8 adminconsole). Add a comment marking why.

- [ ] **Step 5: Run — passes.**

- [ ] **Step 6: Handler** `internal/dsr/handler.go`:
```go
package dsr

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Get("/v1/account/export", append(append([]fiber.Handler{}, guard...), h.export)...)
}

func (h *Handler) export(c *fiber.Ctx) error {
	role := middleware.Role(c)
	if role != "owner" && role != "admin" {
		return fiber.NewError(fiber.StatusForbidden, "owner or admin required")
	}
	bundle, err := h.svc.Export(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	c.Set("Content-Disposition", `attachment; filename="tradebridge-export.json"`)
	return c.JSON(fiber.Map{"bundle": bundle})
}
```

- [ ] **Step 7: Register** in `internal/httpapi/router.go` (a DSR handler value reused by Task 3): `dsr.NewHandler(dsrSvc).Register(f, guard...)` where `dsrSvc := dsr.NewService(q)`.

- [ ] **Step 8: Build + test** — `go build ./... && go test ./internal/dsr/`.

- [ ] **Step 9: Commit** — `feat(core): GDPR data export /v1/account/export (S9)` + trailers.

---

### Task 3: Account deletion (anonymize) + web Account page

**Files:**
- Modify: `pkg/db/query/companies.sql` (add delete/anonymize queries)
- Modify: `internal/dsr/service.go` (`Delete`), `internal/dsr/handler.go` (`POST /v1/account/delete`), tests
- Modify: `internal/httpapi/router.go` (Delete needs `identity.CheckPassword` access + the `audit.Auditor`)
- Create: `web/lib/account.ts` + `web/lib/account.test.ts`; `web/app/(app)/account/page.tsx`

**Interfaces:**
- Consumes: `q.GetUserByID` (to fetch the acting user's password hash + role), `identity.CheckPassword(hash, plain) bool`, `audit.Auditor`.
- Produces: `(*dsr.Service).Delete(ctx, companyID, actingUserID string) error`; `POST /v1/account/delete`; web account page + `canDelete(role string) bool`.

- [ ] **Step 1: Queries** — add to `pkg/db/query/companies.sql`:
```sql
-- name: SoftDeleteAndAnonymizeCompany :exec
UPDATE companies
SET deleted_at = NOW(),
    name = 'Deleted company ' || left(id, 8)
WHERE id = $1 AND deleted_at IS NULL;

-- name: AnonymizeCompanyUsers :exec
UPDATE users
SET email = 'deleted+' || id || '@deleted.invalid',
    password_hash = NULL
WHERE company_id = $1;

-- name: ScrubSupplierProfile :exec
UPDATE supplier_profiles
SET description = NULL, location = NULL
WHERE company_id = $1;

-- name: RevokeAgentCredentialsByCompany :exec
UPDATE agent_credentials SET status = 'revoked' WHERE company_id = $1;
```
Confirm `agent_credentials` has a `status` column with a `'revoked'` value (migration 004 — `status IN ('active','revoked')`). `sqlc generate`.

- [ ] **Step 2: Failing test** — in `internal/dsr/service_test.go`, seed a company + owner user (with a real bcrypt hash) + a supplier profile; call `Delete`; assert `GetCompanyGate` shows `deleted_at` set, the company name starts with `Deleted company `, `GetUserByID` shows the email anonymized + password_hash nil. Also assert a second `Delete` is a no-op (idempotent) and does not error.

- [ ] **Step 3: Run — fails.**

- [ ] **Step 4: Implement `Delete`** in `internal/dsr/service.go` — one transaction via the pool. The `Service` needs a `*pgxpool.Pool` to open a tx; add it:
```go
func NewService(q *sqlc.Queries, pool *pgxpool.Pool) *Service {
	return &Service{q: q, pool: pool}
}

// Delete performs a GDPR soft-delete: it sets deleted_at, anonymizes PII, and
// revokes agent credentials in one transaction. Transactional records
// (orders/invoices/quotes/disputes) are retained for the counterparty. Idempotent.
func (s *Service) Delete(ctx context.Context, companyID, actingUserID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if err := qtx.SoftDeleteAndAnonymizeCompany(ctx, companyID); err != nil {
		return err
	}
	if err := qtx.AnonymizeCompanyUsers(ctx, companyID); err != nil {
		return err
	}
	if err := qtx.ScrubSupplierProfile(ctx, companyID); err != nil {
		return err
	}
	if err := qtx.RevokeAgentCredentialsByCompany(ctx, companyID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
```
(Confirm the sqlc `*Queries` exposes `WithTx(pgx.Tx) *Queries` — it does in this repo; see how other services open txns, e.g. `internal/disputes/service.go` `Resolve`.) Update `NewService` call sites (Task 2's router registration + tests) to pass the pool. The pool is available in the router as the value `q` was built from — find how `q` is constructed (a `*pgxpool.Pool`, likely named `pool` or `db`) and thread it in.

- [ ] **Step 5: Run — passes.**

- [ ] **Step 6: Delete handler** — add to `internal/dsr/handler.go`. The handler needs the acting user's hash/role and the auditor; give `Handler` those deps:
```go
func (h *Handler) delete(c *fiber.Ctx) error {
	if middleware.Role(c) != "owner" {
		return fiber.NewError(fiber.StatusForbidden, "owner required")
	}
	var body struct{ Password string `json:"password"` }
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	userID := middleware.UserID(c)
	u, err := h.q.GetUserByID(c.Context(), userID)
	if err != nil {
		return err
	}
	if u.PasswordHash == nil || !identity.CheckPassword(*u.PasswordHash, body.Password) {
		return fiber.NewError(fiber.StatusForbidden, "password incorrect")
	}
	companyID := middleware.CompanyID(c)
	if err := h.svc.Delete(c.Context(), companyID, userID); err != nil {
		return err
	}
	h.auditor.Admin(c.Context(), "user:"+userID, "account.delete", companyID, map[string]any{"by": userID})
	return c.JSON(fiber.Map{"deleted": true})
}
```
Add `h.q` (`*sqlc.Queries`) and `h.auditor` (`*audit.Auditor`) to `Handler` + `NewHandler`; register `app.Post("/v1/account/delete", append(guard, h.delete)...)`. NOTE: `audit.Auditor.Admin`'s first "adminEmail" arg is prefixed `admin:` internally — for a self-service deletion the actor should read as the user, so pass `"user:"+userID`? Actually `Admin` hardcodes the `admin:` prefix. To record actor `user:<id>` cleanly, either (a) accept the `admin:user:<id>` artifact, or (b) call the underlying `q.CreateAuditLog` directly here with `Actor: "user:"+userID`. Prefer (b) for a correct actor string — write the audit row directly (best-effort, log on error) rather than via `Auditor.Admin`. Implementer: use `q.CreateAuditLog` directly with `Actor: "user:"+userID, Action: "account.delete", CompanyID: companyID, TargetID: &companyID, Detail: {}`.

- [ ] **Step 7: Run handler test** — add a handler test: owner + correct password → 200 + `account.delete` audit row; wrong password → 403; non-owner role → 403.

- [ ] **Step 8: Web account page + lib** — `web/lib/account.ts`:
```ts
"use client";
import { CORE_URL } from "./api";

export async function exportData(token: string): Promise<unknown> {
  const res = await fetch(`${CORE_URL}/v1/account/export`, { headers: { authorization: `Bearer ${token}` }, cache: "no-store" });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}
export async function deleteAccount(token: string, password: string): Promise<void> {
  const res = await fetch(`${CORE_URL}/v1/account/delete`, {
    method: "POST",
    headers: { "content-type": "application/json", authorization: `Bearer ${token}` },
    body: JSON.stringify({ password }),
  });
  if (!res.ok) throw new Error(await res.text());
}
// canDelete gates the delete UI to owners.
export function canDelete(role: string): boolean {
  return role === "owner";
}
```
`web/lib/account.test.ts` — vitest for `canDelete` (owner true; admin/member/"" false).
`web/app/(app)/account/page.tsx` (`"use client"`, in AppShell): "Export my data" button → `exportData` → trigger a JSON blob download; a "Danger zone" card visible only when `canDelete(me.role)` → typed "DELETE" confirmation input + password field → `deleteAccount` → on success `clearToken()` (from `@/lib/api`) and redirect to `/`. Use `useMe()` for the role. Follow existing page patterns (CSS utility classes `.card .field .input .btn .btn-primary .err`).

- [ ] **Step 9: Verify web** — `cd web && npx tsc --noEmit && npx vitest run && npm run build`.

- [ ] **Step 10: Commit** — `feat(core+web): GDPR account deletion (soft-delete + anonymize) + account page (S9)` + trailers.

---

### Task 4: Legal pages + cookie consent (web)

**Files:**
- Create: `web/lib/consent.ts` + `web/lib/consent.test.ts`
- Create: `web/components/CookieConsent.tsx`
- Create: `web/app/(marketing)/legal/{terms,privacy,cookies}/page.tsx` (+ a shared `web/app/(marketing)/legal/LegalShell.tsx` or inline banner)
- Modify: `web/app/layout.tsx` (mount `<CookieConsent/>` + footer legal links)

**Interfaces:**
- Produces: pure `consent.ts` (`readConsent(): "accepted"|"rejected"|null`, `writeConsent(v)`, `needsConsent(): boolean`); `<CookieConsent/>`.

- [ ] **Step 1: Pure consent lib** `web/lib/consent.ts`:
```ts
"use client";
const KEY = "tb_cookie_consent";
export type Consent = "accepted" | "rejected";

export function readConsent(): Consent | null {
  if (typeof window === "undefined") return null;
  const v = localStorage.getItem(KEY);
  return v === "accepted" || v === "rejected" ? v : null;
}
export function writeConsent(v: Consent): void {
  if (typeof window !== "undefined") localStorage.setItem(KEY, v);
}
// needsConsent is true until the user has made a choice.
export function needsConsent(): boolean {
  return readConsent() === null;
}
```

- [ ] **Step 2: Failing vitest** `web/lib/consent.test.ts` — with a jsdom-free node env, `localStorage` is undefined, so test the pure branches by injecting a fake: refactor `readConsent`/`writeConsent` to accept an optional `store: Pick<Storage,"getItem"|"setItem"> = ...`. Test: unknown value → null; "accepted" → "accepted"; `needsConsent` true when null, false after write. (Keep the default `store` guarded by `typeof window`.)

- [ ] **Step 3: Run — fails; implement the injectable store; run — passes.** `cd web && npx vitest run lib/consent.test.ts`.

- [ ] **Step 4: `<CookieConsent/>`** `web/components/CookieConsent.tsx` (`"use client"`): on mount (hydration-safe), if `needsConsent()` render a fixed bottom banner — short copy + link to `/legal/cookies` + Accept / Reject buttons calling `writeConsent(...)` then hiding. No banner once a choice exists.

- [ ] **Step 5: Legal pages** — three server components under `web/app/(marketing)/legal/`. Each renders a persistent top banner: **"DRAFT — not legal advice, pending legal review before launch."** Draft boilerplate with `[Entity Name]`, `[Jurisdiction]`, `[contact@…]` placeholders:
  - `terms/page.tsx` — acceptance, accounts, acceptable use, subscriptions/billing, disclaimers, liability, termination, governing law.
  - `privacy/page.tsx` — data collected, purposes, legal bases, retention, data-subject rights (link to `/account`), a **subprocessor list** (Stripe, Resend, Anthropic, hosting), contact.
  - `cookies/page.tsx` — essential vs non-essential, the consent choice, how to change it.

- [ ] **Step 6: Mount in root layout** — `web/app/layout.tsx`: render `<CookieConsent/>` once, and add a footer with links to `/legal/terms`, `/legal/privacy`, `/legal/cookies`. Keep it minimal and theme-consistent.

- [ ] **Step 7: Verify** — `cd web && npx tsc --noEmit && npx vitest run && npm run build`.

- [ ] **Step 8: Commit** — `feat(web): legal pages (ToS/privacy/cookies, DRAFT) + cookie consent (S9)` + trailers.

---

### Task 5: Core cache layer + apply to hot reads

**Files:**
- Create: `internal/cache/cache.go` + `internal/cache/cache_test.go`
- Modify: `internal/discovery/service.go` (cache Search), the public storefront read in `internal/suppliers/`, `internal/dashboard/service.go`; wire invalidation on the corresponding writes (`internal/suppliers` profile update, `internal/catalog` writes).
- Modify: `internal/httpapi/router.go` (construct one `*cache.Cache`, inject into the services)

**Interfaces:**
- Produces: `cache.New(rdb *redis.Client, logger zerolog.Logger) *Cache`; `(*Cache).GetJSON(ctx, key string, dest any) (hit bool, err error)`; `(*Cache).SetJSON(ctx, key string, v any, ttl time.Duration)`; `(*Cache).Delete(ctx, keys ...string)`. All fail-open.

- [ ] **Step 1: Failing test** `internal/cache/cache_test.go`:
```go
package cache

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNilClientFailsOpen(t *testing.T) {
	c := New(nil, zerolog.Nop())
	var dst map[string]int
	hit, err := c.GetJSON(context.Background(), "k", &dst)
	if err != nil || hit {
		t.Fatalf("nil client: want miss+no-err, got hit=%v err=%v", hit, err)
	}
	// Set + Delete must not panic with a nil client.
	c.SetJSON(context.Background(), "k", map[string]int{"a": 1}, time.Minute)
	c.Delete(context.Background(), "k")
}
```

- [ ] **Step 2: Run — fails.** `go test ./internal/cache/`.

- [ ] **Step 3: Implement** `internal/cache/cache.go`:
```go
// Package cache is a fail-open Redis JSON cache for hot read paths. A nil or
// unreachable Redis makes every operation a miss / no-op — correctness never
// depends on the cache.
package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Cache struct {
	rdb    *redis.Client
	logger zerolog.Logger
}

func New(rdb *redis.Client, logger zerolog.Logger) *Cache {
	return &Cache{rdb: rdb, logger: logger}
}

// GetJSON reports whether key was a hit and, on hit, unmarshals into dest.
func (c *Cache) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, nil
	}
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		c.logger.Debug().Err(err).Str("key", key).Msg("cache get failed (fail-open)")
		return false, nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, nil
	}
	return true, nil
}

func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) {
	if c == nil || c.rdb == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	if err := c.rdb.Set(ctx, key, raw, ttl).Err(); err != nil {
		c.logger.Debug().Err(err).Str("key", key).Msg("cache set failed (fail-open)")
	}
}

func (c *Cache) Delete(ctx context.Context, keys ...string) {
	if c == nil || c.rdb == nil || len(keys) == 0 {
		return
	}
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		c.logger.Debug().Err(err).Msg("cache delete failed (fail-open)")
	}
}
```

- [ ] **Step 4: Run — passes.**

- [ ] **Step 5: Apply to public discovery** — inject `*cache.Cache` into `discovery.Service`. In `Search`, build a key from the normalized query+filters (e.g. `"disc:" + fmt.Sprintf("%q", normalizedQuery)`), `GetJSON` first (return on hit), else compute + `SetJSON` with a 45s TTL. Only cache the PUBLIC path (no per-company/personalized data). Add a test asserting a nil-cache Service still returns correct results (fail-open) — do NOT assert cache hits in a unit test (that needs Redis); the fail-open path is the correctness guarantee.

- [ ] **Step 6: Apply to public storefront profile** — cache the public supplier profile read (`internal/suppliers` public handler) with a longer TTL (e.g. 5m), key `"store:" + companyID`. In the profile-update and catalog-write paths (`internal/suppliers` upsert, `internal/catalog` create/update/delete), call `cache.Delete(ctx, "store:"+companyID)` after the write commits so stale profiles don't linger.

- [ ] **Step 7: Apply to dashboards (optional, short TTL)** — buyer/seller dashboard reads keyed `"dash:buyer:"+companyID` / `"dash:seller:"+companyID`, 20s TTL. Skip if it complicates invalidation; discovery + storefront are the priority.

- [ ] **Step 8: Wire router** — construct `appCache := cache.New(cfg.Redis, cfg.Logger)` once and inject into the discovery/suppliers/dashboard/catalog constructors that need it. Update those constructor signatures + call sites + any tests.

- [ ] **Step 9: Build + tests** — `go build ./... && go test ./internal/cache/ ./internal/discovery/ ./internal/suppliers/ ./internal/dashboard/`.

- [ ] **Step 10: Commit** — `feat(core): fail-open Redis cache for discovery/storefront/dashboards (S9)` + trailers.

---

### Task 6: Agent autonomy work-queue (backpressure + retry)

**Files:**
- Create: `tradebridge-agent/src/queue/workqueue.ts` + `tradebridge-agent/src/queue/workqueue.test.ts`
- Modify: `tradebridge-agent/src/runtime.ts` (dispatch through the queue)

**Interfaces:**
- Produces: a `WorkQueue` with bounded concurrency, retry-with-backoff, and a bounded buffer (backpressure). Idempotency stays in `runtime.ts` via the existing `ProcessedEvent` ledger.

- [ ] **Step 1: Study the current dispatch** — read `tradebridge-agent/src/runtime.ts` (the `onEvent` path that checks `ProcessedEvent` then `await`s `handleRfqMatched`/`handleQuoteMessage`/`handleCounter`). The queue sits between the connector's `onEvent` and these handlers: `onEvent` enqueues, workers drain.

- [ ] **Step 2: Failing test** `tradebridge-agent/src/queue/workqueue.test.ts` (`bun test`):
```ts
import { test, expect } from "bun:test";
import { WorkQueue } from "./workqueue.ts";

test("respects max concurrency", async () => {
  let active = 0, peak = 0;
  const q = new WorkQueue({ concurrency: 2, maxRetries: 0 });
  const job = async () => {
    active++; peak = Math.max(peak, active);
    await new Promise((r) => setTimeout(r, 20));
    active--;
  };
  await Promise.all(Array.from({ length: 6 }, () => q.push(job)));
  expect(peak).toBeLessThanOrEqual(2);
});

test("retries a transient failure then succeeds", async () => {
  const q = new WorkQueue({ concurrency: 1, maxRetries: 3, baseDelayMs: 1 });
  let attempts = 0;
  await q.push(async () => {
    attempts++;
    if (attempts < 3) throw new Error("transient");
  });
  expect(attempts).toBe(3);
});

test("gives up after maxRetries and rejects", async () => {
  const q = new WorkQueue({ concurrency: 1, maxRetries: 2, baseDelayMs: 1 });
  let attempts = 0;
  await expect(q.push(async () => { attempts++; throw new Error("always"); })).rejects.toThrow();
  expect(attempts).toBe(3); // initial + 2 retries
});
```

- [ ] **Step 3: Run — fails.** `cd tradebridge-agent && bun test src/queue/workqueue.test.ts`.

- [ ] **Step 4: Implement `WorkQueue`** `tradebridge-agent/src/queue/workqueue.ts`:
```ts
export interface WorkQueueOptions {
  concurrency: number;      // max jobs in flight
  maxRetries: number;       // retries after the first attempt
  baseDelayMs?: number;     // backoff base (default 200)
  maxPending?: number;      // buffer cap for backpressure (default 1000)
}

type Job<T> = () => Promise<T>;

// WorkQueue drains jobs at a bounded concurrency, retrying transient failures
// with exponential backoff. push() resolves/rejects with the job's outcome and
// applies backpressure by rejecting when the pending buffer is full.
export class WorkQueue {
  private active = 0;
  private pending: Array<() => void> = [];
  constructor(private opts: WorkQueueOptions) {}

  private async slot(): Promise<void> {
    if (this.active < this.opts.concurrency) { this.active++; return; }
    if (this.pending.length >= (this.opts.maxPending ?? 1000)) {
      throw new Error("work queue full (backpressure)");
    }
    await new Promise<void>((resolve) => this.pending.push(resolve));
    this.active++;
  }

  private release(): void {
    this.active--;
    const next = this.pending.shift();
    if (next) next();
  }

  async push<T>(job: Job<T>): Promise<T> {
    await this.slot();
    try {
      return await this.runWithRetry(job);
    } finally {
      this.release();
    }
  }

  private async runWithRetry<T>(job: Job<T>): Promise<T> {
    const base = this.opts.baseDelayMs ?? 200;
    let attempt = 0;
    for (;;) {
      try {
        return await job();
      } catch (err) {
        if (attempt >= this.opts.maxRetries) throw err;
        const delay = base * 2 ** attempt;
        attempt++;
        await new Promise((r) => setTimeout(r, delay));
      }
    }
  }
}
```

- [ ] **Step 5: Run — passes.**

- [ ] **Step 6: Wire into `runtime.ts`** — construct one `WorkQueue` (concurrency from env `AGENT_QUEUE_CONCURRENCY`, default 3; `maxRetries` default 3). Change `onEvent` so the ProcessedEvent idempotency check + handler dispatch run INSIDE `queue.push(async () => { ... existing dispatch ... })`. Keep the `ProcessedEvent` mark-processed exactly where it is (after a successful handler) so a retried, eventually-successful event is marked once and a permanently-failing one is NOT marked (so a later replay can retry). Do not `await` the push in a way that re-serializes intake — `onEvent` should enqueue and return promptly so the connector keeps reading (backpressure kicks in only when the buffer fills). Log a warning on a job that exhausts retries (dead-letter) with the event id.

- [ ] **Step 7: Full agent test run** — `cd tradebridge-agent && bun test` (whole suite green).

- [ ] **Step 8: Commit** (in the AGENT repo, master local) — `feat(agent): bounded autonomy work-queue with backpressure + retry (S9)` + trailers.

---

### Task 7: Security review + fixes (both repos)

**Files:**
- Create: `docs/superpowers/specs/2026-07-18-sprint-09-security-findings.md`
- Modify: whatever the Critical/High findings require (with tests)

This task is a review-then-fix cycle, run by the controller dispatching security-review subagents; the plan defines the scope and the fix discipline.

- [ ] **Step 1: Dependency audit** — Core: `go version`, then `govulncheck ./...` if installed (else `go list -m all` + note). Agent: `bun pm ls` / npm advisories on `package.json` deps. Record versions with known CVEs.

- [ ] **Step 2: Adversarial code review** — dispatch security reviewers over: (a) Core auth/session (JWT, admin realm, the S8 impersonation + S9 DSR/delete surfaces), tenant isolation, SQL/injection (sqlc params), SSRF (any outbound fetch), secret handling; (b) agent auth (dashboard token), outbound provider calls, the new queue. Focus especially on the NEW S9 surfaces: does `/v1/account/export` leak another tenant's data or password hashes? Does `/v1/account/delete` properly gate on owner + password? Can a deleted company still act?

- [ ] **Step 3: Triage** — write `docs/superpowers/specs/2026-07-18-sprint-09-security-findings.md` listing each finding with severity (Critical/High/Medium/Low), location, and disposition.

- [ ] **Step 4: Fix Critical/High** — for each, write a failing test that demonstrates the issue, fix it, confirm the test passes. Document Medium/Low with a rationale for deferral. Commit fixes with descriptive messages + trailers.

- [ ] **Step 5: Re-run suites** — `go test ./... -p 1` (Core), `bun test` (agent), `npx tsc --noEmit && npx vitest run` (web) all green.

- [ ] **Step 6: Commit** the findings doc + any fixes.

---

### Task 8: E2E #9 — GDPR export + deletion

**Files:**
- Create: `test/integration/e2e_09_dsr_test.go`

- [ ] **Step 1: Study the harness** — read `test/integration/e2e_08_admin_ops_test.go` for the router build + auth-token + seed helpers (register company, create RFQ, create order). Ensure the test DB is at v29 (`migrate ... up`).

- [ ] **Step 2: Write the E2E** — through the real API:
  1. Register a company (+ owner) and log in; seed one RFQ and one order (reuse seed helpers).
  2. `GET /v1/account/export` (owner token) → 200; assert the bundle contains `company`, and the seeded RFQ id and order id appear; assert NO `password_hash` string appears anywhere in the response body.
  3. `POST /v1/account/delete {password:<owner password>}` (owner) → 200; assert an `account.delete` audit row (`GET /admin/v1/audit?company_id=` via an admin token, or query the DB directly).
  4. Assert (DB query via the test `*sqlc.Queries`): `deleted_at` set, `companies.name` starts with `Deleted company `, the owner's `users.email` anonymized + `password_hash` NULL.
  5. A fresh login for that company's original email → **403** (deleted) — note the email is now anonymized, so log in BEFORE deletion to capture the token, and assert the post-delete login attempt with the original creds fails 403 (the email no longer resolves / account deleted).
  6. The seeded order + RFQ rows STILL EXIST (retained).
  7. `POST /v1/account/delete` with a wrong password (fresh company) → **403**; with a non-owner member → **403**.

- [ ] **Step 3: Run** — `go test ./test/integration/ -run E2E09 -p 1 -v` (match the file's tag/name conventions), then `go test ./... -p 1` green.

- [ ] **Step 4: Commit** — `test(core): E2E #9 — GDPR export + account deletion (S9)` + trailers.

---

## Self-Review

- **Spec coverage:** DSR export (T2) + delete/anonymize (T3) + migration/gate (T1) + web account page (T3); legal + cookie consent (T4); cache (T5); agent queue (T6); security review+fixes (T7); E2E #9 (T8). Deferred infra items intentionally absent. ✓
- **Type consistency:** `RequireActiveCompany` new 3-return gate func used identically in T1 middleware, router, and login; `dsr.NewService(q, pool)` (T3 adds the pool) — Task 2's registration must pass the pool from the start to avoid a mid-plan signature change (fold the pool into T2's `NewService` so T3 only adds the `Delete` method). `cache.New(rdb, logger)` + `GetJSON/SetJSON/Delete` used consistently in T5. `WorkQueue({concurrency,maxRetries,baseDelayMs})` matches its tests. ✓
- **Placeholder scan:** the `Export` "…one block per section…", the router pool-threading, and the schema-confirmation NOTEs are explicit "inventory the real queries / confirm the generated type" instructions, not gaps. The security task (T7) is intentionally a review-then-fix process, not pre-written code. ✓
- **Adjustment for T2/T3 seam:** define `dsr.NewService(q *sqlc.Queries, pool *pgxpool.Pool)` in Task 2 (pool unused until Task 3) so the signature is stable across both tasks.

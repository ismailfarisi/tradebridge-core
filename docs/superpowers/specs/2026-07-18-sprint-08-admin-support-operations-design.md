# Sprint 8 — Admin Console & Support Operations (design spec)

**Date:** 2026-07-18
**Sprint:** 8 of 10 · Tracks: Core · Frontend
**Repos:** `tradebridge-core`, admin surface in `tradebridge-core/web`
**Depends on:** S3 (verification, reports), S4 (disputes, reviews), S7 (activation events — for S10 analytics, not here)

## Goal

Give an operator one internal console to run, moderate, and support the business before public
launch: manage users & companies (search / view / **suspend**), work the existing review queues,
browse a unified audit log, and **impersonate** a user read-only for support — all audited.

## Locked decisions (user, 2026-07-18)

- **Ops-only scope.** Business analytics (GMV/MRR/active/funnel/churn) deferred to **S10**. Also
  deferred (not in S8): general feature-flag/config system, in-app helpdesk/support widget, live
  Stripe refunds/credits.
- **Impersonation = read-only view-as** — scoped, short-TTL, fully audited; all writes blocked.
- **Refunds deferred** entirely (Stripe not activated on live).
- **Company-level suspend only** — user-level suspend deferred (YAGNI; company suspend is the lever).
- Suspension enforced **immediately** via a per-request active-company guard (correctness over the
  micro-cost of one indexed PK lookup; a cache is an S9 concern).

## Existing foundation (do NOT rebuild)

The platform-superadmin realm already exists and is mounted fail-closed when `ADMIN_JWT_SECRET` is
set (`internal/httpapi/router.go` admin block):

- `internal/admin` — admin login/me, `PlatformAdminGuard`, `ScopePlatformAdmin`, HS256 admin tokens.
- `internal/trust/admin.go` — verification review (`/admin/v1/companies/:id/verification`,
  `/admin/v1/verifications`).
- `internal/trust/reports.go` — abuse/report moderation queue (`/admin/v1/reports`).
- `internal/disputes/admin.go` — dispute list + resolve (`/admin/v1/...`).
- `internal/reviews` admin handler; `internal/entitlements/admin.go` (`/admin/v1/plans`);
  `internal/settings` (LLM config).
- `audit_logs` table (migration 006): `id, company_id NOT NULL REFERENCES companies(id), actor,
  action, target_id, detail JSONB, created_at`; index `idx_audit_company (company_id, created_at DESC)`.
  Written today only by the gateway on every validated agent command
  (`internal/gateway/commands.go` `audit(...)`).
- Web: `web/app/admin/` — `page.tsx` (settings), `login/`, `disputes/`; `web/components/AdminShell.tsx`;
  `web/lib/admin.ts`.
- User JWT `Claims{CompanyID, UserID, Role, RegisteredClaims}` (`pkg/middleware/auth.go`); minted by
  `identity.TokenService.Issue` (`internal/identity/token.go`). `TenantGuard` +
  `CompanyID/UserID/Role` accessors in `pkg/middleware/tenant.go`.

## Global constraints

- All admin routes stay inside the existing `PlatformAdminGuard` realm under `/admin/v1/*`; nothing
  new is publicly reachable.
- `companies.status` values are exactly `active` | `suspended` (DB CHECK).
- Impersonation token TTL = **30 minutes**; carries `ReadOnly=true`; `EnforceReadOnly` blocks any
  method other than GET/HEAD with HTTP **403**.
- Every admin mutation (suspend/unsuspend, verification decision, dispute resolve, report resolve,
  impersonation issue) writes exactly one `audit_logs` row with `actor = "admin:<email>"` and
  `company_id = <target company>`.
- Tests use `tradebridge_core_test` — **never** live `tradebridge_core`. Full Go suite runs `-p 1`.
- Web vitest is node-env, `lib/**` only — testable logic lives in `lib`, not components.
- Work merges to `master` **locally, never pushed**. Commit trailers:
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` and the `Claude-Session:` line.

## Components

### 1. Company status + suspend enforcement (migration 028)

- Migration `028_company_status`: `ALTER TABLE companies ADD COLUMN status TEXT NOT NULL DEFAULT
  'active' CHECK (status IN ('active','suspended'))` + `CREATE INDEX idx_companies_suspended ON
  companies (status) WHERE status = 'suspended'`.
- sqlc: `GetCompanyStatus(id) -> text`, `SetCompanyStatus(id, status)`.
- `identity` login rejects a `suspended` company with 403 (no new token issued).
- `middleware.RequireActiveCompany(q)` runs after `TenantGuard` on the authed tenant group: looks up
  the request company's status; 403 `company suspended` when suspended. One indexed PK lookup.

### 2. Companies management API (`internal/adminconsole`)

New package, mounted under `adminGuard`:

- `GET /admin/v1/companies?q=&status=&limit=` — case-insensitive match on name or id; optional status
  filter; capped limit (default 50, max 200). Returns `{companies: [{id, name, type, status,
  verification_status, plan_code, created_at}]}`.
- `GET /admin/v1/companies/:id` — detail: base company, plan_code, verification_status, users
  (`[{id, email, role}]`), and counts `{rfqs, quotes, orders}`.
- `POST /admin/v1/companies/:id/suspend` `{reason}` and `POST /admin/v1/companies/:id/unsuspend` —
  set status, audit `company.suspend` / `company.unsuspend` with `{reason}` in detail.

### 3. Audit helper + admin-action logging + viewer (`internal/audit`)

- New `internal/audit` package: `Admin(ctx, q *sqlc.Queries, adminEmail, action, targetCompanyID
  string, detail map[string]any)` — best-effort insert (log-on-error, never fail the request),
  actor stored as `"admin:"+adminEmail`. Reuses the existing `CreateAuditLog` query.
- Wire `audit.Admin` into the mutations that don't log today: trust verification decision, dispute
  resolve, report resolve, and the new suspend/unsuspend + impersonate. Admin email comes from the
  admin claims (thread it through each handler).
- Viewer: `GET /admin/v1/audit?company_id=&actor=&action=&limit=` — new sqlc `ListAuditLogs` with
  optional filters (all-optional via `sqlc.narg`), ordered `created_at DESC`, capped limit.

### 4. Read-only support impersonation

- `middleware.Claims` gains `ReadOnly bool `json:"ro,omitempty"`` and `ActingAdmin string
  `json:"imp,omitempty"``. Existing tokens (no fields) deserialize unchanged.
- `identity.TokenService.IssueImpersonation(companyID, userID, role, adminEmail string)` — 30-minute
  TTL, `ReadOnly:true`, `ActingAdmin:adminEmail`. (TTL is independent of the normal login TTL.)
- `middleware.EnforceReadOnly()` after `TenantGuard`: if `claims.ReadOnly` and method ∉ {GET, HEAD}
  → 403 `read-only session`.
- `POST /admin/v1/impersonate` `{company_id}` — resolves an `owner` user of that company (404 if the
  company has no user), mints the impersonation token, audits `support.impersonate`, returns
  `{token, company_id, user_id, expires_at}`.

### 5. Unified `/admin/*` console (web)

- Extend `AdminShell` nav: Companies · Verifications · Disputes · Reports · Audit · Settings.
- `web/lib/admin.ts` gains typed clients: `listCompanies`, `getCompany`, `suspendCompany`,
  `unsuspendCompany`, `listVerifications`, `resolveVerification`, `listReports`, `resolveReport`,
  `listAudit`, `impersonate`. Pure helpers (e.g. audit filter builder) are vitest-covered.
- New pages: `/admin/companies` (search + list), `/admin/companies/[id]` (detail + suspend/unsuspend
  + **Impersonate** button), `/admin/verifications`, `/admin/reports`, `/admin/audit`.
- Impersonation hand-off: the button calls `POST /admin/v1/impersonate`, then opens the buyer/seller
  app carrying the read-only token, which renders a persistent "Viewing as <company> — read-only
  (support)" banner. Writes are already blocked server-side; the banner + disabled primary actions
  are the client courtesy.

### 6. E2E #8 (`test/integration/e2e_08_admin_ops_test.go`)

Seed an admin + a company with a pending verification and an open dispute. Then, through the real
HTTP API under the admin realm:

1. Approve the verification → assert 200 and one `audit_logs` row `action='company.verification'`
   (or the existing action name) for the target company with `actor LIKE 'admin:%'`.
2. Resolve the dispute → assert 200 and an audit row for `dispute.resolve`.
3. Impersonate the company → assert 200 + token; a mutating call with the token → **403**; a GET with
   the token → 200; assert an audit row `support.impersonate`.
4. `GET /admin/v1/audit?company_id=<id>` returns all three rows.

## Testing strategy

- Per-plan TDD: Go table + integration tests (`tradebridge_core_test`), web vitest on `lib`.
- Suspension, read-only enforcement, and audit-on-mutation each get a direct unit/integration test.
- Full Go suite green `-p 1`; web tsc + vitest + `next build` clean; E2E #8 green.

## Out of scope (deferred, do not build)

- Business analytics dashboards (GMV/MRR/active/funnel/churn) — **S10** (compute on-the-fly SQL over
  orders / subscriptions / activation_events when built).
- Feature-flag / config management system.
- In-app support/helpdesk widget & contact→inbox hooks.
- Live Stripe refunds/credits.
- User-level (as opposed to company-level) suspend.

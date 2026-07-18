# Sprint 9 — Scale, Security & Compliance Hardening (design spec)

**Date:** 2026-07-18
**Sprint:** 9 of 10 · Tracks: Core · Security · Compliance · Agent
**Repos:** `tradebridge-core` (+ `web/`), `tradebridge-agent`
**Depends on:** Sprints 1–8 (hardening the finished surface)

## Goal

Make the platform legally launch-safe and burst-resilient in the code we control: GDPR data-subject
rights (export + deletion), launch legal pages + cookie consent, a security review with findings
fixed, caching for hot read paths, and an agent autonomy queue so RFQ bursts never drop quotes.

## Locked decisions (user, 2026-07-18)

- **Scope = everything code-buildable:** GDPR DSR + legal/cookie + security review & fixes + Core
  caching + agent autonomy queue/backpressure. **Deferred to ops/runbook (NOT code, env-blocked):**
  PgBouncer, Redis scaling, WAF/bot protection, secrets manager, load-test-at-scale, automated
  backups, incident/on-call.
- **Agent SQLite→Postgres deferred to S10.** The agent stays on SQLite; the queue/backpressure work
  lands on SQLite. (Self-host stays SQLite-only; the managed multi-instance move is S10.)
- **Account deletion = soft-delete + anonymize** — retain non-PII transactional records for
  counterparty integrity + legal/tax; scrub PII; block access.
- **Legal text = draft boilerplate**, banner-flagged "DRAFT — needs legal review before launch".
- **Deletion requires owner role + password re-confirmation.**

## Global constraints

- DSR + legal routes: export/delete are tenant-authed (`/v1/account/*`); legal pages are public.
- `companies.deleted_at TIMESTAMPTZ` nullable; login and `RequireActiveCompany` reject when it is
  set (reuses the S8 suspension rail — same 403 path).
- Deletion anonymizes: `companies.name` → `"Deleted company " || left(id,8)`; `users.email` →
  `"deleted+" || id || "@deleted.invalid"`, `password_hash` → NULL; `supplier_profiles.description`
  / `location` → NULL; agent credentials revoked. Transactional
  rows (orders, invoices, quotes, disputes, reviews, RFQs) are RETAINED.
- Deletion is audited via the S8 `audit.Auditor` (`account.delete`, actor = the deleting user).
- Cache layer is **fail-open**: a nil or unreachable Redis makes every cache op a pass-through
  (mirrors `middleware.NewRedisRateCounter`). Cached values are JSON with a TTL; writes invalidate.
- Agent queue: bounded concurrency, retry-with-backoff on transient failure, idempotency via the
  existing `ProcessedEvent` entity — a burst or crash never double-quotes or drops an RFQ.
- Legal pages carry a visible "DRAFT — not legal advice, pending review" banner.
- Tests: Core → `tradebridge_core_test` (NEVER live), full Go suite `-p 1`; agent → `bun test`;
  web → vitest node-env `lib/**` only. Work merges to `master` **locally, never pushed** (both
  repos). Commit trailers: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` +
  `Claude-Session: https://claude.ai/code/session_01JrKi3VYFWQJPRvZGwScMqh`.

## PII / data footprint (for DSR)

Company-identifying / PII columns: `companies.name`; `users.email` + `users.password_hash` (added
in `002_auth`); `supplier_profiles.description` / `location`; `notification_prefs.email`. A
company's data footprint (for export) spans every table referencing it: users, supplier_profiles,
catalog, rfqs, rfq_matches, rfq_views, quotes, orders, order_events, invoices, reviews, disputes,
reports (as reporter/reported), blocks, notifications, subscriptions, activation_events,
agent_credentials, audit_logs, threads/messages.

## Components

### 1. GDPR DSR (Core `internal/dsr` + web) — migration 029

- **Migration 029:** `ALTER TABLE companies ADD COLUMN deleted_at TIMESTAMPTZ` (nullable) +
  `CREATE INDEX idx_companies_deleted ON companies (deleted_at) WHERE deleted_at IS NOT NULL`.
- **Enforcement:** `identity` login rejects a company with `deleted_at` set (403, after password
  check). `RequireActiveCompany` (S8) also rejects deleted companies — extend it to block when
  status suspended OR `deleted_at` set. (Fetch both via one row; add `GetCompanyGate(id) ->
  {status, deleted_at}` or extend `GetCompanyStatus`.)
- **`internal/dsr` service:**
  - `Export(ctx, companyID) (Bundle, error)` — read-only, gathers the footprint above into a
    structured `Bundle` (map of section → rows). New sqlc `:many` queries per section as needed
    (reuse existing `...ByCompany` queries where present).
  - `Delete(ctx, companyID, actingUserID) error` — in ONE transaction: set `deleted_at = NOW()`,
    anonymize the PII columns (per Global constraints), revoke agent credentials. Idempotent
    (no-op if already deleted).
- **Handler** `internal/dsr/handler.go`: `GET /v1/account/export` (authed owner/admin) →
  `{bundle}`; `POST /v1/account/delete` `{password}` (authed **owner**; re-verify the acting user's
  password via `identity.CheckPassword`; 403 if wrong/not owner) → 200, audited `account.delete`.
- **Web:** `app/(app)/account/page.tsx` — "Export my data" (fetches export, triggers a JSON
  download) + "Delete account" (owner-only; typed "DELETE" confirmation + password field →
  `POST /v1/account/delete`, then clears the token and redirects home). `lib/account.ts` client +
  a pure helper (e.g. `canDelete(role)`) with vitest.

### 2. Legal pages + cookie consent (web)

- `app/(marketing)/legal/{terms,privacy,cookies}/page.tsx` — static server components, draft
  boilerplate with a persistent "DRAFT — needs legal review" banner + placeholders (entity name,
  jurisdiction, contact email) + a subprocessor list on the privacy page.
- `components/CookieConsent.tsx` — a bottom banner (Accept / Reject non-essential), choice persisted
  in `localStorage` (`tb_cookie_consent`); a pure `lib/consent.ts` (`readConsent`/`writeConsent`/
  `needsConsent`) with vitest. Rendered from the root layout; footer links to the three legal pages.
- No non-essential trackers exist today — consent is the gate for any future analytics.

### 3. Security review + fixes (both repos)

- An adversarial security-review pass (subagents) over Core + agent: auth/session, tenant isolation,
  injection, SSRF, secret handling, and the NEW DSR/account surfaces; plus a dependency audit
  (`go list -m all` / `govulncheck` if available; `bun`/npm advisories). Produce a findings report
  (`docs/superpowers/specs/2026-07-18-sprint-09-security-findings.md`), **fix Critical/High**, and
  document Medium/Low with a rationale. Bounded: findings + fixes, not an open-ended rewrite.

### 4. Core cache layer (Core `internal/cache`)

- `cache.New(rdb *redis.Client, logger) *Cache`; `GetJSON(ctx, key, dest) (bool, error)`,
  `SetJSON(ctx, key, v, ttl)`, `Delete(ctx, keys...)`. Fail-open: nil/unreachable Redis → miss /
  no-op, never an error that breaks the request.
- Apply to hot reads: public discovery results (short TTL ~30–60s, key = normalized query+filters);
  public storefront profile (longer TTL, invalidated on that company's profile/catalog write);
  buyer/seller dashboards (short per-company TTL). Each write path that changes cached data calls
  `Delete` for the affected keys. Cache is a read-through optimization only — correctness must not
  depend on it (fail-open verified by a test with a nil client).

### 5. Agent autonomy queue / backpressure (`tradebridge-agent`)

- Introduce a bounded work queue around the SSE→reasoning path (`src/reasoning/sse.ts` feeds
  `src/reasoning/handler.ts`): a fixed max-concurrency worker pool (config, default e.g. 3),
  **retry with exponential backoff** on transient errors (LLM/network), and **backpressure** so
  intake doesn't outrun processing. Idempotency via the existing `ProcessedEvent` entity — an event
  already processed is skipped; an in-flight event that fails is retried, not dropped. Unit-tested
  with `bun test` (queue drains in order under a concurrency cap; a failing job retries then
  dead-letters after N attempts without blocking the pool).

### 6. E2E #9 (`test/integration/e2e_09_dsr_test.go`)

Through the real HTTP API: register a company (+ owner), seed an RFQ and an order. Then:
1. `GET /v1/account/export` → 200; assert the bundle contains the company, the seeded RFQ, and the
   order.
2. `POST /v1/account/delete {password}` as the owner → 200; assert an `account.delete` audit row.
3. Assert `companies.deleted_at` is set, `companies.name` anonymized, the owner's `users.email`
   anonymized + `password_hash` NULL.
4. A fresh login for that company → **403** (deleted).
5. The seeded order/RFQ rows STILL EXIST (retained for counterparty integrity).
6. Deletion with a wrong password or a non-owner → **403** (guard test).

## Testing strategy

Per-plan TDD. Core: Go table + integration (`tradebridge_core_test`); the DSR export/delete and
cache fail-open each get direct tests. Agent: `bun test` for the queue. Web: vitest on `lib`
(account, consent helpers). Spanning E2E #9. Full Go suite green `-p 1`; agent bun suite green; web
tsc + vitest + `next build` clean.

## Out of scope (deferred)

- **Infra/ops (runbook + your work, not code):** PgBouncer, Redis scaling, WAF/bot protection,
  secrets manager / key rotation, load test at target concurrency, automated DB backups + restore
  drill, incident process / on-call.
- **Agent SQLite→Postgres** — S10 (multi-instance prerequisite).
- Real lawyer-reviewed legal copy (S9 ships draft boilerplate, banner-flagged).
- Hard-delete / physical erasure (S9 does soft-delete + anonymize).

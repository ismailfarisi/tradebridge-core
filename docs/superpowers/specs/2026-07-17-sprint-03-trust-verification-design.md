# Sprint 3 — Trust & Safety I: Verification, Profiles & Abuse Controls — Design

**Date:** 2026-07-17
**Sprint:** 3 of 10 (`/home/deploy/b2b/sprints/sprint-03-trust-verification.md`)
**Repos:** `tradebridge-core`, `tradebridge-core/web`
**Depends on:** Sprint 1 (notifications/verify), Sprint 2 (entitlements/admin realm)

## Goal

Make it safe to deal with a stranger. A seller proves they control a real domain and earns a
**verified badge** shown on quotes, search results, and a **public storefront**. Unverified sellers
can be **excluded from the RFQ match pool** (config-gated, default off during pilot). Buyers can
**report and block** bad actors, and abuse attempts (spam RFQs/quotes/messages, rapid signups) are
**rate-limited**.

## Locked decisions (user, 2026-07-17)

1. **Scope: code now, ops deferred.** Build verification+badges, public profiles, and abuse controls
   (rate limits + report/block) this sprint. Standing up a prod-parity **staging environment** and
   **onboarding 5–10 design-partner sellers** are a separate ops session — documented here, not coded.
2. **Verification: automated domain verification only** — self-serve, instant, no admin bottleneck on
   the happy path. Method is **DNS-TXT**. A minimal admin **override/revoke** surface remains as an
   escape valve.
3. **Abuse controls: limits + report/block only.** The free-text **content-moderation guard**
   (profanity/PII/link) is **deferred** to a later sprint.
4. **Match-pool gate: config-gated, default OFF.** Unverified sellers keep matching during pilot; flip
   the flag on at public launch.

## Architecture

- New package **`internal/trust/`** — verification service + handlers, reports, blocks.
- New **`internal/middleware`** rate limiter (Redis-backed, `internal/config` already exposes Redis).
- **`companies.verification_status`** is the single source of truth for the badge; `supplier_profiles.verified`
  is kept in sync so the existing search `ORDER BY sp.verified DESC` keeps working.
- The match-pool gate installs in the existing `MatchSuppliers` query (`pkg/db/query/rfq.sql:51`),
  fed by a config flag.
- All frontend lands in **`core/web`** (the buyer app). Companies are both-role, so a selling company
  manages verification + storefront from its authenticated settings there; the public storefront is a
  public route in the same app. **No dependency on the deferred seller app.**

### Why DNS-TXT and not an HTTP-file method

An HTTP-file check (`GET https://<domain>/.well-known/...`) fetches a **user-supplied URL server-side**
→ a Server-Side Request Forgery vector: a seller claims `domain = 169.254.169.254` (cloud metadata),
`127.0.0.1`, or an internal host, and the server fetches it. DNS-TXT (`net.LookupTXT`) has no such
surface, and it is the industry standard for domain-ownership proof (Google/AWS/Stripe). The HTTP-file
method is **deferred**; when built it needs private/loopback/link-local IP-range guards, redirect
disabling, and response-size caps.

## Data model (migration 017)

```sql
-- Verification state lives on the company (both-role: gates buying and selling identity).
ALTER TABLE companies
    ADD COLUMN verification_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (verification_status IN ('unverified','pending','verified','rejected')),
    ADD COLUMN verified_domain TEXT,
    ADD COLUMN verified_at     TIMESTAMPTZ;

-- One domain can only be *verified* by one company at a time (pending claims may collide).
CREATE UNIQUE INDEX idx_companies_verified_domain
    ON companies (verified_domain) WHERE verification_status = 'verified';

-- Active domain-ownership challenge(s). One row per (company, domain) claim.
CREATE TABLE domain_verifications (
    id              TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id      TEXT NOT NULL REFERENCES companies(id),
    domain          TEXT NOT NULL,
    token           TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','verified','failed')),
    attempts        INT  NOT NULL DEFAULT 0,
    last_checked_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    verified_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_domain_verifications_company_domain
    ON domain_verifications (company_id, domain);
CREATE INDEX idx_domain_verifications_company ON domain_verifications (company_id, created_at DESC);

-- Abuse reports: one company reports another. Admin resolves.
CREATE TABLE reports (
    id                  TEXT PRIMARY KEY DEFAULT gen_ulid(),
    reporter_company_id TEXT NOT NULL REFERENCES companies(id),
    reported_company_id TEXT NOT NULL REFERENCES companies(id),
    reason              TEXT NOT NULL
        CHECK (reason IN ('spam','fraud','abuse','inappropriate','other')),
    detail              TEXT,
    status              TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','reviewed','dismissed','actioned')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at         TIMESTAMPTZ
);
CREATE INDEX idx_reports_status ON reports (status, created_at DESC);
CREATE INDEX idx_reports_reported ON reports (reported_company_id);

-- Blocks: blocker company hides / de-matches a blocked company (one direction stored;
-- matching filters BOTH directions).
CREATE TABLE blocks (
    id                 TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id         TEXT NOT NULL REFERENCES companies(id),
    blocked_company_id TEXT NOT NULL REFERENCES companies(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (company_id, blocked_company_id),
    CHECK (company_id <> blocked_company_id)
);
CREATE INDEX idx_blocks_company ON blocks (company_id);
CREATE INDEX idx_blocks_blocked ON blocks (blocked_company_id);
```

Migration `017_trust.down.sql` drops the tables/indexes and the three `companies` columns.

## Verification flow

1. **Claim** — `POST /v1/verification/domain {domain}` (owner/admin, `RequireVerified` email).
   - Validate domain syntax (lowercased, no scheme/path, must contain a dot, length cap). Reject
     public-suffix-only inputs (e.g. `com`).
   - Generate `token` = 32 bytes base32/hex (crypto/rand). Upsert `domain_verifications` row for
     `(company, domain)` → `status=pending, attempts=0`, regenerate token on re-claim.
   - Set `companies.verification_status='pending'` (only if not already `verified`).
   - Response: `{domain, record_name: "_tradebridge-verify.<domain>", record_type: "TXT",
     record_value: "tradebridge-verify=<token>"}`.
2. **Check** — `POST /v1/verification/domain/check {domain}` (owner/admin; **rate-limited**).
   - Load the pending row; `attempts++`, stamp `last_checked_at`.
   - Resolve via injected `TXTResolver.LookupTXT(ctx, "_tradebridge-verify."+domain)` with a bounded
     timeout (config `VERIFY_DNS_TIMEOUT`, default 5s). Match if any returned record equals
     `tradebridge-verify=<token>` (trim, case-insensitive on the prefix).
   - **Match** → in one tx: `domain_verifications.status='verified'`+`verified_at`; `companies`
     `verification_status='verified', verified_domain=domain, verified_at=NOW()`; sync
     `supplier_profiles.verified=true`; emit `company.verified` event + notification. Returns the new
     status. If the partial-unique index rejects (domain already verified elsewhere) → 409
     `{code:"domain_taken"}`.
   - **Miss** → row stays `pending`; return 200 `{status:"pending", hint:"TXT record not found yet…"}`
     (actionable, not an error — DNS propagation is normal).
3. **Status** — `GET /v1/verification` → current `verification_status`, `verified_domain`, and any
   pending claim (domain + the record to add).
4. **Admin override/revoke** — `GET /admin/v1/verifications?status=` (list companies + claims) and
   `POST /admin/v1/companies/:id/verification {status, reason}` where status ∈
   `verified|rejected|unverified`. Manually verify a seller who can't do DNS, or revoke a bad actor
   (revoke also clears `verified_domain` and syncs `supplier_profiles.verified=false`). Reuses the
   platform-admin auth on the existing `/admin` realm.

`TXTResolver` is an interface (`LookupTXT(ctx, name) ([]string, error)`) with a `net.Resolver`-backed
default and a fake for tests — no real DNS in the suite.

## Match-pool gate

Config `VERIFY_REQUIRED_FOR_MATCH bool` (default **false**). `MatchSuppliers` gains a boolean arg;
when true the query adds `AND c.verification_status = 'verified'`. `findMatches`
(`internal/rfq/service.go:301`) passes `cfg.VerifyRequiredForMatch`. Default-off keeps the pilot pool
full; launch flips it on.

## Public storefront

- `GET /public/v1/sellers/:companyID` — **unauthenticated**, rate-limited. Returns tenant-safe fields
  only: `{company_id, name, verification_status, verified_domain, description, categories, location,
  catalog: [{name, category, summary}], reputation: null}`. `reputation` is an explicit null slot
  (ratings land Sprint 4). 404 if the company has no `supplier_profiles` row or does not exist. **No**
  users, emails, RFQs, subscription, or other private fields.
- Seller edits profile via the existing authed `UpsertSupplierProfile` (`/v1/suppliers/profile`).
- Frontend route `/s/[companyID]` (ULID in URL; pretty slugs deferred).

## Abuse controls

### Rate limiter (`internal/middleware`)

- Redis-backed **fixed-window** counter, key `rl:<scope>:<bucket>:<window>` where `bucket` is client
  IP (from `X-Forwarded-For` left-most / `c.IP()`) and, where the caller is authenticated, also the
  company id. Per-route `{limit, window}` config.
- Applied to: **signup**, **RFQ create**, **quote submit** (gateway command), **quote messages**, and
  **verification check**. Limits come from config with sane defaults (e.g. signup 5/hour/IP, RFQ
  create 20/hour/company, check 10/hour/company).
- **Fails open** (logs `warn`) if Redis errors/unreachable — a Redis blip must not take the
  marketplace down. On limit exceeded → 429 `{code:"rate_limited", retry_after_seconds}`.
- `RATE_LIMIT_ENABLED bool` (default true) master switch for tests/local.

### Reports

- `POST /v1/reports {reported_company_id, reason, detail}` (authed, `RequireVerified`, rate-limited).
  Can't report yourself. Inserts `status=open`.
- Admin: `GET /admin/v1/reports?status=` (list) and `POST /admin/v1/reports/:id {status}` to resolve
  (`reviewed|dismissed|actioned` + `resolved_at`).

### Blocks

- `POST /v1/blocks {blocked_company_id}` / `DELETE /v1/blocks/:blockedCompanyID` / `GET /v1/blocks`
  (authed). UNIQUE prevents dup; can't block yourself.
- **Effect:** in `findMatches`, after candidate scoring, drop any seller where a block exists in
  **either direction** with the buyer (load the buyer's block set + companies that blocked the buyer;
  post-query filter in Go — simpler and safer than complicating `MatchSuppliers` SQL). Blocked
  companies are also omitted from `SearchSuppliers` results for the requesting company.

## Config additions (`internal/config`)

| Key | Default | Purpose |
|---|---|---|
| `VERIFY_REQUIRED_FOR_MATCH` | `false` | match-pool gate |
| `VERIFY_DNS_TIMEOUT` | `5s` | DNS lookup timeout |
| `RATE_LIMIT_ENABLED` | `true` | master switch |
| `RATE_LIMIT_SIGNUP` | `5/1h` | per IP |
| `RATE_LIMIT_RFQ` | `20/1h` | per company |
| `RATE_LIMIT_QUOTE` | `60/1h` | per company |
| `RATE_LIMIT_MESSAGE` | `120/1h` | per company |
| `RATE_LIMIT_VERIFY_CHECK` | `10/1h` | per company |

All default to safe values; nothing new is required in `.env` for the app to boot.

## Frontend (`core/web`)

- `/settings/verification` — claim domain, show the DNS TXT record to add, **Verify now** button,
  live status badge, and (if verified) the verified domain.
- `/settings/storefront` — edit description/categories/location; link to the public page.
- `/s/[companyID]` — public storefront: name + `<VerifiedBadge>`, about, catalog showcase, reputation
  slot placeholder ("Ratings coming soon"), and **Report**/**Block** affordances.
- `<VerifiedBadge>` component — surfaced on **quotes** (buyer's quote-compare view), **search
  results**, and the **storefront**. Quote and search read APIs are extended to include the seller's
  `verification_status`.
- Report modal (reason + detail) and Block/Unblock buttons.
- Lib logic (`lib/trust.ts`) → vitest; pages/components → `tsc --noEmit` + `next build` (Sprint 1/2
  precedent, no RTL).

## Testing strategy

- **Go:** sqlc query tests; verification service with a **fake `TXTResolver`** (match / miss /
  domain-taken / revoke); rate-limit middleware against **miniredis** (allow, exceed→429, Redis-down→
  fail-open); reports + blocks CRUD + self-guards; match-pool gate (flag off→included, on→excluded)
  and block-filtering (both directions); public storefront field-allow-list (assert no private fields
  leak).
- **Frontend:** `lib/trust.ts` unit tests; page/route builds green.
- **E2E #3 (capstone, Plan 3):** seller unverified → excluded from match when the gate is on;
  seller verifies (fake resolver) → `verification_status` flips to `verified`, badge data present on
  quote + storefront reads, and now included in the match pool.

## Decomposition — one spec → 4 plans

1. **Verification spine** — migration 017 (companies columns + `domain_verifications`), `internal/trust`
   verification service (injectable `TXTResolver`), claim/check/status endpoints, admin override/revoke,
   `supplier_profiles.verified` sync, match-pool gate config + `MatchSuppliers` arg.
2. **Public storefront** — `GET /public/v1/sellers/:id` read API (tenant-safe allow-list), profile-edit
   wiring, and `verification_status` surfaced on quote + search read APIs (badge data).
3. **Abuse controls** — rate-limit middleware (Redis + miniredis tests), reports (+ admin queue),
   blocks (+ match/search filtering), and the capstone **E2E #3**.
4. **Frontend** — verification & storefront settings, public `/s/[id]`, `<VerifiedBadge>`,
   report/block UI; `lib/trust.ts`.

## Deferred (documented, not built this sprint)

- HTTP-file verification method (+ SSRF guards: private-IP-range block, no redirects, size caps).
- Free-text content-moderation guard (profanity/PII/link).
- Staging environment stand-up + design-partner onboarding (ops session).
- Pretty profile slugs (`/s/acme` instead of ULID).
- Background re-check sweeper for pending verifications (pull-based only for now).

## Definition of Done

A seller completes DNS-TXT verification and earns a badge shown on every quote and their public
storefront. Unverified sellers can be excluded from matching via one config flag. A buyer can
report and block a bad actor, blocked pairs stop matching, and abuse attempts (spam RFQs, rapid
signups) are throttled. E2E #3 is green.

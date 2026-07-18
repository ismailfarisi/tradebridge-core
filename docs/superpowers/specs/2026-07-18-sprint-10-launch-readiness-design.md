# Sprint 10 — Launch Readiness: Analytics, Waitlist Invites, Feedback, SEO (design spec)

**Date:** 2026-07-18
**Sprint:** 10 of 10 (final) · Tracks: Core · Admin/Frontend · Data
**Repos:** `tradebridge-core` (+ `web/`)
**Depends on:** Sprints 1–9 (the finished, hardened product)

## Goal

The final, launch-readiness slice we can build as code: platform business analytics (the deferred S8
dashboard), the waitlist-invite flow, an in-app feedback/NPS loop, SEO/OG basics, a capstone
full-journey E2E, and a Prometheus `/metrics` endpoint. Open the product to measure, learn, and be
found — without the infra we can't build here.

## Locked decisions (user, 2026-07-18)

- **Keep the agent on SQLite** — the SQLite→Postgres / multi-instance move is **post-launch**, not S10.
- **Marketing/GTM site deferred** ("later") — no landing/pricing/features/docs/changelog rebuild this
  sprint; but cheap SEO/OG plumbing on the EXISTING public pages is in scope.
- **Doors stay open** — public self-serve registration is ALREADY a public route; no new gate. The
  feature-flag for "opening" is de-scoped as redundant; the valuable launch action is the
  waitlist-invite flow.
- **Business analytics = on-the-fly SQL** (the S8 choice), in the admin console.
- Include the optional Prometheus `/metrics` endpoint.

## Global constraints

- Analytics + waitlist-invite + feedback-read routes mount under the existing `/admin/v1/*`
  `PlatformAdminGuard` realm (S8). The feedback WRITE route is tenant-authed under `/v1/*`.
- Analytics is on-the-fly SQL over existing tables — no rollup tables, no external engine. Read-only.
- **MRR honesty:** the `plans` table has no price column and Stripe is not activated, so MRR cannot
  be computed from live data today. Add nullable `plans.price_cents_month` / `price_cents_year`
  (migration); MRR = Σ(active subscription × its plan's price) computed ONLY when prices are set;
  otherwise the dashboard shows plan-mix + a "prices unset" note. Never fabricate MRR.
- `/metrics` is unauthenticated exposition (Prometheus text format) of request golden signals; it
  must not leak tenant data (labels are method/route-template/status only, never ids).
- Feedback/NPS: `score` is an integer 0–10 for `kind='nps'`; `message` free text for
  `kind='feedback'`. Tenant-scoped writes.
- Tests: Core → `tradebridge_core_test` (NEVER live), full Go suite `-p 1`; web → vitest node-env
  `lib/**` only. Migrations continue at 030. Work merges to `master` **locally, never pushed**.
  Commit trailers: `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` +
  `Claude-Session: https://claude.ai/code/session_01JrKi3VYFWQJPRvZGwScMqh`.

## Components

### 1. Business analytics (Core `internal/analytics` + admin web) — migration 030 (plan prices)

- **Migration 030:** `ALTER TABLE plans ADD COLUMN price_cents_month INTEGER; ADD COLUMN
  price_cents_year INTEGER;` (both nullable — MRR is computed only when set).
- **`internal/analytics` service** (admin realm), on-the-fly SQL:
  - **GMV** — `SUM(orders.total_cents)` excluding cancelled, total + a per-period (last 12 weeks or
    months) time series.
  - **Active companies** — distinct buyers (RFQ/order in trailing 30d) + distinct sellers
    (quote/order in trailing 30d).
  - **Activation funnel** — count of distinct companies per `activation_events.event` milestone.
  - **Churn** — active subscriptions vs `canceled` (and `cancel_at_period_end=true`) in the period.
  - **Plan mix** — active subscription count per plan+status. **MRR** — Σ over active/trialing subs
    of the plan's `price_cents_month` (year prices ÷ 12); `null` (with `prices_set:false`) when any
    active plan lacks a price.
- **Handler:** `GET /admin/v1/analytics/business` → a single JSON summary. Web: `/admin/analytics`
  page (tiles: GMV, active buyers/sellers, MRR-or-"prices unset", churn %) + a funnel list + a small
  GMV sparkline/time-series table. Add "Analytics" to the AdminShell nav.

### 2. Waitlist invite flow (Core + admin web) — migration 031

- **Migration 031:** `ALTER TABLE waitlist_signups ADD COLUMN invited_at TIMESTAMPTZ;`
- Admin: `GET /admin/v1/waitlist?status=pending|invited|all` (list, capped) + `POST
  /admin/v1/waitlist/invite {ids?: []}` — send an invite/announce email via the existing
  `notify.Sender` to the given signups (or all pending when `ids` omitted), set `invited_at`, audited
  (`waitlist.invite`, via the S8 auditor — target company N/A, so write the audit row directly with
  actor `admin:<email>` and a synthetic detail; OR skip audit for waitlist since there's no company
  FK — audit_logs.company_id is NOT NULL, so DO NOT audit waitlist invites through the company-keyed
  audit table; instead just log). Web: `/admin/waitlist` page (list + select + "Invite" / "Invite all
  pending"). Email send is best-effort per recipient; the response reports counts.

### 3. In-app feedback + NPS (Core + web) — migration 032

- **Migration 032:** `CREATE TABLE feedback (id, company_id, user_id, kind TEXT CHECK (kind IN
  ('feedback','nps')), score INTEGER, message TEXT, created_at)`.
- Core: `POST /v1/feedback {kind, score?, message?}` (tenant-authed) — validate (`nps` needs
  `score` 0–10; `feedback` needs non-empty `message`); insert. Admin: `GET /admin/v1/feedback` (list,
  capped, newest first) + a simple NPS summary (promoters/passives/detractors, NPS score).
- Web: `lib/feedback.ts` client + pure `npsCategory(score)` / `npsScore(rows)` helpers (vitest); a
  `<FeedbackWidget/>` (a small button in the app shell → modal: tab between "Send feedback" (textarea)
  and "Rate us" (0–10 NPS scale)) → `POST /v1/feedback`. Admin `/admin/feedback` page.

### 4. SEO / discoverability basics (web)

- Per-page `metadata` (title/description/canonical) on public routes; OpenGraph + Twitter-card tags
  (site-wide default in the root layout + per-page overrides on landing/pricing/marketplace/storefront/
  legal). A default OG image (static asset or a simple SVG). `app/sitemap.ts` (public URLs) +
  `app/robots.ts` (allow public, disallow `/admin`, `/dashboard`, `/account`, app routes). A pure
  `lib/analytics-consent.ts` `trackEvent(name, props)` that no-ops unless cookie consent is
  "accepted" (S9) and a provider is configured — a wired stub, no external provider added.

### 5. Capstone full-journey E2E + prod smoke (Core)

- `test/integration/e2e_10_full_journey_test.go`: buyer + seller register → seller profile + catalog
  → buyer RFQ → match → seller quote via the gateway → buyer accept → order + auto-invoice; assert an
  invoice row/number exists and the order is in the accepted/fulfilled state. The DoD's
  "full journey passes E2E." (Reuse the seed/harness helpers from earlier E2Es.)
- `scripts/smoke.sh` — a runnable prod smoke: curls the live health + a sample of guarded/public
  endpoints (health 200; `/v1/account/export` 401; `/public/v1/discovery` 200; `switeaz.com/` 200;
  agent health) and reports pass/fail. The "prod smoke + rollback plan" item as a script (rollback
  stays a runbook note).

### 6. Prometheus `/metrics` (Core)

- A Fiber middleware recording request count + duration histogram + in-flight, labelled by method,
  route template (NOT the raw path — no ids), and status class; `GET /metrics` exposes Prometheus
  text format (unauthenticated, no tenant data). Use `prometheus/client_golang` (add the dep) OR a
  tiny hand-rolled exposition if adding the dep is heavy — prefer the standard client. Golden signals
  (latency, errors, traffic; saturation = in-flight) so any scraper can be pointed at it.

## Testing strategy

Per-plan TDD. Core: Go table + integration (`tradebridge_core_test`) — analytics aggregations,
feedback validation, waitlist invite (email send stubbed/counted), `/metrics` exposition. Web: vitest
on `lib` (nps helpers, consent-gated trackEvent, sitemap/robots pure bits). The capstone E2E #10.
Full Go suite green `-p 1`; web tsc + vitest + `next build` clean.

## Out of scope (deferred / documented)

- **Marketing/GTM site** (landing/pricing/features/docs/changelog rebuild) — "later" per the user.
- **Agent SQLite→Postgres**, Docker, gateway-as-separate-service — post-launch scale.
- **CI/CD deploy gates** (neither repo pushed to GitHub), Grafana/dashboards/alertmanager, hosted
  status page, uptime monitoring — infra/ops runbook.
- **Real MRR** — needs plan prices set + Stripe activation.
- Referral/invite program; Stripe Connect transaction-fee rail; feature-flag gate (signup already
  open).

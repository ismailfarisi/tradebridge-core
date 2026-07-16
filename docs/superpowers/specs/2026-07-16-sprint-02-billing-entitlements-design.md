# Sprint 2 — Billing & Entitlements — Design

**Date:** 2026-07-16
**Sprint:** 2 of 10 · **Repos:** `tradebridge-core`, `tradebridge-agent`, `core/web`
**Depends on:** Sprint 1 (done)
**Source backlog:** `/home/deploy/b2b/sprints/sprint-02-billing-entitlements.md`

## Goal

Build the money machine and the feature-gating spine. Freemium: the marketplace core stays free;
**Pro unlocks agent autonomy**, **Business adds ERP/invoicing + seats**. Global / Stripe-first. Every
later sprint checks entitlements, so this lands early.

## Locked decisions (this session, 2026-07-16)

1. **Stripe behind an interface + test mode.** Mirror Sprint 1's `Sender` pattern: a `Billing`
   interface with a real `StripeBilling` (active when `STRIPE_SECRET_KEY` is set) and a deterministic
   `StubBilling` (used in tests and when the key is empty). **Full-account activation gates only live
   charges + real Stripe Tax**, not building/testing. **This session builds & tests stub-only** — no
   Stripe keys required; wiring real test-mode keys + a Stripe CLI webhook forward is a documented
   activation step.
2. **Whole sprint = one spec → 4 plans**, executed via subagent-driven TDD, **merged locally** (never
   pushed — consistent with Sprint 1).
3. **Agent-autonomy entitlement reaches the Bun agent via the gateway.** Core is the single source of
   truth; Core exposes the company's entitlements on the agent-authenticated gateway; the agent
   reads/caches and **fails closed** (Free/unset/unreachable → queue-for-review, never auto-send).
4. **Frontend in the buyer app (`core/web`) now; seller-app billing UI deferred** (needs an
   Agent-backend proxy — same cross-backend split that deferred Sprint 1 seller notifications).
5. **Entitlement limit values are platform-admin-editable**, not hardcoded. They live in a seeded
   `plans` DB table (defaults below) with platform-admin CRUD to tune them live without a migration.
   Stripe owns money (price IDs stay config); the platform admin owns feature limits.

## Architecture

Two new Core modules, both behind interfaces so the whole thing builds and unit-tests without any
external service (the Sprint 1 `Sender`/`LogSender` pattern):

- **`internal/entitlements/`** — the spine. Resolves a company → its current plan → its entitlement
  set (read from the `plans` table), exposes `RequireEntitlement` helpers, and meters usage.
- **`internal/billing/`** — Stripe. The `Billing` interface, `StripeBilling` (`stripe-go`),
  `StubBilling` (deterministic fake), checkout/portal/webhook handlers, and subscription→plan state.

**Plan resolution (single source of truth):** a company's plan is derived from its **active
subscription row**; **no active subscription = Free** (fail-closed). The only thing denormalized onto
`companies` is `stripe_customer_id`. Entitlement *values* for a plan come from the `plans` table.

## Plans & entitlements

Three plans. Entitlement values are the **seeded defaults** — editable by the platform admin. `-1`
means unlimited (∞).

| Entitlement (`plans` column) | Free | Pro | Business |
|---|---|---|---|
| `agent_autonomy` (bool) | false | true | true |
| `catalog_max_items` | 25 | 500 | -1 |
| `rfq_monthly_limit` | 10 | 100 | -1 |
| `seats` | 2 | 5 | -1 |

- **Billing intervals:** monthly + **annual (= 10× monthly price, "2 months free")**, optional 14-day
  trial on paid plans. Prices/intervals are Stripe price IDs in config — **not** in the `plans` table.
- **Both-role companies:** one company = one plan; entitlements are company-keyed, so a single plan
  gates both the buying side (`rfq_monthly_limit`) and the selling side (`agent_autonomy`)
  automatically. No per-side plans.

## Enforcement points (`RequireEntitlement`)

Over-limit / not-entitled responses use **HTTP 402** with body `{code, feature, plan_required}` so the
frontend paywall can name the specific benefit and the plan that unlocks it.

- **`agent_autonomy`** — boolean gate. Core is the source of truth (exposed on the gateway); the
  **behavioral** gate (auto-send vs queue) lives in the Bun agent and fails closed. Core does not try
  to distinguish auto vs manual quote submits — manual quoting stays free.
- **`rfq_monthly_limit`** — a `usage_counters` row keyed `(company_id, 'rfq', period='YYYY-MM')`,
  incremented on RFQ create, checked **before** create. Unlimited when limit is `-1`.
- **`catalog_max_items`** — live `COUNT(catalog_items WHERE company)` checked **before** insert.
- **`seats`** — live `COUNT(users) + COUNT(pending invites)` checked **before** invite; **a pending
  invite holds a seat**. Invite past the cap → 402 paywall.

## Stripe surface (Core)

- **Webhook** `POST /v1/billing/webhook` — **public, signature-verified, idempotent**. Idempotency via
  a `processed_stripe_events` table (Stripe event id UNIQUE; a re-delivered event is a no-op). Handles:
  `checkout.session.completed`, `customer.subscription.created|updated|deleted`,
  `invoice.payment_failed`, `invoice.payment_succeeded`. Each drives the company's `subscriptions` row.
- **Checkout** `POST /v1/billing/checkout {plan, interval}` (authed; owner/admin) → creates/looks up the
  company's Stripe customer, returns a Checkout Session URL with `automatic_tax` enabled (real Tax needs
  the live account; the flag builds now). Success/cancel URLs return to `/settings/billing`.
- **Portal** `POST /v1/billing/portal` → Stripe Customer Portal deep-link (manage payment method /
  cancel / download invoices). **No custom billing UI** for these.
- **Dunning / grace / downgrade:** entitlements are honored while subscription status ∈
  `{trialing, active, past_due}`; `canceled`/`unpaid`/none → **downgrade to Free** (fail-closed).
  Stripe drives retries; `invoice.payment_failed` → `past_due`; final failure →
  `customer.subscription.deleted`/`status=canceled` → Free.
- **`GET /v1/billing/summary`** (authed) → `{plan, status, current_period_end, cancel_at_period_end,
  entitlements:{…}, usage:{rfq_used, rfq_limit, catalog_used, catalog_limit, seats_used, seats_limit}}`
  — the single read the buyer app uses for the billing screen, paywall context, and usage meters.

## Platform-admin control of limits

- `GET /admin/v1/plans` and `PATCH /admin/v1/plans/:code` — guarded by the existing platform-admin JWT
  (`ScopePlatformAdmin`, same guard as `/admin/v1/me`). `PATCH` updates any of `agent_autonomy`,
  `catalog_max_items`, `rfq_monthly_limit`, `seats` for a plan (`free|pro|business`).
- The entitlements resolver reads the `plans` table (with a short in-process cache invalidated on
  `PATCH`), so an admin edit takes effect for all companies without a deploy.

## Agent-autonomy gate (cross-repo)

- **Core (gateway):** `GET /agent/entitlements` on the agent-authenticated gateway — resolves the
  calling agent's company from its credential → returns `{agent_autonomy: bool, plan}`.
- **Bun agent:** at the auto-send decision (`src/reasoning/handler.ts` / `src/policy`), fetch (with a
  short-TTL cache) the company entitlement and **fail closed**: if `agent_autonomy` is false, unset, or
  Core is unreachable → **queue-for-review** instead of auto-send. Free companies may still configure a
  policy; their quotes always queue. Pro/Business auto-send.

## Frontend (buyer app, `core/web`)

- **`/pricing`** — public pricing page + feature-comparison table **mapped 1:1 to the entitlements
  table** above, with a monthly/annual toggle.
- **`<UpgradeModal>`** — contextual paywall; fires on any `402 {code, feature, plan_required}`; names
  the specific benefit and the plan that unlocks it; CTA → Checkout.
- **`/settings/billing`** — current plan + status, portal deep-link (manage/cancel/invoices),
  `<UsageMeter>` bars for RFQs / catalog / seats, near-limit nudge. Reads `GET /v1/billing/summary`.
- **`/settings/team`** — members list + invite (on the existing `internal/identity` invite flow), seat
  usage, invite-past-cap → `<UpgradeModal>`.
- **Checkout flow:** click plan → `POST /v1/billing/checkout` → redirect to Stripe Checkout → return to
  `/settings/billing?checkout=success` → poll `GET /v1/billing/summary` until the webhook has confirmed
  the new plan (optimistic "activating…" state until then).

## Data model — migration `016_billing.up.sql`

- **`plans`** — `code TEXT PRIMARY KEY (free|pro|business)`, `name TEXT`, `agent_autonomy BOOL`,
  `catalog_max_items INT`, `rfq_monthly_limit INT`, `seats INT`, `updated_at TIMESTAMPTZ`. **Seeded**
  with the defaults above.
- **`subscriptions`** — `id`, `company_id` (UNIQUE — one active plan per company), `stripe_customer_id`,
  `stripe_subscription_id`, `plan TEXT REFERENCES plans(code)`, `interval TEXT (month|year)`,
  `status TEXT (trialing|active|past_due|canceled|unpaid|incomplete)`, `current_period_end TIMESTAMPTZ`,
  `cancel_at_period_end BOOL`, `created_at`, `updated_at`.
- **`usage_counters`** — `company_id`, `metric TEXT (rfq)`, `period TEXT (YYYY-MM)`, `count INT`,
  UNIQUE `(company_id, metric, period)`.
- **`processed_stripe_events`** — `event_id TEXT PRIMARY KEY`, `type TEXT`, `processed_at TIMESTAMPTZ`.
- **`companies.stripe_customer_id`** — `ALTER TABLE companies ADD COLUMN stripe_customer_id TEXT`.

## Config additions (`internal/config`)

`STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_PRICE_PRO_MONTH`, `STRIPE_PRICE_PRO_YEAR`,
`STRIPE_PRICE_BUSINESS_MONTH`, `STRIPE_PRICE_BUSINESS_YEAR`. All empty by default → `StubBilling` +
checkout disabled with a clear 503 until configured. No secrets committed.

## Plan decomposition (one spec → 4 plans; subagent-driven TDD; merged locally)

1. **Entitlements spine (Core)** — migration `016` (plans seeded, subscriptions default-Free,
   usage_counters, processed_stripe_events, companies.stripe_customer_id); `internal/entitlements`
   (resolver + `RequireEntitlement` + usage metering, reading the `plans` table with cache); gates
   wired onto RFQ create / catalog insert / invite; platform-admin `GET/PATCH /admin/v1/plans`;
   `GET /v1/billing/summary`. Everyone resolves **Free**. Independently testable and useful.
2. **Stripe billing (Core)** — `internal/billing` (`Billing` interface, `StripeBilling`, `StubBilling`),
   config, `POST /v1/billing/checkout`, `POST /v1/billing/portal`, `POST /v1/billing/webhook`
   (idempotent, signed), subscription→plan resolution, dunning/grace/downgrade.
3. **Agent-autonomy gate (cross-repo)** — Core gateway `GET /agent/entitlements`; Bun agent
   reads/caches/fail-closed at the auto-send decision. **E2E #2** (Free hits the autonomy gate →
   upgrades via Checkout (stub webhook) → entitlement unlocks) lands here.
4. **Frontend (buyer app)** — `/pricing`, `<UpgradeModal>`, `/settings/billing` (+ `<UsageMeter>`),
   `/settings/team`, the Checkout redirect + summary-poll flow.

## Definition of Done (from the backlog)

> A Free user upgrades to Pro with a card, and the gated feature (agent autonomy) unlocks via webhook.
> Limits are enforced server-side (a Free user cannot exceed the RFQ cap or turn on autonomy).
> Downgrade, cancel, invoices, and dunning all work through Stripe. Tax is applied. Inviting a member
> past the seat cap is blocked server-side and surfaces the paywall.

**This-session DoD (stub-mode):** every path above is exercised via `StubBilling` + integration tests
(including the E2E #2 webhook round-trip driven through the stub); live-Stripe verification (real
Checkout, real Tax, real dunning retries) is an **activation follow-up** documented in the Plan 2 doc.

## Out of scope / deferred

- **Seller-app billing UI** (needs Agent-backend proxy) — deferred, consistent with Sprint 1.
- **Live Stripe verification** (real account, Tax, dunning retries) — activation follow-up.
- **Transaction fees / Stripe Connect** — post-launch (roadmap), not this sprint.
- **Usage-based billing / metered Stripe prices** — not now; limits are enforced in-app, plans are flat.

## References

Backlog `sprint-02-billing-entitlements.md`; journey freemium touchpoints (Acts 2–3); wireframes A2,
A4, A5 (team & seats), C6 (autonomy gate). Patterns to mirror: `internal/notify` (interface + stub),
`internal/admin` (platform-admin auth), `internal/identity` (invite flow).

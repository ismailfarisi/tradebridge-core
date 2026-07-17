# Sprint 7 — Onboarding, Activation & UX Polish — Design Spec

**Date:** 2026-07-17 · **Sprint:** 7 of 10 · **Repos:** `tradebridge-core` (+ `web/`), `tradebridge-agent` (light)
**Depends on:** Sprint 6 · **Status:** design approved (2026-07-17)

## Goal

Turn signups into active, paying users. Finish guided onboarding for both sides, add activation scaffolding
(checklist, empty states, prefilled sample templates), wire freemium upgrade nudges to real entitlement
limits, instrument activation events (the data Sprint 8's funnels consume), and do a targeted UX/a11y polish
pass on the core flows so the product feels like one product.

## Locked decisions (user, 2026-07-17)

1. **Polish = targeted pass** — onboarding/activation/conversion get full attention; polish covers empty
   states everywhere + a11y/responsive on the CORE flows only (signup → first-quote / first-RFQ). A full
   cross-app design-system reconciliation (two separate Next apps) is **deferred**.
2. **Activation state = dedicated Core endpoint** — `GET /v1/activation` computes each step server-side from
   real data; one source of truth for checklist + nudges + E2E.
3. **Build the activation-events spine now** — `activation_events` table + best-effort emit at real
   milestones; Sprint 8 builds funnels/dashboards on top.
4. **Rich empty states + prefilled templates** — every empty surface has a purposeful state + next-best
   action; "sample data" = a prefilled example RFQ/catalog TEMPLATE the user submits or dismisses, **never**
   rows silently seeded into the tenant (no analytics pollution, no teardown).

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never pushed** (both repos). `.env`/secrets never committed.
- DB/integration tests use `tradebridge_core_test`, **NEVER** live `tradebridge_core`.
- Activation emit is **fire-and-forget** — a failed emit must never break the request it rides on (mirrors
  the notify engine's best-effort pattern).
- New migration number is **027** (test DB currently at v26; live at v24).
- Follow existing patterns: sqlc queries in `pkg/db/query/*.sql`, handlers `NewHandler(...).Register(...)`,
  `middleware.CompanyID/UserID/Role`, web `req<T>`/`CORE_URL` clients + vitest, existing CSS utilities
  (`.card .stat .badge .btn .muted .small .grid .col .row .field .input .tbl .empty?`), `UpgradeModal`.
- Web verification: `npm test`, `npx tsc --noEmit`, bare `npm run build` (NEVER piped).

## Architecture

Web-heavy sprint on a thin new Core spine, across both apps (`core/web` = buyer+seller marketplace,
`agent/web` = the seller's agent console).

1. **Core activation spine** (`internal/activation`, new) — `GET /v1/activation` + `activation_events`
   table + `Emit` helper wired into existing milestone handlers.
2. **Shared web primitives** (`core/web/components`) — `<EmptyState>`, `<ActivationChecklist>`,
   `<UpgradeNudge>`, `lib/activation.ts`.
3. **Guided onboarding** — seller + buyer flows on the checklist spine; the agent-console conversational
   onboarding (already built) is *linked* from the checklist (connected, not merged).
4. **Targeted polish** — empty states + a11y/responsive on the core flows only.

## Core activation spine (`internal/activation`)

- **`GET /v1/activation`** (tenant-guarded) → `{role, percent_complete, steps:[{key,label,done,cta_href,hint}]}`.
  - Role from `companies.type`: `seller`/`both` → seller steps; `buyer` → buyer steps.
  - **Seller steps (ordered):** `profile` (supplier_profile has non-empty description AND ≥1 category) →
    `catalog` (≥1 `catalog_summaries` row) → `verified` (`companies.verification_status = 'verified'`) →
    `agent_connected` (an active, non-expired `agent_credentials` row with `last_seen` set) → `first_quote`
    (≥1 quote where `seller_company_id = me`). All done ⇒ go-live.
  - **Buyer steps:** `first_rfq` (≥1 RFQ) → `received_quote` (≥1 quote on this buyer's RFQs) → `first_order`
    (≥1 order as buyer).
  - `percent_complete = round(done / total * 100)`. Each step carries a `cta_href` (e.g.
    `/seller/profile`, `/seller/catalog`, `/settings/verification`, `/seller/agent`, `/chat`) + a short hint.
- **`activation_events`** table: `id, company_id, user_id (nullable), event TEXT, props JSONB, created_at`,
  **UNIQUE(company_id, event)** so each milestone is recorded once (clean funnel semantics). Index on
  `(event, created_at)` for S8 funnels.
- **`activation.Emit(ctx, companyID, userID, event, props)`** — `INSERT ... ON CONFLICT (company_id, event)
  DO NOTHING`; errors are logged and swallowed (best-effort). Milestone events + their emit sites:
  | event | emitted where |
  |---|---|
  | `signup` | `identity.register` (after company/user created) |
  | `profile_set` | `suppliers.upsertMine` |
  | `catalog_added` | `catalog.create` |
  | `verified` | verification success (DNS-TXT check + admin override) |
  | `agent_connected` | `gateway.heartbeat` (first touch) |
  | `first_rfq` | `rfq.create` |
  | `first_quote` | quote submit path (`quotes` service Record / gateway `quote.submit`) |
  | `first_order` | order creation (quote accept) |
  | `upgrade` | billing plan activation (subscription created/updated to a paid plan) |

## Shared web components (`core/web`)

- **`lib/activation.ts`** — `getActivation(token) → {role, percent_complete, steps[]}` (typed).
- **`<EmptyState title body cta?>`** — reusable purposeful empty state; used on every empty surface.
- **`<ActivationChecklist>`** — fetches `/v1/activation`; renders ordered steps (done ✓ / todo with CTA),
  a progress bar (`percent_complete`), auto-hides at 100%. Used on `/dashboard`.
- **`<UpgradeNudge feature usage limit>`** — a near-limit banner (shows when `used/limit ≥ 0.8` and limit
  ≥ 0) + a locked-feature variant whose CTA opens the existing `UpgradeModal` / routes to
  `/settings/billing`. Reuses `UpgradeModal`'s `FEATURE_COPY`.

## Onboarding flows

- **Seller:** `<ActivationChecklist>` is the spine on `/dashboard`; first-run empty states on `/seller/*`
  (profile, catalog, agent); the `agent_connected`/"train your agent" step links to the agent-console
  conversational onboarding (agent/web `/onboarding`, already built — light polish + a working deep link,
  no merge).
- **Buyer:** first-RFQ guidance in `/chat` (primer + a **prefilled sample-RFQ template** the user can submit
  or dismiss — nothing seeded); the buyer checklist on `/dashboard`; empty states on `/rfqs`, `/orders`.
- **Nudges:** near-limit banners on catalog/seats/RFQ usage; locked-feature CTAs (agent autonomy, assist) →
  paywall.

## Targeted polish (core flows only)

Empty states with no dead-ends across both apps; a focus/keyboard/contrast/semantics pass + responsive pass
on the signup → first-live-quote (seller) and signup → first-posted-RFQ (buyer) paths. Buttons/links get
visible focus states; forms get labels/aria; the core pages reflow on mobile widths. No new CSS framework;
extend the existing utility classes/tokens.

## Testing / E2E

- **Unit (Go):** each seller/buyer step's done/not-done logic; `Emit` idempotency (second identical emit is a
  no-op via ON CONFLICT); `percent_complete` math; role selection (`both` → seller steps).
- **Web (vitest):** `lib/activation.ts` client; `<ActivationChecklist>` rendering (todo vs done vs 100%
  hidden); `<UpgradeNudge>` threshold logic; `<EmptyState>` CTA.
- **E2E #7** (`test/integration/e2e_07_activation_test.go`): fresh seller signup → `GET /v1/activation`
  shows steps todo → set profile → add catalog → verify (seed) → agent heartbeat → submit quote via gateway
  → `GET /v1/activation` = 100% complete AND the milestone rows exist in `activation_events` — "first live
  quote, unaided."

## Plan breakdown (5 plans; subagent-driven; merged locally only)

1. **Core activation spine** — migration 027 (`activation_events`), `internal/activation` (status query +
   service + `GET /v1/activation`), `Emit` helper + milestone wiring into the listed handlers, unit tests.
2. **Shared web primitives** — `lib/activation.ts`, `<EmptyState>`, `<ActivationChecklist>`,
   `<UpgradeNudge>` + vitest.
3. **Seller onboarding + activation** — checklist on `/dashboard`, seller empty states, agent-console deep
   link, upgrade nudges near catalog/seats/RFQ/autonomy limits.
4. **Buyer onboarding + activation** — first-RFQ guidance + prefilled sample-RFQ template + "what you buy"
   primer, buyer checklist, buyer empty states + assist upgrade nudge.
5. **Targeted a11y/responsive polish on the core flows + E2E #7.**

## Scope / deferred (documented, not built)

Full cross-app design-system unification; persisted seed data; the S8 analytics funnels/dashboards
themselves (this sprint only *emits* events); native mobile app (responsive web only); deep agent-console
onboarding rework (light polish + link only); backfilling historical activation events. Neither repo pushed
to GitHub.

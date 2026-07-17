# Sprint 4 — Ratings, Disputes, Order Lifecycle & Invoicing — Design

**Date:** 2026-07-17
**Sprint:** 4 of 10 · Tracks: Core · Frontend
**Depends on:** Sprints 1 (notify), 2 (entitlements), 3 (trust/storefront)
**Repos:** `tradebridge-core`, `tradebridge-core/web`

## Goal

Close the transaction loop. Orders walk a real lifecycle, accepting a quote emits an
emailed PDF invoice, both sides build reputation, and either party can escalate a dispute
to a working admin console.

## Locked decisions (user, 2026-07-17)

- **PDF engine:** pure-Go library (`go-pdf/fpdf`). No host binaries, deterministic,
  unit-testable — fits the no-sudo `systemd-run --user` deploy.
- **Order authorization:** seller-driven fulfillment, buyer confirms completion.
- **Review visibility:** immediate on submit (no double-blind reveal).
- **Scope:** everything now, **including the full dispute-resolution admin UI** — the first
  admin web surface in the app, the seed the S8 console grows from.

## Standing rules (carried forward)

- All work merged to `master` **locally only, never pushed** (pushing needs explicit consent).
- DB/integration tests use `tradebridge_core_test` ONLY, never live `tradebridge_core`.
- `BILLING_DEV_MODE=true` never set on any internet-reachable env.
- `.env` is gitignored; no secrets committed.

## Current state (what exists)

- `orders` table (007): `status` defaults `'created'`, no CHECK, no transitions. Created
  atomically in `internal/quotes/service.go` `Accept()`, which already notifies both parties
  via the S1 `notify` engine (`TypeOrderCreated`).
- `internal/orders/handler.go`: only `GET /v1/orders/:id` (both-party guard).
- `notify` engine: typed events + email/in-app channels; `NotifyCompany`, `SendTransactional`.
- `audit_logs` table (006) + `CreateAuditLog` query (company_id NOT NULL, actor, action,
  target_id, detail JSONB).
- `/admin/v1` realm: separate admin token (`admin/token.go`), `PlatformAdminGuard`; S3 added
  admin queues (verifications, reports). **No admin web frontend yet.**
- Storefront `/s/[id]` + `internal/suppliers/public.go` carry a `reputation: null` slot.
- RFQs have **no structured line items** — quote is a lump-sum `price_cents`; invoice line
  items derive from RFQ title × `requirements.quantity`.
- No PDF library in `go.mod`.

## Architecture

Four interlocking subsystems, all hanging off the order. Each new domain gets its own
`internal/` package with a service (business logic + state) separated from its HTTP handler,
following the S3 `internal/trust` pattern. Migrations 019–022, one per subsystem.

### 1. Order lifecycle (migration 019)

State machine on `orders.status`:

```
created ──confirm(seller)──> confirmed ──fulfill(seller)──> fulfilled ──complete(buyer)──> completed
   │                             │
   └──────── cancel ─────────────┘   (buyer or seller, only before `fulfilled`)
   any active ──dispute(either)──> disputed ──(admin resolve)──> completed | cancelled
```

- **`order_events`** table: `id, order_id, from_status, to_status, actor (buyer|seller|admin),
  actor_company_id (nullable for admin), note, created_at`. Timeline source of truth + audit.
- **Transitions map + authz** in `internal/orders/service.go`. `CanTransition(from, to)` +
  `ActorFor(to)` (which role may drive each transition). Cancel allowed only from
  `created|confirmed`. Dispute allowed from `created|confirmed|fulfilled`.
- **Endpoints:**
  - `POST /v1/orders/:id/transitions` `{to, note?}` — validates machine + caller role
    (`buyer`/`seller` derived from order party); 409 on illegal transition, 403 on wrong role.
  - `GET /v1/orders` — list orders for caller's company (as buyer or seller), newest first.
  - `GET /v1/orders/:id` — order + `events` timeline (extends existing handler).
- **Notifications:** every transition notifies the counterparty. New typed events:
  `order.confirmed`, `order.fulfilled`, `order.completed`, `order.cancelled`, `order.disputed`.

### 2. Invoicing (migration 020)

- **`invoices`** table: `id, order_id UNIQUE, invoice_number TEXT UNIQUE, buyer_company_id,
  seller_company_id, currency, subtotal_cents, total_cents, issued_at`. `invoice_number` drawn
  from a Postgres **sequence** → formatted `INV-<YYYY>-<6-digit>` (year captured at issue).
- **`invoice_line_items`** table: `id, invoice_id, description, quantity, unit_price_cents,
  amount_cents`. One row generated now (description = RFQ title; quantity =
  `requirements.quantity` or 1; unit = total/qty); the table keeps the invoice extensible.
- **Generated at quote acceptance** — hook into the existing `Accept()` transaction so the
  invoice row + line item are written atomically with the order. This is the DoD trigger
  ("accepting a quote produces a downloadable + emailed PDF invoice").
- `internal/invoicing/` package: `Service` (create invoice inside a tx; fetch invoice+lines)
  + `Renderer` (pure `go-pdf/fpdf`, `Render(invoice, lines, seller, buyer) []byte`).
- **PDF endpoint:** `GET /v1/orders/:id/invoice.pdf` — both-party guard, streams
  `application/pdf` with a filename header.
- **Email:** at issue time, `notify.SendTransactional` to the buyer with the invoice summary
  and a link (PDF attach optional — start with a link to the invoice endpoint to avoid
  large-attachment complexity; note as a follow-up if we want the PDF attached).
- New typed event `invoice.issued`.

### 3. Ratings & reviews (migration 021)

- **`reviews`** table: `id, order_id, reviewer_company_id, reviewee_company_id, direction
  (buyer_on_seller|seller_on_buyer CHECK), rating INT CHECK 1..5, body TEXT, status
  (visible|removed, default visible), created_at`, `UNIQUE(order_id, reviewer_company_id)`.
- **Allowed only after** the order is `completed`, and only by a party to that order; direction
  derived from which party the reviewer is.
- **Visible immediately.** Aggregate `AVG(rating)::numeric` + `COUNT` computed in-query
  (`GetCompanyReputation`), no denormalized column.
- **Wiring:**
  - `internal/reviews/` package: create, list-for-company (public, visible only), reputation.
  - Public storefront: fill the `reputation` slot in `internal/suppliers/public.go` with
    `{avg_rating, review_count}` and expose the visible reviews list.
  - Quote-compare: add seller `avg_rating`/`review_count` to `RankQuotesByRFQ` (badge data).
  - Endpoints: `POST /v1/orders/:id/reviews`, `GET /v1/orders/:id/reviews`,
    `GET /public/v1/sellers/:id/reviews` (unauthenticated, visible only).
- **Moderation:** a review can be reported (reuses S3's `POST /v1/reports`, target = reviewee
  company, with the review id in detail); admin flips `status='removed'` via
  `POST /admin/v1/reviews/:id/remove`. Removed reviews drop out of aggregate + lists.
- New typed event `review.received` (notify the reviewee).

### 4. Disputes + admin console (migration 022)

- **`disputes`** table: `id, order_id, raised_by_company_id, reason (CHECK: not_delivered |
  not_as_described | payment | other), detail TEXT, status (open|resolved|rejected, default
  open), resolution TEXT, resolved_by_admin_id (nullable), created_at, resolved_at (nullable)`.
  Partial-unique: **one open dispute per order** (`WHERE status='open'`).
- **Raising** (`POST /v1/orders/:id/disputes`, party-only, order in an active state) inserts the
  dispute and transitions `orders.status='disputed'` (records an `order_events` row).
- **Resolution** (admin): `GET /admin/v1/disputes` (queue, filter by status) +
  `POST /admin/v1/disputes/:id/resolve` `{outcome: resolved|rejected, order_to:
  completed|cancelled, resolution}`. Writes the dispute resolution, moves the order to
  the chosen state (order_events actor=`admin`), and logs to **`audit_logs`**
  (`CreateAuditLog`, company_id = order buyer, actor = admin id, action = `dispute.resolved`,
  target_id = dispute id, detail = outcome + order transition).
- `internal/disputes/` package (service + party handler + admin handler).
- New typed events `dispute.opened` (notify counterparty), `dispute.resolved` (notify both).

### 5. Frontend (core/web)

- **`web/lib/orders.ts`** — typed client: listOrders, getOrder (+events), transitionOrder,
  getInvoiceUrl, createReview, listReviews, createDispute. **`web/lib/admin.ts`** — adminLogin,
  listDisputes, resolveDispute (separate admin token in its own storage key).
- **Order detail** `/orders/[id]`: status header, **timeline** from `order_events`, role-aware
  action buttons (seller: Confirm/Mark fulfilled; buyer: Mark completed; either:
  Cancel/Dispute per rules), **Download invoice (PDF)**, post-completion **Leave a review**,
  **Open a dispute** modal.
- **Orders list** on buyer + seller dashboards (status pill + link).
- **Reviews** on the public storefront `/s/[id]` (rating + list, fills the reputation slot) and
  a rating badge on the quote-compare table.
- **Admin console (new surface):** `/admin/login` (admin token) + `/admin/disputes` queue with
  resolve action. Deliberately minimal — CSS/components reused from the app shell; this is the
  seed S8 expands. Lives outside the `(app)` buyer/seller guard, under its own admin guard.
- Components: `<OrderTimeline>`, `<OrderActions>`, `<ReviewForm>`, `<ReviewList>`,
  `<StarRating>`, `<DisputeModal>`.

## Testing strategy

- **Go:** transition/authz table tests (legal + illegal + wrong-role); invoice creation inside
  Accept tx; PDF renderer content assertions (non-empty, contains invoice number/parties/total);
  review create rules (only-after-completed, one-per-party, aggregate excludes removed); dispute
  raise → order disputed → admin resolve → order terminal + audit row. DB tests → `tradebridge_core_test`.
- **Web:** vitest on `lib/orders.ts` + `lib/admin.ts` (mock `fetch`); pages via `tsc --noEmit`
  + `next build`. No React Testing Library.
- **E2E #4** (`internal/*/e2e_test.go`): accept quote → invoice row + PDF renders + email queued
  → order walks `confirmed → fulfilled → completed` → buyer review → seller reputation reflects it.

## Config

- No new required config. `go-pdf/fpdf` added to `go.mod` (vendored/module download at build).
- Invoice email reuses existing `notify`/Resend config (`APP_BASE_URL` for links).

## Plan decomposition (5 plans)

1. **Order lifecycle** — migration 019 (`orders.status` CHECK, `order_events`); state machine +
   authz; transition/list endpoints; timeline; transition notifications.
2. **Invoicing** — migration 020 (`invoices`, `invoice_line_items`, invoice-number sequence);
   `internal/invoicing` service + `go-pdf/fpdf` renderer; generate inside `Accept()`; PDF
   endpoint; issue email.
3. **Reviews** — migration 021 (`reviews`); `internal/reviews`; create/list/reputation; wire
   storefront reputation slot + quote-compare badge; report + admin remove.
4. **Disputes + admin API** — migration 022 (`disputes`); `internal/disputes`; raise + order
   disputed; admin queue + resolve + audit log.
5. **Frontend** — `lib/orders.ts` + `lib/admin.ts`; order detail + timeline + actions; invoice
   download; review UI on storefront/quotes; dispute modal; admin dispute console + login.

## Deferred (documented)

- PDF-as-email-attachment (start with link); ERP/batch invoicing (Business plan, later);
  double-blind reviews; review edit/delete; multi-currency invoice conversion; dispute
  messaging thread; the polished S8 admin console (this sprint ships the minimal seed).

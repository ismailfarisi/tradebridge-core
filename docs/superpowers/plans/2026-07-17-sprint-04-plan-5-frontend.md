# Sprint 4 · Plan 5 — Frontend Implementation Plan

> **For agentic workers:** TDD where it fits (vitest on `lib/*` with mocked `fetch`); pages verified via
> `tsc --noEmit` + `next build` (NO React Testing Library). Follow existing patterns: `lib/trust.ts`
> (client), `lib/admin.ts` (admin token), `components/AppShell.tsx`, `components/AdminShell.tsx`,
> `app/s/[id]/page.tsx`. Commit per task.

**Goal:** Surface the Sprint-4 backend in `core/web`: an order detail page with lifecycle timeline +
role-aware actions + PDF invoice download + review + dispute affordances, an orders list, seller ratings
on the storefront and quote-compare, and an admin dispute-resolution console.

**Architecture:** New `lib/orders.ts` typed client; dispute calls added to `lib/admin.ts`; new pages
`app/(app)/orders/page.tsx` (list) and `app/(app)/orders/[id]/page.tsx` (detail) inside the app guard;
new `app/admin/disputes/page.tsx` under `AdminShell`; small presentational components. Existing
`QuoteRow`/`PublicSeller` types extended for ratings.

**Tech Stack:** Next.js 16 / React 19 / TS strict, vitest.

## Global Constraints

- Merge to `master` LOCALLY ONLY, never push.
- Run web commands from `web/`. NEVER pipe `npm run build` through `head` (SIGPIPE corrupts `.next`).
- Verify each page task: `npx tsc --noEmit` then `npm run build` (full, unpiped) — both clean.
- Money is integer cents from the API; format `${currency} ${(cents/100).toFixed(2)}`.
- Reuse CSS utility classes: `.card .col .field .input .btn .btn-primary .btn-sm .btn-ghost .badge
  .badge-success .badge-warning .tbl .row .muted .ok .err`. Do not invent a new design system.
- The order detail page derives the caller's role by comparing `useMe().me.company_id` to the order's
  `buyer_company_id`/`seller_company_id`. Never trust a role from the URL.

## Backend contracts (already shipped, Plans 1–4)

- `GET /v1/orders` → `{orders: Order[]}`
- `GET /v1/orders/:id` → `{order: Order, events: OrderEvent[]}`
- `POST /v1/orders/:id/transitions` body `{to, note?}` → `{order: Order}` (409 illegal, 403 wrong role)
- `GET /v1/orders/:id/invoice.pdf` → PDF bytes (`application/pdf`), auth via Bearer token
- `POST /v1/orders/:id/reviews` body `{rating, body}` → `{review: Review}` (409 not completed / dup, 400 bounds)
- `GET /v1/orders/:id/reviews` → `{reviews: Review[]}`
- `GET /public/v1/sellers/:id/reviews` → `{reviews: PublicReview[]}` (unauth)
- `POST /v1/orders/:id/disputes` body `{reason, detail}` → `{dispute: Dispute}` (409 not active / dup)
- `GET /v1/orders/:id/disputes` → `{disputes: Dispute[]}`
- `GET /admin/v1/disputes?status=` → `{disputes: Dispute[]}`
- `POST /admin/v1/disputes/:id/resolve` body `{outcome, order_to, resolution}` → `{dispute: Dispute}`

Order JSON: `{id, rfq_id, quote_id, buyer_company_id, seller_company_id, total_cents, currency, status, created_at}`.
OrderEvent: `{id, order_id, from_status, to_status, actor, actor_company_id, note, created_at}`.
Review: `{id, order_id, reviewer_company_id, reviewee_company_id, direction, rating, body, status, created_at}`.
Dispute: `{id, order_id, raised_by_company_id, reason, detail, status, resolution, resolved_by_admin_id, created_at, resolved_at}`.

---

### Task 1: `lib/orders.ts` client + tests

**Files:** Create `web/lib/orders.ts`, `web/lib/orders.test.ts`.

Mirror `lib/trust.ts`: a local `req<T>` helper (copy the one from `trust.ts`/`api.ts` verbatim —
token+body+no-store, text-on-error) and typed functions + types.

```ts
export type OrderStatus = "created" | "confirmed" | "fulfilled" | "completed" | "cancelled" | "disputed";
export type Order = {
  id: string; rfq_id: string; quote_id: string;
  buyer_company_id: string; seller_company_id: string;
  total_cents: number; currency: string; status: OrderStatus; created_at: string;
};
export type OrderEvent = {
  id: string; order_id: string; from_status: string; to_status: string;
  actor: string; actor_company_id: string | null; note: string; created_at: string;
};
export type Review = {
  id: string; order_id: string; reviewer_company_id: string; reviewee_company_id: string;
  direction: string; rating: number; body: string; status: string; created_at: string;
};
export type Dispute = {
  id: string; order_id: string; raised_by_company_id: string; reason: string; detail: string;
  status: string; resolution: string; resolved_by_admin_id: string | null;
  created_at: string; resolved_at: string | null;
};

export const listOrders = (token: string) => req<{ orders: Order[] }>("GET", "/v1/orders", { token });
export const getOrder = (token: string, id: string) =>
  req<{ order: Order; events: OrderEvent[] }>("GET", `/v1/orders/${id}`, { token });
export const transitionOrder = (token: string, id: string, to: OrderStatus, note = "") =>
  req<{ order: Order }>("POST", `/v1/orders/${id}/transitions`, { token, body: { to, note } });
export const listOrderReviews = (token: string, id: string) =>
  req<{ reviews: Review[] }>("GET", `/v1/orders/${id}/reviews`, { token });
export const createOrderReview = (token: string, id: string, rating: number, body: string) =>
  req<{ review: Review }>("POST", `/v1/orders/${id}/reviews`, { token, body: { rating, body } });
export const listOrderDisputes = (token: string, id: string) =>
  req<{ disputes: Dispute[] }>("GET", `/v1/orders/${id}/disputes`, { token });
export const createOrderDispute = (token: string, id: string, reason: string, detail: string) =>
  req<{ dispute: Dispute }>("POST", `/v1/orders/${id}/disputes`, { token, body: { reason, detail } });
// Invoice download: fetch as a blob with auth, since it's not JSON.
export async function downloadInvoice(token: string, id: string): Promise<Blob> {
  const res = await fetch(`${CORE_URL}/v1/orders/${id}/invoice.pdf`, {
    headers: { authorization: `Bearer ${token}` }, cache: "no-store",
  });
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
  return res.blob();
}
```
(Import `CORE_URL` from `./api`.)

- [ ] **Step 1: Write failing vitest** `lib/orders.test.ts` (mirror `lib/trust.test.ts`: mock `global.fetch`).
  Assert: `transitionOrder` POSTs to `/v1/orders/o1/transitions` with body `{to:"confirmed",note:""}` and
  Bearer header; `createOrderReview` posts `{rating:5,body:"good"}`; a non-ok response throws the body text;
  `listOrders` returns the parsed `orders`.
- [ ] **Step 2: Run** `npx vitest run lib/orders.test.ts` → fail.
- [ ] **Step 3: Implement** `lib/orders.ts`.
- [ ] **Step 4: Run** → pass. `npx tsc --noEmit` clean.
- [ ] **Step 5: Commit** — `feat(web): orders/reviews/disputes API client`.

---

### Task 2: Extend shared types + `lib/admin.ts` dispute calls + tests

**Files:** `web/lib/api.ts` (QuoteRow), `web/lib/trust.ts` (PublicSeller reputation + reviews),
`web/lib/admin.ts` (dispute calls), `web/lib/admin.test.ts`.

- **QuoteRow** (`lib/api.ts`): add
  `seller_avg_rating?: number;` and `seller_review_count?: number;`.
- **PublicSeller** (`lib/trust.ts`): change `reputation: null;` to
  `reputation: { avg_rating: number; review_count: number } | null;` and add:
```ts
export type PublicReview = { id: string; rating: number; body: string; reviewer_name: string; created_at: string };
export const getPublicSellerReviews = (companyId: string) =>
  call<{ reviews: PublicReview[] }>("GET", `/public/v1/sellers/${companyId}/reviews`);
```
  (use the existing `call<T>` in trust.ts; no token).
- **lib/admin.ts**: add the `Dispute` type (or import from `lib/orders`) and:
```ts
export const listDisputes = (token: string, status = "") =>
  req<{ disputes: Dispute[] }>("GET", `/admin/v1/disputes${status ? `?status=${status}` : ""}`, { token });
export const resolveDispute = (token: string, id: string, outcome: string, orderTo: string, resolution: string) =>
  req<{ dispute: Dispute }>("POST", `/admin/v1/disputes/${id}/resolve`, { token, body: { outcome, order_to: orderTo, resolution } });
```
  (import `type Dispute` from `./orders` to avoid duplication).

- [ ] **Step 1: Write failing vitest** `lib/admin.test.ts` (create if absent; mirror trust.test): assert
  `listDisputes(token,"open")` GETs `/admin/v1/disputes?status=open` with Bearer; `resolveDispute` POSTs
  `{outcome, order_to, resolution}` to `/admin/v1/disputes/d1/resolve`.
- [ ] **Step 2: Run → fail. Step 3: Implement. Step 4: Run → pass; `npx tsc --noEmit` clean.**
- [ ] **Step 5: Commit** — `feat(web): rating fields on quotes/storefront + admin dispute API`.

---

### Task 3: Order detail page + components

**Files:** Create `web/app/(app)/orders/[id]/page.tsx`,
`web/components/OrderTimeline.tsx`, `web/components/StarRating.tsx`, `web/components/ReviewForm.tsx`,
`web/components/DisputeModal.tsx`.

**`<StarRating>`** — presentational: props `{ value: number; count?: number; onChange?: (n:number)=>void }`.
Read-only shows `★★★★☆` + `(count)`; when `onChange` given, renders 5 clickable stars for input.

**`<OrderTimeline>`** — props `{ events: OrderEvent[] }`; renders an ordered list of
`from → to` with actor + relative/formatted `created_at` + optional note.

**`<ReviewForm>`** — props `{ onSubmit: (rating:number, body:string)=>Promise<void> }`; a StarRating input
+ textarea + submit; shows `.ok`/`.err` message.

**`<DisputeModal>`** — props `{ onSubmit:(reason,detail)=>Promise<void>; onClose:()=>void }`; a `<select>`
of reasons (`not_delivered|not_as_described|payment|other`) + detail textarea + submit; simple overlay
using `.card`.

**Order detail page** (`"use client"`, `useParams<{id:string}>()`, `useToken`/`useMe`):
- Load `getOrder(token, id)` + `listOrderReviews` + `listOrderDisputes`.
- Header: order id (short), status `.badge`, total, created date. If `status==="disputed"`, a
  `.badge-warning` "Under dispute" banner and NO party action buttons.
- Derive `role`: `me.company_id === order.seller_company_id ? "seller" : "buyer"` (also handle non-party
  → the API already 404s; show "Order not found").
- **Action buttons** (call `transitionOrder`, then reload):
  - seller & `created` → "Confirm order" (`confirmed`)
  - seller & `confirmed` → "Mark fulfilled" (`fulfilled`)
  - buyer & `fulfilled` → "Mark completed" (`completed`)
  - either & status in `{created,confirmed}` → "Cancel order" (`cancelled`)
  - either & status in `{created,confirmed,fulfilled}` & no open dispute → "Open a dispute" (opens `<DisputeModal>`)
- **Invoice:** "Download invoice (PDF)" button → `downloadInvoice(token,id)` then trigger a browser download
  (`URL.createObjectURL(blob)` → anchor click → revoke).
- **Timeline:** `<OrderTimeline events={events} />`.
- **Reviews:** show existing reviews; if `status==="completed"` and I haven't reviewed yet
  (no review with `reviewer_company_id===me.company_id`), show `<ReviewForm>` → `createOrderReview`.
- Show any open/past disputes with their status + resolution.

- [ ] **Step 1: Build the components + page.** Keep API errors visible via `.err`.
- [ ] **Step 2: Verify** `npx tsc --noEmit` clean, then `npm run build` (full, unpiped) — route
  `/orders/[id]` present, exit 0.
- [ ] **Step 3: Commit** — `feat(web): order detail — timeline, lifecycle actions, invoice, review, dispute`.

---

### Task 4: Orders list, nav, quote-compare rating, storefront reputation

**Files:** Create `web/app/(app)/orders/page.tsx`; modify `web/components/AppShell.tsx` (nav),
`web/app/(app)/rfqs/[id]/page.tsx` + `web/app/(chat)/chat/[rfqId]/page.tsx` (rating badge),
`web/app/s/[id]/page.tsx` (reputation + reviews).

- **Orders list** `app/(app)/orders/page.tsx`: `listOrders(token)` in an AppShell page, a `.tbl` of
  order id (link to `/orders/:id`), counterparty (buyer/seller company id short), total, status `.badge`,
  created date. Empty state via `.muted`.
- **Nav:** in `AppShell.tsx` `NAV`, add to the first (unlabeled) group:
  `{ href: "/orders", label: "Orders", show: () => true }`.
- **Quote-compare rating badge:** in BOTH `app/(app)/rfqs/[id]/page.tsx` and
  `app/(chat)/chat/[rfqId]/page.tsx`, next to the `<VerifiedBadge>` beside each seller, render
  `q.seller_review_count ? <StarRating value={q.seller_avg_rating ?? 0} count={q.seller_review_count} /> : <span className="muted">No ratings</span>`.
- **Accepted-order link:** in `rfqs/[id]/page.tsx`, where it shows "purchase order {order_id} created",
  make it a link to `/orders/{order_id}`.
- **Storefront** `app/s/[id]/page.tsx`: when `seller.reputation` is present, show
  `<StarRating value={seller.reputation.avg_rating} count={seller.reputation.review_count} />` under the
  header; below the catalog, load `getPublicSellerReviews(id)` and render a `<ReviewList>` (inline or a
  small component: reviewer_name, stars, body, date).

- [ ] **Step 1: Implement.**
- [ ] **Step 2: Verify** `npx tsc --noEmit` + `npm run build` (unpiped) — `/orders` present, exit 0.
- [ ] **Step 3: Commit** — `feat(web): orders list, nav, seller ratings on quotes + storefront`.

---

### Task 5: Admin dispute console

**Files:** Create `web/app/admin/disputes/page.tsx`; modify `web/components/AdminShell.tsx` (add nav links).

- **AdminShell nav:** add a simple nav row in the header (or under it) with links to `/admin`
  ("Settings") and `/admin/disputes` ("Disputes"). Use `next/link`; keep it minimal.
- **`app/admin/disputes/page.tsx`** (`"use client"`, wrapped in `<AdminShell>`, uses `useAdminToken`):
  - `listDisputes(token, statusFilter)` with a status filter (`open`/all); a `.tbl` of dispute id (short),
    order id (short), reason, status `.badge`, raised date.
  - For an `open` dispute, a "Resolve" control: pick `outcome` (`resolved`/`rejected`), `order_to`
    (`completed`/`cancelled`), a resolution note → `resolveDispute(...)` then reload.
  - Show resolution + resolver for closed ones.

- [ ] **Step 1: Implement.**
- [ ] **Step 2: Verify** `npx tsc --noEmit` + `npm run build` (unpiped) — `/admin/disputes` present, exit 0.
- [ ] **Step 3: Commit** — `feat(web): admin dispute-resolution console`.

---

## Self-Review checklist

- `npx vitest run` — all green (new `lib/orders.test.ts`, `lib/admin.test.ts` + existing).
- `npx tsc --noEmit` clean; `npm run build` (full, unpiped) exit 0 with routes `/orders`, `/orders/[id]`,
  `/admin/disputes` in the route list.
- Role-aware actions are correct (seller confirms/fulfils, buyer completes; disputed hides party actions).
- Invoice downloads with the auth token (not an unauthenticated link).
- Storefront shows the rating; quote-compare shows per-seller ratings; both handle zero-review sellers.
- Admin console lists + resolves disputes using the separate admin token (`useAdminToken`).
- No `NEXT_PUBLIC_*` build-time URL assumptions changed.

# Sprint 5 · Plan 4 — Frontend (Thread + Assistant) Implementation Plan

> **For agentic workers:** TDD lib (vitest, mock fetch); pages via `tsc --noEmit` + `next build` (bare, no
> pipe; NO RTL). Follow `lib/orders.ts`/`lib/trust.ts` (client), `lib/sse.ts` (`readSSE`), existing pages.

**Goal:** Surface Sprint-5 backend in `core/web`: a live quote thread (messages + version history +
counter form + accept-latest) on the RFQ compare page, and a buyer AI assistant ("Improve this RFQ" on the
composer, "Explain these quotes" on the compare view) with metered usage + paywall handling.

**Architecture:** New `lib/thread.ts` + `lib/assist.ts` typed clients; a `<QuoteThread>` component
(consumes the SSE via `readSSE`) embedded per-quote on `rfqs/[id]`; an "Improve" affordance on the chat
`Composer`; an "Explain quotes" panel on the compare view.

**Tech Stack:** Next.js 16 / React 19 / TS strict, vitest.

## Global Constraints

- Merge to `master` LOCALLY ONLY, never push. Run web commands from `web/`.
- NEVER pipe `npm run build` through anything (SIGPIPE corrupts `.next`). Verify: `npx tsc --noEmit` then bare `npm run build`.
- Money is integer cents: `${currency} ${(cents/100).toFixed(2)}`.
- Reuse CSS utility classes (`.card .col .field .input .btn .btn-primary .btn-sm .badge .tbl .row .muted .ok .err`). No new design system.
- SSE is consumed via **fetch + `readSSE`** (Authorization header), NOT EventSource.
- A `402` from an assist endpoint is the paywall — surface it as an upgrade prompt, not a raw error.

## Backend contracts (shipped, Plans 1–3)

- `GET /v1/quotes/:id/thread` → `{quote, messages:[{id,author,body,attachments,created_at}], versions:[{id,version,price_cents,currency,delivery_days,notes,source,created_at}]}`
- `POST /v1/quotes/:id/counter {price_cents?, delivery_days?, message}` → `{message}`
- `POST /v1/quotes/:id/messages {body, attachment_ids?}` → message (existing)
- `GET /v1/quotes/:id/thread/stream` → SSE, `event: thread`, `data: {kind:"message"|"version", quote_id, message?, version?}`
- `POST /v1/quotes/:id/accept` → `{order_id, ...}` (existing; accepts the head/latest version)
- `POST /v1/assist/rfq {draft}` → `{suggestion}` (402 when capped)
- `POST /v1/assist/compare {rfq_id}` → `{explanation}` (402 when capped; buyer-only)

---

### Task 1: `lib/thread.ts` + `lib/assist.ts` + tests

**Files:** `web/lib/thread.ts`, `web/lib/assist.ts`, `web/lib/thread.test.ts`, `web/lib/assist.test.ts`.

`lib/thread.ts` (local `req<T>` copied from `lib/orders.ts`; import `CORE_URL` + `readSSE` from `./sse`):
```ts
export type ThreadMessage = { id: string; author: "buyer"|"seller"; body: string; attachments: unknown; created_at: string };
export type QuoteVersion = { id: string; version: number; price_cents: number; currency: string; delivery_days: number|null; notes: string; source: "seller"|"counter"; created_at: string };
export type Thread = { quote: { id: string; price_cents: number; currency: string; status: string }; messages: ThreadMessage[]; versions: QuoteVersion[] };

export const getThread = (token: string, quoteId: string) => req<Thread>("GET", `/v1/quotes/${quoteId}/thread`, { token });
export const postCounter = (token: string, quoteId: string, body: { price_cents?: number; delivery_days?: number; message: string }) =>
  req<{ message: ThreadMessage }>("POST", `/v1/quotes/${quoteId}/counter`, { token, body });
export const postThreadMessage = (token: string, quoteId: string, body: string) =>
  req<ThreadMessage>("POST", `/v1/quotes/${quoteId}/messages`, { token, body: { body } });

// streamThread opens the SSE and calls onEvent for each {kind,...} envelope.
// Returns an AbortController so the caller can close it on unmount.
export function streamThread(token: string, quoteId: string, onEvent: (env: { kind: string; message?: unknown; version?: unknown }) => void): AbortController {
  const ctrl = new AbortController();
  fetch(`${CORE_URL}/v1/quotes/${quoteId}/thread/stream`, { headers: { authorization: `Bearer ${token}` }, signal: ctrl.signal, cache: "no-store" })
    .then((res) => readSSE(res, (_event, data) => { try { onEvent(JSON.parse(data)); } catch {} }))
    .catch(() => {}); // aborted / network — caller re-fetches on remount
  return ctrl;
}
```

`lib/assist.ts`:
```ts
export class PaywallError extends Error { constructor(public planRequired?: string) { super("assist limit reached"); } }
// req variant that throws PaywallError on 402:
export const assistRFQ = (token: string, draft: string) => callAssist<{ suggestion: string }>(token, "/v1/assist/rfq", { draft });
export const assistCompare = (token: string, rfqId: string) => callAssist<{ explanation: string }>(token, "/v1/assist/compare", { rfq_id: rfqId });
```
(`callAssist` posts with the token; on `res.status === 402` throws `PaywallError`; else the usual text-on-error.)

- [ ] **Step 1: Failing vitest** — mock `global.fetch`: `postCounter` POSTs `/v1/quotes/q1/counter` with the
  body + Bearer; `getThread` returns parsed `{quote,messages,versions}`; `assistRFQ` returns `{suggestion}`;
  a `402` response from `assistRFQ` throws `PaywallError`. (`streamThread` — assert it issues a fetch to the
  stream URL with the Bearer header; you can mock `fetch` to resolve a `Response` with an empty body.)
- [ ] **Step 2–4: fail → implement → pass** (`npx vitest run lib/thread.test.ts lib/assist.test.ts`).
- [ ] **Step 5: Commit** — `feat(web): thread + assist API clients`.

---

### Task 2: `<QuoteThread>` component (live) + counter form

**Files:** `web/components/QuoteThread.tsx`.

Props `{ quoteId: string; token: string; onAccepted?: (orderId: string) => void }`.
- On mount: `getThread` → render messages (author-labeled bubbles) + a **version history** table
  (version, price, delivery, source, date) with the latest highlighted.
- Open `streamThread` on mount; on each envelope, refetch the thread (simplest correct update) or append;
  abort the controller on unmount.
- **Counter form**: price (optional), delivery days (optional), message (required) → `postCounter` → clear
  form (the SSE / refetch shows the new message). Show `.err` on failure.
- **Accept latest**: a button calling `acceptQuote(token, quoteId)` (from `lib/api`) → `onAccepted(order_id)`.
  Label it "Accept latest (v{n})" using the top version.

- [ ] **Step 1:** Build the component.
- [ ] **Step 2:** `npx tsc --noEmit` clean.
- [ ] **Step 3: Commit** — `feat(web): live QuoteThread component (history + counter + accept-latest)`.

---

### Task 3: Wire thread + "Explain quotes" into the compare page

**Files:** `web/app/(app)/rfqs/[id]/page.tsx`, `web/components/AssistExplain.tsx`.

- Under each quote row (or via a "Negotiate" expander), render `<QuoteThread quoteId={q.id} token={token}
  onAccepted={(id) => setAcceptedOrderId(id)} />`. Keep the existing accept button OR replace it with the
  thread's accept-latest — pick one path so accept isn't duplicated confusingly (prefer the thread's, since
  it accepts the latest version).
- Add an **`<AssistExplain rfqId={id} token={token} />`** panel above the quotes: a button "Explain these
  quotes"; on click → `assistCompare(token, id)` → render `{explanation}` in a `.card`; on `PaywallError`
  show an upgrade prompt linking to `/settings/billing`.

- [ ] **Step 1:** Wire both.
- [ ] **Step 2:** `npx tsc --noEmit` + bare `npm run build` — `/rfqs/[id]` still builds, exit 0.
- [ ] **Step 3: Commit** — `feat(web): quote thread + explain-quotes assist on the compare page`.

---

### Task 4: "Improve this RFQ" on the composer

**Files:** the chat `Composer` (`web/app/(chat)/chat/page.tsx` and/or its `Composer` component), `web/components/AssistImprove.tsx`.

- Add an "Improve" button beside the composer's send control. On click → `assistRFQ(token, draft)` →
  set the composer draft to the returned `suggestion` (or show it as a suggestion the user can accept).
  On `PaywallError` → upgrade prompt. Disable while pending.
- Keep it unobtrusive (a small `.btn-sm .btn-ghost`); don't block sending.

- [ ] **Step 1:** Wire it (find where `draft`/`setDraft` live — the Composer receives them).
- [ ] **Step 2:** `npx tsc --noEmit` + bare `npm run build` — exit 0.
- [ ] **Step 3: Commit** — `feat(web): 'Improve this RFQ' assist on the composer`.

---

## Self-Review checklist

- `npx vitest run` all green (new thread/assist tests + existing).
- `npx tsc --noEmit` clean; bare `npm run build` exit 0; `/rfqs/[id]`, `/chat` still present.
- The thread updates live via SSE (fetch+readSSE, Bearer header); the counter form posts and the new
  message/version appears; accept-latest accepts the head version.
- Assist paywall (402) is surfaced as an upgrade prompt, not a raw error.
- No `NEXT_PUBLIC_*` build-time URL assumptions changed.

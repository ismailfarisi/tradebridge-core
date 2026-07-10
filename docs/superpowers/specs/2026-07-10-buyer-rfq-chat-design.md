# Buyer RFQ Chat (ChatGPT-style) — Design

- **Date:** 2026-07-10
- **Status:** Approved (design) — pending implementation plan
- **Repo:** `tradebridge-core` (Go Core API + Next.js buyer web)

## 1. Goal

Reshape the buyer experience into a **ChatGPT-style chat**: a left sidebar of RFQ
conversations, a top app bar with a **Core-reachability status indicator on the
right**, and a centered chat where the buyer describes what they need in plain
language. The assistant **clarifies over a few turns**, then creates the RFQ
(reusing the existing extract → match → quote → order flow); matched sellers'
quotes render **inline in the thread**, and the buyer accepts one to create an
order. Seller/admin pages are unchanged.

## 2. Scope

**In scope:** a new conversational-intake endpoint in Core; a nullable `messages`
column on `rfqs`; a `ChatShell` + `/chat` and `/chat/[rfqId]` routes in the buyer
web; poll-based quote rendering; the app-bar status indicator.

**Out of scope (YAGNI):** token streaming (replies are short, returned whole);
draft/conversation persistence *before* an RFQ is created; a buyer-facing SSE
stream (we poll); any change to the seller/admin UIs or the RFQ/quote/order
backend beyond the new intake endpoint + column.

**Decisions settled in brainstorming:** full buyer chat shell (sidebar = RFQ
threads) · **multi-turn clarify then create** · stateless intake endpoint (only
the finished RFQ persists) · quotes via polling · status = Core `/health`
reachability.

## 3. Backend — conversational intake (Core, `internal/rfq`)

**`POST /v1/rfq-chat`** (buyer, tenant-guarded):
- **Request:** `{ messages: [{ role: "user"|"assistant", content: string }] }` — the
  conversation so far (frontend-held; not persisted until an RFQ is created).
- **One intake step** over the messages via the **settings-driven LLM** (reuse
  `ExtractorResolver`/`ProviderConfig` so keys stay UI-configurable), returning a
  structured decision:
  - `ask` → response `{ done: false, assistant: "<clarifying question>" }`.
  - `create` → Core creates the RFQ through the **existing RFQ service** (extract
    structured `requirements`, `raw_text` = the accumulated conversation, match
    sellers, emit `rfq.matched`, persist the transcript on the RFQ), returning
    `{ done: true, assistant: "<confirmation>", rfq, match_count }`.
- **Heuristic fallback (no LLM key), deterministic:** if the accumulated text has a
  product/category **and** a quantity → `create`; else `ask` the next missing-slot
  question (quantity → delivery location/date). Caps at ~2 questions, then creates
  with whatever is present. Works with **zero keys**, matching the rest of the system.
- **Guardrails:** cap clarification at ~3 turns; treat an explicit "just create it"
  as `create`.
- The decision prompt + parsing live beside the existing extractor
  (`internal/rfq`); the `create` branch calls the existing create/match path — no
  duplication of match/quote logic.

**Migration:** add `messages jsonb NULL` to `rfqs` (additive, reversible). The
transcript is written only on creation; `GET /v1/rfqs/:id` returns it so a thread
can replay the conversation. Everything else in `rfqs`/`quotes`/`orders` is unchanged.

## 4. Frontend — the buyer `ChatShell` (Core web)

**Layout (per the reference screenshot):**
- **Sidebar:** `＋ New RFQ` at top → the buyer's RFQ conversations (each RFQ = one
  thread, titled by title/category, newest first) → a compact bottom user menu
  (company name + Seller/Admin/Settings links *for users who are also those* +
  Sign out). Collapsible.
- **App bar:** current thread title (or "New RFQ") on the left; on the **right**, the
  **status indicator** (green "Connected" / red "Core offline", polled `/health`
  every ~10s) + the company/role badge.
- **Center:** empty state = centered "What do you need to source?" + chat input;
  active thread = message list with inline quote cards, input pinned at the bottom.

**Routes & components:**
- `web/app/(app)/chat/page.tsx` — new-RFQ empty state + input.
- `web/app/(app)/chat/[rfqId]/page.tsx` — a created thread (linkable/refresh-safe).
- New `ChatShell` component (sidebar + app bar + main) for the buyer path; sellers/
  admins keep the existing `AppShell`. Buyers default-land on `/chat` (adjust the
  post-login redirect + the buyer nav).

**Data flow (one thread):**
1. **New RFQ:** type → append user bubble → `POST /v1/rfq-chat {messages}`.
   `done:false` → append the assistant question → answer → repeat.
   `done:true` → route to `/chat/<rfq.id>` and show the confirmation.
2. **Created thread** (`/chat/[rfqId]`): load `getRfq` (+ persisted `messages`) +
   `rfqQuotes`; **poll `rfqQuotes` every ~4s** while the RFQ isn't closed; each quote
   renders as an inline card (seller · price · delivery · notes · rank) with **Accept**.
3. **Accept** → `acceptQuote` → thread shows "✅ Order created — PO #… for $X";
   polling stops.

**Client additions** (`web/lib/api.ts`): `rfqChat(token, messages)` → `POST /v1/rfq-chat`;
add `messages?: ChatMessage[]` to the `RFQ` type; reuse `listRfqs` (sidebar),
`rfqQuotes`, `acceptQuote`. Reuse the dark design tokens in `globals.css`.

## 5. Resilience & error handling

- `rfq-chat` failure → inline error bubble ("Couldn't reach Core — retry"); the typed
  message is preserved for resend.
- Quote-poll failures are silent (transient); repeated failures flip the app-bar dot
  to offline.
- Accept failure → inline error on the card, button re-enabled.
- Terminal/empty states: no quotes → "Sellers' agents are responding…"; RFQ
  closed/won → order summary + accepts disabled.
- Auth reuses the existing hydration-safe token guard (`useMe`/`useToken`).

## 6. Testing

- **Backend (Go/testify):** `/v1/rfq-chat` heuristic path *asks then creates*
  deterministically (no key); tenant-guard rejects non-buyers; the `create` branch
  yields an RFQ + matches (reusing existing rfq-service coverage); the transcript is
  persisted on the RFQ and returned by `GET /v1/rfqs/:id`.
- **Frontend:** gate = a clean production build (Next compile + ESLint), matching how
  the web app is tested today; plus a small unit test for the chat reducer (append
  user/assistant turns; `done` → navigate) where practical.
- **Migration:** applies + reverts cleanly; existing RFQs (null `messages`) still load.

## 7. Decisions locked

- Full buyer chat shell; sellers/admins keep `AppShell`; buyers land on `/chat`.
- Multi-turn clarify → create, via a stateless `POST /v1/rfq-chat`; only the finished
  RFQ persists (with its transcript in a new `rfqs.messages jsonb`).
- Settings-driven LLM intake + deterministic heuristic fallback (works with no key).
- Quotes via ~4s polling of `rfqQuotes`; status = `/health` reachability (no buyer SSE).
- No streaming, no pre-create draft persistence, no seller/admin changes.

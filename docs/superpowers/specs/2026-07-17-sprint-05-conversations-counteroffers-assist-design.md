# Sprint 5 — Conversations, Counter-offers & Buyer AI Assist — Design

**Date:** 2026-07-17
**Sprint:** 5 of 10 · Tracks: Core · Agent · Frontend
**Depends on:** Sprints 1 (notify/threads), 2 (entitlements), 4 (orders/quotes head)
**Repos:** `tradebridge-core`, `tradebridge-agent`, `tradebridge-core/web`

## Goal

Make the buyer↔seller exchange a negotiation, not a one-shot quote: versioned counter-offers that
round-trip to the seller's agent, a persistent real-time thread, and a metered buyer-side AI assistant.

## Locked decisions (user, 2026-07-17)

- **Real-time:** add a **buyer-facing SSE** channel (true push), not polling.
- **Counter degradation:** a buyer counter **always posts to the durable thread**; it round-trips to the
  agent (`message.received` → `quote.counter`) only when the seller has **agent autonomy (Pro) + a
  connected agent**, else it stays a message the seller answers later. (Roadmap: Pro = agent autonomy.)
- **Buyer AI assist:** build **both** features (RFQ-writing help + quote-comparison explainer), run on
  **Haiku**, **metered** — Free capped (default 20/mo), Pro/Business unlimited — via the S2 usage engine.
- **Agent quote-quality eval:** build the **eval harness + a synthetic golden set now**; swap in real
  pilot catalogs later.

## Standing rules (carried forward)

- Merge to `master` **LOCALLY ONLY, never push**.
- DB/integration tests use `tradebridge_core_test` ONLY. Never live `tradebridge_core`.
- `BILLING_DEV_MODE=true` never on any internet-reachable env. `.env`/agent secrets never committed.

## Current state (what exists)

- **Gateway command protocol** (`internal/gateway/commands.go`): agents POST `/agent/v1/commands` with a
  closed vocabulary (`quote.submit`, `clarification.request`, `no_bid`); `quote.submit` → `quotes.Record`
  + `maybeQuote` + buyer notify. Authorization: agent's company must be a matched seller for the RFQ.
- **Event delivery** (`internal/eventbus`, `internal/protocol`): `agent_events` (durable, per-company,
  monotonic `seq`) + Redis channel `agent:events:<companyID>`; the **agent-facing** SSE
  (`internal/gateway/stream.go`) replays by seq then live-tails. **Buyers have no real-time channel.**
- **Quotes** (migration 007): `quotes` UNIQUE(rfq_id, seller_company_id), resubmit UPDATES in place —
  **no versioning**. `quote_messages` (author `buyer|seller`, body, attachments) already exists; REST
  `GET/POST /v1/quotes/:id/messages` (rate-limited). Accept/ranking/invoice all read `quotes`.
- **Agent reasoning** (`tradebridge-agent/src/reasoning/quote.ts`): already reasons over
  `ThreadMsg{author, body}` on `rfq.matched`; deterministic `StubProvider` heuristic when no LLM key.
- **Core LLM plumbing** (`internal/rfq/intake_llm.go`, `internal/settings`): provider abstraction,
  configurable model — reusable for the buyer assistant.
- **Entitlements** (`internal/entitlements`): generic `usage_counters(company_id, metric, period)` +
  `GetUsageCounter`/`IncrementUsageCounter`; `CheckRFQQuota`/`IncrementRFQUsage` + `PaywallError`
  pattern; plans carry `RfqMonthlyLimit`, `CatalogMaxItems`, `Seats`, `AgentAutonomy`.

## Architecture

Five subsystems across three repos. Core migrations 023 (quote versioning) + 024 (assist limit on plans).

### 1. Quote versioning + counter-offer loop (Core) — migration 023

- **`quote_versions`** table (immutable append-only): `id, quote_id REFERENCES quotes(id) ON DELETE
  CASCADE, version INT, price_cents, currency, delivery_days, notes, completeness, source
  (CHECK 'seller'|'counter'), agent_command_id (nullable), created_at`, `UNIQUE(quote_id, version)`.
- **`quotes.version INT NOT NULL DEFAULT 1`** — `quotes` remains the **head**; a version row is appended
  whenever the head changes, so ranking/accept/invoice keep reading `quotes` unchanged.
- **`quotes.Record`** (used by `quote.submit`) also writes version 1. A new **`quotes.RecordCounter`**
  appends version N+1 and updates the head (in one tx).
- **Buyer counter:** `POST /v1/quotes/:id/counter {price_cents?, delivery_days?, message}` (buyer on the
  RFQ only): records a `quote_messages` row (author `buyer`, `body` = the human-readable message; the
  proposed terms are summarized into the body text — **no new column** on `quote_messages`). The
  structured counter terms travel to the agent in the event payload: — **only if
  `entitlements.For(seller).AgentAutonomy`** — publishes a **`message.received`** agent event to the
  seller (payload `{quote_id, rfq_id, counter:{price_cents, delivery_days}, thread:[{author,body}...]}`).
  Always publishes the new message to the buyer thread channel (§2).
- **Agent reply command `quote.counter`** (added to `supportedCommands`): payload `{quote_id, price,
  currency, delivery_days?, notes?}`. Core validates (agent matched to the quote's RFQ; quote's
  seller == agent company), then `RecordCounter` (source `counter`, `agent_command_id=cmd.ID`),
  records a `quote_messages` row (author `seller`), notifies the buyer (`quote.countered`), and streams
  to the thread channel. Idempotency + audit as with existing commands.
- Accept unchanged: it reads the `quotes` head (the latest version), snapshotting terms into the PO.

### 2. Buyer-facing thread SSE (Core)

- **`GET /v1/quotes/:id/thread/stream`** (tenant guard; party-only = buyer on the RFQ or the quote's
  seller): sets `text/event-stream`, subscribes to Redis channel **`thread:<quoteID>`**, emits keepalive
  pings, and live-tails. **No durable replay** — the page loads the thread over REST first and re-fetches
  on reconnect (a thread is ephemeral while open; this is deliberately lighter than the agent gateway).
- **Publish points:** a small `thread` publisher (`internal/thread` or a helper in `internal/quotes`)
  publishes a compact envelope `{kind:"message"|"version", ...}` to `thread:<quoteID>` whenever a
  `quote_messages` row or a `quote_versions` row is created (buyer counter, seller counter, plain
  message). Redis may be nil → publish is a no-op (dev/test).
- Thread REST payload (`GET /v1/quotes/:id`) extended to include `versions` alongside `messages`.

### 3. Buyer AI assistant, metered on Haiku (Core) — migration 024

- **`plans.assist_monthly_limit INT NOT NULL DEFAULT 20`** (free), Pro/Business `-1` (unlimited); wire
  into `entitlements.Entitlements` + `For()` + `freeFallback` (20).
- **Metering:** `metricAssist = "assist"`; `CheckAssistQuota` / `IncrementAssistUsage` mirror the RFQ
  pattern (usage_counters, per-month period, `PaywallError{Feature:"assist_monthly_limit"}`).
- **`internal/assist`** package: a `Service` wrapping the existing LLM provider **pinned to Haiku**
  (`claude-haiku-4-5`; falls back to a deterministic stub when no key, so tests + keyless dev work):
  - `POST /v1/assist/rfq {draft}` → `{suggestion}` (a sharpened RFQ draft).
  - `POST /v1/assist/compare {rfq_id}` → `{explanation}` (plain-language comparison of the RFQ's quotes;
    buyer-on-RFQ only). Both gated by `CheckAssistQuota` (paywall on cap) + `IncrementAssistUsage`.
- Non-streaming first (simpler); streaming is a documented follow-up.

### 4. Frontend (core/web)

- **`web/lib/thread.ts`**: getThread (messages + versions), postMessage, postCounter, acceptLatest, and
  `openThreadStream(quoteId, token, onEvent)` (EventSource-style SSE consumer with token via query or
  header-capable fetch stream — match the existing SSE client in `lib/sse.ts`).
- **`web/lib/assist.ts`**: assistRFQ(draft), assistCompare(rfqId).
- **Quote thread component** on the RFQ/quote detail: message list + version history, counter-offer form
  (price/delivery/message), live updates via the SSE, "Accept latest".
- **Buyer assistant panel**: "Improve this RFQ" on the composer + "Explain these quotes" on the compare
  view; shows metered usage and the paywall when capped.

### 5. Agent counter reasoning + quote-quality eval (tradebridge-agent)

- **Counter reasoning:** subscribe/handle the new `message.received` event → build the thread + counter
  context → reason (extends `src/reasoning/quote.ts`'s `ThreadMsg` path; deterministic stub when no key)
  → send a `quote.counter` command via the existing command client. Reuse the connector/gateway client.
- **Golden-RFQ eval harness** (`tradebridge-agent/src/eval/` + a `bun run eval` script): a fixture
  catalog + 10–20 **synthetic** RFQs each with an expected `{price_min, price_max, delivery_max}`; the
  runner feeds each through the deterministic quote heuristic (`StubProvider` path — CI has no LLM key)
  and asserts the produced quote falls in range. Exit non-zero on any miss (fails CI). A README notes
  how to swap in real pilot catalogs later.

## Testing strategy

- **Core Go:** version append + head bump; counter authorization (buyer-only counter, seller-agent-only
  `quote.counter`, matched-to-RFQ); `message.received` emitted only when seller has autonomy; SSE
  publish on message/version (assert Redis publish via a fake/nil-safe path); assist metering + paywall +
  Haiku-stub output; accept reads the head. DB tests → `tradebridge_core_test`.
- **Agent (bun):** counter reasoning produces a valid `quote.counter`; the eval runner passes on the
  synthetic set and fails when a fixture range is violated.
- **Web:** vitest on `lib/thread.ts` + `lib/assist.ts` (mock fetch/EventSource); pages via tsc + build.
- **E2E #5** (Core integration, agent simulated via a `quote.counter` gateway command): buyer counters →
  `message.received` recorded → simulated agent `quote.counter` → new `quote_versions` row + head bump +
  buyer notified → buyer accepts the latest → order/invoice reflect the countered terms.

## Config

- No new required config. Assist model defaults to Haiku via the settings/provider layer; keyless →
  deterministic stub. Redis reused for the thread channel (nil-safe).

## Plan decomposition (5 plans + E2E #5)

1. **Quote versioning + counter loop** — migration 023; `quote_versions` + `quotes.version`;
   `quotes.RecordCounter`; buyer `POST /v1/quotes/:id/counter` (+`message.received` gated on autonomy);
   gateway `quote.counter`; buyer notification.
2. **Buyer thread SSE** — `GET /v1/quotes/:id/thread/stream`; `thread:<quoteID>` publisher wired to
   message/version creation; thread REST payload gains `versions`.
3. **Buyer AI assistant + metering** — migration 024 (`plans.assist_monthly_limit`); assist metric +
   quota/paywall; `internal/assist` Haiku service; `POST /v1/assist/rfq` + `/v1/assist/compare`.
4. **Frontend** — `lib/thread.ts` + `lib/assist.ts`; thread component (history + counter + live SSE +
   accept-latest); buyer assistant panels; metering/paywall UX.
5. **Agent counter reasoning + eval set** — `message.received` → `quote.counter`; golden-RFQ eval harness
   + synthetic set + `bun run eval`.

Plus **E2E #5** in the Core integration suite.

## Deferred (documented)

- Streaming assist responses (start non-streaming); plain buyer messages triggering agent replies (only
  structured counters round-trip this sprint); durable-replay buyer SSE (live-tail only); real pilot
  catalogs in the eval set; multi-turn free-form negotiation beyond counter/quote.

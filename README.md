# TradeBridge Core + Gateway

The neutral marketplace layer: companies, RFQs, quote ranking, orders, billing,
audit — plus the **Agent Gateway** that holds seller-agent SSE connections and
delivers events. Go / Fiber. See the Implementation Plan for the full picture.

This repo is **Phase 0** — everything is wired, nothing ships product behavior yet.

## Layout

```
cmd/api        Core API entrypoint            (:8080)
cmd/gateway    Agent Gateway (SSE) entrypoint (:8081)
internal/config  Viper config
internal/di      Google Wire dependency graph
pkg/middleware   JWT auth, tenant guard, request logging
pkg/ulid         app-side ULID generation
pkg/db           migrations + sqlc queries/generated code
web/           Next.js buyer/admin UI         (:3000)
```

## Prerequisites

- Go 1.26+, Docker + Compose
- CLI tools: `sqlc`, `migrate` (golang-migrate), `wire`, `golangci-lint`, `air`
- Node 24+ (for `web/`)

## Quick start

```bash
cp .env.example .env
make up          # start Postgres + Redis
make generate    # sqlc + wire codegen (run after cloning / editing queries)
make migrate     # apply migration 001
make dev         # Core API with hot reload  -> http://localhost:8080/health
make gateway     # Agent Gateway             -> http://localhost:8081/agent/v1/stream

# Frontend (separate process)
cd web && npm install && npm run dev   # -> http://localhost:3000
```

## Make targets

`up` · `down` · `dev` · `gateway` · `migrate` · `migrate-down` · `generate` ·
`test` · `lint` · `build` · `docker` · `demo-seed` · `simseller`

## Seller RFQ inbox

Human sellers see the RFQs Core matched to their company in the web app at
`/seller/rfqs` (list with Awaiting quote / Quoted / Closed tabs and search) and
`/seller/rfqs/:id` (requirements, buyer attachments, their own quote and the
quote thread). The API is `GET /v1/seller/rfqs?tab=open|quoted|closed&q=` and
`GET /v1/seller/rfqs/:id`; visibility comes from `rfq_matches`, so unmatched
companies get 404, and the buyer's intake chat is never returned. Suppliers sign
up at `/seller/register` (creates a seller company and its supplier profile in
one step so matching works immediately) and sign in at `/seller/login`.

Sellers can quote by hand from the RFQ page (`POST /v1/seller/rfqs/:id/quote`,
email verification required) as well as through their agent on the Agent
Gateway. The first submission behaves like the agent's `quote.submit`; later
ones are revised offers that behave like `quote.counter` (new quote version,
seller thread message, buyer notified). Accepted or rejected quotes and closed
RFQs cannot be changed. Sellers without an autonomous agent are notified when
the buyer counters, since a person has to answer.

## Autonomous negotiation

Buyers can hand an RFQ to a negotiation agent (`internal/negotiation`). The
mandate (`PUT /v1/rfqs/:id/negotiation`) sets a target price, walk-away price,
max delivery days, optional payment terms, issue weights, round budget, tone
(`collaborative` linear / `firm` Boulware concession curve) and whether to
auto-accept (Pro+). The engine answers every supplier quote version once
(idempotent on `quote_id, version`), counters through the normal buyer
counter path, records each round with its rationale, and closes on the
best-utility offer within the walk-away price: auto-accepting it or holding it
for `POST /v1/rfqs/:id/negotiation/approve`. A background worker ticks active
negotiations every `NEGOTIATION_TICK_SECONDS`; suppliers that don't answer a
counter within `NEGOTIATION_SELLER_TIMEOUT_MINUTES` are evaluated on their
last terms. Savings/spend analytics: `GET /v1/negotiations/savings`.

**Safety under real money.**
- *One instance per RFQ:* the worker runs on every API replica. Ticks, closes,
  auto-accepts, approvals and declines hold a Postgres advisory lock keyed on
  the RFQ (`pg_try_advisory_xact_lock` for ticks, which skip a busy RFQ;
  blocking with a 10s `lock_timeout` for buyer actions, which answer 409 when it
  runs out).
- *Crash-safe counters:* a round row and its counter (thread message + durable
  `message.received` agent event) commit in one transaction marked
  `delivery_status=pending`; live side effects (Redis, notifications) run after
  commit and mark it `sent`. A crash in between is replayed on the next tick.
  You never get a round without its counter, or two counters for one seller
  version.
- *Mandate invariants* (never above walk-away or past the delivery limit,
  counters never retreat, at most `max_rounds`) are property- and fuzz-tested:
  `go test -run Property ./internal/negotiation`,
  `go test -fuzz=FuzzDecide ./internal/negotiation`.
- *Kill switches:* platform admins use `GET/PUT /admin/v1/negotiations/kill-switch`
  (admin console → Negotiations; a reason is required). Company owners and
  admins use `GET/PUT /v1/negotiations/controls`, with the control on the
  Negotiations page. Either switch stops all countering, accepting and
  auto-accepting. Negotiation views carry `pause` so the UI can say who paused
  it. Every flip is recorded in `negotiation_kill_switch_log`; company flips
  also go to `audit_logs`.

### Payment terms and logrolling

Payment terms (`payment_terms_days`: days after delivery the buyer pays;
`0` = cash on delivery, `30`, `60`, `90` …) are a third negotiable issue.
The mandate optionally takes `preferred_payment_terms_days` (0–180, longer is
better for the buyer), `min_payment_terms_days` (shortest acceptable, ≤
preferred) and `terms_weight`. Weights are shares of one: price
`price_weight`, terms `terms_weight` (default 0.15 when preferred terms are
set), delivery gets the rest; `price_weight + terms_weight ≤ 1`. The view
returns the split as `weights: {price, delivery, terms}`. A mandate without
preferred terms is scored and negotiated exactly as before (two issues).

With terms in the mandate, when a supplier resists on price (holds, or
concedes under a third of the gap since our last offer), the agent logrolls:
it concedes on the issue the buyer weighs less — faster payment (snapped to
standard terms, never below the minimum) or a longer delivery (never past
the limit) — and asks for a lower price than a straight concession, sized so
the buyer's utility is unchanged. The round rationale explains the trade.
Walk-away guarantees hold on every issue: no offer above the walk-away price,
no acceptance of terms below the minimum or a stated delivery past the
limit, and only acceptable rivals are used as leverage. Rounds record
`seller_payment_terms_days`, `offer_payment_terms_days` and `message_source`.

**LLM-phrased counters.** The engine decides every figure. If an LLM key is
configured in the platform settings (the same Claude client and settings RFQ
intake uses) and `NEGOTIATION_LLM_MESSAGES=true` (default), the decided move
is passed to the model to word a supplier-facing message that discloses the
buyer is represented by an AI agent. The text is used only if it states
exactly the engine's price, delivery and terms, contains no other figure or
money amount and does not claim a final offer the engine didn't make;
otherwise — or on error or after `NEGOTIATION_LLM_TIMEOUT_MS` (default
4000) — the deterministic template is sent. `message_source` is `llm` or
`template`.

### Agent protocol: payment terms (backward compatible)

- `quote.submit` and `quote.counter` payloads accept an optional
  `payment_terms_days` (integer 0–365). Omitted means "not stated" on a
  first quote and "unchanged" on a revision, so agents built before terms
  existed keep working unchanged.
- The buyer-counter event `message.received` carries
  `counter.payment_terms_days` only when the buyer proposed terms
  (`omitempty`), alongside `counter.price_cents` / `counter.delivery_days`.
- The seller manual quote (`POST /v1/seller/rfqs/:id/quote`), buyer counter
  (`POST /v1/quotes/:id/counter`), quote lists and the seller RFQ view accept
  or return `payment_terms_days` the same way.

The simulated sellers (`internal/simseller`) open with their own terms, give
credit up to a limit, and lower their floor for faster payment or longer
lead time, so `make demo-seed` (four RFQs negotiate terms) shows real
logrolled rounds.

**Local demo** (API + gateway running, `RATE_LIMIT_ENABLED=false`):
`make demo-seed` creates `demo@northwind.test` / `Password123!`, three
simulated sellers and seven RFQs negotiated end-to-end; `make simseller`
keeps the simulated sellers online for new RFQs. Both are local-only tools.

## Notes

- **`gen_ulid()`** — Postgres has no built-in ULID, so migration 001 defines a
  plpgsql generator used as the `DEFAULT` for every id column.
- The Gateway's `/agent/v1/stream` is an **empty** keepalive stream in Phase 0.
  Redis pub/sub, durable event replay and acks land in Phase 1 (plan §5).

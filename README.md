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
max delivery days, price-vs-delivery weight, round budget, tone
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

**Local demo** (API + gateway running, `RATE_LIMIT_ENABLED=false`):
`make demo-seed` creates `demo@northwind.test` / `Password123!`, three
simulated sellers and seven RFQs negotiated end-to-end; `make simseller`
keeps the simulated sellers online for new RFQs. Both are local-only tools.

## Notes

- **`gen_ulid()`** — Postgres has no built-in ULID, so migration 001 defines a
  plpgsql generator used as the `DEFAULT` for every id column.
- The Gateway's `/agent/v1/stream` is an **empty** keepalive stream in Phase 0.
  Redis pub/sub, durable event replay and acks land in Phase 1 (plan §5).

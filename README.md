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

**Local demo** (API + gateway running, `RATE_LIMIT_ENABLED=false`):
`make demo-seed` creates `demo@northwind.test` / `Password123!`, three
simulated sellers and seven RFQs negotiated end-to-end; `make simseller`
keeps the simulated sellers online for new RFQs. Both are local-only tools.

## Notes

- **`gen_ulid()`** — Postgres has no built-in ULID, so migration 001 defines a
  plpgsql generator used as the `DEFAULT` for every id column.
- The Gateway's `/agent/v1/stream` is an **empty** keepalive stream in Phase 0.
  Redis pub/sub, durable event replay and acks land in Phase 1 (plan §5).

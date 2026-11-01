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

### How savings are measured

An opening quote is easy to inflate, so savings against it alone mean little.
Every deal is measured against up to three baselines
(`internal/negotiation/baseline.go`), and the headline **verified savings** use
the lowest one that applies:

| Baseline | Definition | Used when |
| --- | --- | --- |
| Market | Lower median of all suppliers' opening (v1) quotes on the RFQ, same currency (with two quotes, the lower one) | at least 2 same-currency quotes |
| Historical | The buyer's most recent confirmed/fulfilled/completed order in the same category and currency, per-unit price scaled to this RFQ's quantity | same quantity unit, quantity 0.5–2x, item-term similarity ≥ 0.5, ≤ 365 days old |
| Opening quote | The winning supplier's own opening quote | it is not flagged as an anchor |

An opening quote is flagged as a likely **anchor** when it is more than 25%
above the lower median of the other suppliers' openings
(`AnchorMarketThreshold`) or more than 35% above the historical baseline
(`AnchorHistoricalThreshold`). Anchored openings never count as a baseline, and
the flag shows in the negotiation room and agent log. Currencies are never
compared with each other. If the final price is not below the chosen baseline,
verified savings are zero.

Baselines are snapshot into `negotiation_baselines` (migration 037) whenever a
negotiation closes (agent close, approval, awarding another supplier, or an
external close), so reported figures don't drift. Migration 037 backfills
existing negotiations from immutable quote versions (opening + market only;
`source = 'backfill'`). Orders without a snapshot are measured live against
the RFQ's opening quotes.

API: the negotiation view adds `baselines` (every baseline, `verified_basis`,
`reason`, `anchor`, `snapshot`/`source`), `summary.verified_*` and per-supplier
`anchored`/`anchor_note`; `GET /v1/negotiations/savings` adds `verified`
(headline totals in the buyer's main currency, basis counts, explanation),
`monthly[].verified_savings_cents` and per-deal verified fields in
`recent_deals`; `GET /v1/negotiations` adds `verified_savings_cents` for
closed deals. Existing opening-quote fields (`totals`, `savings_pct`,
`summary.savings_cents`, ...) are unchanged.

**Local demo** (API + gateway running, `RATE_LIMIT_ENABLED=false`):
`make demo-seed` creates `demo@northwind.test` / `Password123!`, three
simulated sellers and seven RFQs negotiated end-to-end; `make simseller`
keeps the simulated sellers online for new RFQs. Both are local-only tools.

## Notes

- **`gen_ulid()`** — Postgres has no built-in ULID, so migration 001 defines a
  plpgsql generator used as the `DEFAULT` for every id column.
- The Gateway's `/agent/v1/stream` is an **empty** keepalive stream in Phase 0.
  Redis pub/sub, durable event replay and acks land in Phase 1 (plan §5).

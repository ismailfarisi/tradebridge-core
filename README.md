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
`test` · `lint` · `build` · `docker`

## Notes

- **`gen_ulid()`** — Postgres has no built-in ULID, so migration 001 defines a
  plpgsql generator used as the `DEFAULT` for every id column.
- The Gateway's `/agent/v1/stream` is an **empty** keepalive stream in Phase 0.
  Redis pub/sub, durable event replay and acks land in Phase 1 (plan §5).

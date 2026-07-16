# Sprint 1 — Plan 3: Minimal CI (test-on-push)

**Goal:** Add test-on-push CI to both repos so the following sprints run on a safety net. **Sentry is deferred** (user decision, 2026-07-16). Deploy gates, previews, and a status page stay in Sprint 10.

**Scope decision:** CI only; Sentry not now. Both repos (both have GitHub remotes). Land **locally-committed** (inert until pushed) — matches the local-only pattern of Plans 1 & 2.

## Workflows

### `tradebridge-core/.github/workflows/ci.yml`
- **core-go** job: `postgres:16` service (tradebridge/tradebridge/tradebridge, health-checked); `TEST_DATABASE_URL` points at it; installs `golang-migrate` (`go install ...@v4.18.1`); applies migrations; runs `go vet`, `go build ./...`, `go test ./...`. Go version from `go.mod` (1.26.4). No Redis service — the integration suite builds the API/gateway with `Redis: nil` (fail-open) and only needs Postgres.
- **core-web** job (working-directory `web`): Node 20, `npm ci`, `npx tsc --noEmit`, `npm run test` (vitest), `npm run build`.

### `tradebridge-agent/.github/workflows/ci.yml`
- **agent** job: `oven-sh/setup-bun@v2` (bun 1.3.14), `bun install --frozen-lockfile`, `bun run typecheck`, `bun test`. Self-contained (temp SQLite) — no services.

Triggers: `push` + `pull_request`; `concurrency` cancels superseded runs per ref.

## Local verification (every CI command was run locally and passed)
- Core Go: `go build ./...` clean; `go test ./...` green (integration suite ~12s, dev Postgres on :5442).
- Core web: `npx tsc --noEmit` clean; `vitest` green; `next build` clean (14 routes).
- Agent: `bun run typecheck` clean; `bun test` → **250 pass / 2 skip / 0 fail** across 75 files.

## Known caveats / follow-ups
- **Go 1.26.4 on GitHub runners:** `go-version-file: go.mod` requests 1.26.4. If GitHub's hosted runners don't yet offer that exact toolchain, pin `go-version` to an available release. This can only be confirmed once pushed.
- **Sentry (deferred):** the `SENTRY_DSN` config field already exists in Core (added in Plan 1). Wiring `sentry-go` / `@sentry/nextjs` / `@sentry/bun` is a later task.
- **Activation:** these files do nothing until `master` (with them) is pushed to `origin`. First push publishes all local commits (Plans 1–3).

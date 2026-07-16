# Gateway Hardening — Rate Limiting + Credential Lifecycle — Design

**Date:** 2026-07-16
**Repo:** tradebridge-core (+ a small agent-side log tweak)
**Status:** Approved, ready for implementation plan

## Problem

The Core↔agent gateway authenticates a seller's agent by a long-lived
`Bearer <id>.<secret>` credential and correctly isolates each agent to its own
`company_id`. Two hardening gaps remain:

1. **No rate limiting** — a valid (or compromised) agent can spam `/agent/v1/*`
   commands, and there is no throttle on repeated failed-auth attempts
   (credential brute-forcing).
2. **Credentials never expire** — a leaked secret is valid forever; the only
   remedy is manual revoke, and there is no first-class rotation path.

## Goals

1. Throttle gateway traffic per authenticated credential, and throttle
   failed-auth attempts per source IP.
2. Give credentials a TTL and a zero-downtime rotation path.

Non-goals (YAGNI): mTLS, automatic secret push to the self-hosted agent (rotation
is a manual `.env` update within the grace window), per-endpoint custom limits,
anomaly detection/alerting.

## Operational constraints (drive the design)

- The agent holds its credential as a static `AGENT_TOKEN` in its `.env`; it only
  picks up a new secret via an `.env` edit + restart. So any hard rejection
  (expiry) takes the agent offline until an operator acts.
- The gateway runs as a separate process (`cmd/gateway`) and already holds a
  `*redis.Client` (`h.redis`, may be nil). The API process (`cmd/api`) owns
  credential issuance/management (`internal/agent`).
- Deployment is behind Cloudflare → the real client IP is in `CF-Connecting-IP`,
  not `c.IP()`.

## Feature A — Credential lifecycle (TTL + rotation)

Decisions: **default TTL 90 days** (env `AGENT_CRED_TTL_DAYS`, `0` = never);
**rotation grace 7 days** (env `AGENT_CRED_ROTATE_GRACE_DAYS`). Existing
credentials are backfilled to `now() + TTL` so the live agent gets a full runway
rather than an instant 401.

### Schema (migration `013_credential_expiry`)

```sql
ALTER TABLE agent_credentials ADD COLUMN expires_at TIMESTAMPTZ;
-- Grandfather existing credentials onto the policy WITH a full runway from deploy
-- (not retroactively expired) so the live agent keeps working:
UPDATE agent_credentials SET expires_at = NOW() + INTERVAL '90 days'
  WHERE expires_at IS NULL AND status = 'active';
```

`expires_at IS NULL` means "never expires" (the `TTL=0` escape hatch and any
explicitly-unbounded credential). Down migration drops the column.

Note: the migration is static SQL and cannot read `AGENT_CRED_TTL_DAYS`, so the
one-time grandfather backfill uses a fixed **90 days** from deploy. The env TTL
governs *new* issuance and rotation going forward; the two only differ if an
operator sets a non-90 TTL, in which case existing creds still got a 90-day
runway (acceptable for a one-time migration).

### Enforcement (gateway `authenticate()`)

After the existing status + bcrypt checks, add:

```
if cred.ExpiresAt is set AND cred.ExpiresAt <= now():
    return 401 "credential expired"
```

### Issuance (`internal/agent` `issue`)

Set `expires_at = now() + ttl` when `AGENT_CRED_TTL_DAYS > 0`, else NULL.
New sqlc `CreateAgentCredential` gains an `expires_at` param.

### Rotation (`POST /v1/agent-credentials/:id/rotate`)

Owner/admin, tenant-scoped (the `:id` credential must belong to the caller's
company). Mechanism = **issue-new + expire-old-after-grace** (reuses issuance;
avoids storing two hashes per row):

1. Load `:id`; 404 if not found or not this company; 409 if already revoked.
2. Create a NEW credential (new id + secret, same `label`, fresh TTL).
3. Set the OLD credential's `expires_at = min(existing, now() + grace)` (so the
   old secret keeps authenticating for the grace window, then dies). Old row
   stays `status='active'` — expiry, not revocation, is what ends it.
4. Return the new full secret `<newid>.<newsecret>` exactly once (same shape as
   issue).

During grace both `<oldid>.<oldsecret>` and `<newid>.<newsecret>` authenticate,
so the operator updates the agent's `.env` + restarts with zero downtime.

### Visibility (`GET /v1/agent-credentials`)

Owner/admin, tenant-scoped. Lists this company's credentials:
`{ id, label, status, last_seen, expires_at, created_at }` (never the secret) so
an owner can see what's expiring and rotate ahead of time. New sqlc
`ListAgentCredentialsByCompany`.

## Feature B — Gateway rate limiting

Decisions: per-credential **120 req/min** (`AGENT_RL_PER_MIN`); per-IP
failed-auth **20 / 300s** (`AGENT_AUTHFAIL_MAX`, `AGENT_AUTHFAIL_WINDOW_SEC`).
Redis-backed, **fail-open** when `h.redis == nil` or a Redis call errors.

### Where

Centralized in `gateway.authenticate()` — the single choke point every handler
(`command`, `stream`, `ack`, `heartbeat`, `attachment`) calls. Flow:

```
ip := clientIP(c)                      // CF-Connecting-IP → X-Forwarded-For → c.IP()
if authFailBlocked(ip):  return 429 (Retry-After = window)   // pre-auth, cheap
cred := lookup(id)
if invalid/expired/revoked:
    recordAuthFailure(ip)              // INCR rl:authfail:<ip>, EXPIRE window
    return 401
if perCredOverLimit(cred.ID):  return 429 (Retry-After = 60)
return agent
```

### Redis keys (fixed-window counters)

- `rl:cred:<id>:<epochMinute>` — `INCR`; on first hit `EXPIRE 60`. Over
  `AGENT_RL_PER_MIN` ⇒ 429.
- `rl:authfail:<ip>` — `INCR` on each failure; on first hit
  `EXPIRE AGENT_AUTHFAIL_WINDOW_SEC`. Value `>= AGENT_AUTHFAIL_MAX` ⇒ the IP is
  blocked (429) until the key expires.

Any Redis error or `h.redis == nil` ⇒ skip the check (fail-open), logged once at
startup / at debug on error — rate limiting must never take the gateway down
(matches the existing "redis may be nil → live pub/sub disabled, replay still
works" tolerance).

### Config plumbing

Add the five env fields to `internal/config/config.go` (with `SetDefault`s) and
pass the gateway-relevant ones into `gateway.NewHandler(...)` (a small
`RateLimitConfig` struct) from `cmd/gateway/main.go`. `AGENT_CRED_TTL_DAYS` is
read by the API process (issuance) via `internal/agent`.

## Agent-side (small) — legible 401

`tradebridge-agent/src/connector/index.ts` currently throws
`stream returned HTTP <status>` on any non-OK and reconnects with backoff. Add:
on `res.status === 401`, `logAgent("error", …)` a loud, unambiguous line
("Core rejected the agent credential (401) — it may be expired or revoked;
rotate AGENT_TOKEN in .env and restart") before the throw, so an unattended
operator learns why the agent went quiet instead of watching silent retries.
(Behavior otherwise unchanged — it still backs off and retries, in case the 401
is transient during a rotation cutover.)

## Data Flow

```
Agent request → gateway.authenticate():
   authFailBlocked(ip)? ──▶ 429                 (brute-force cooldown)
   bad/expired secret  ──▶ recordAuthFailure(ip) ▶ 401
   good secret, over 120/min ──▶ 429
   good secret, ok ──▶ handler runs, scoped to cred.CompanyID

Owner rotates:  POST /v1/agent-credentials/:id/rotate
   ▶ new cred (fresh TTL) returned once
   ▶ old cred expires_at = now()+grace  (both work during grace)
Owner lists:    GET /v1/agent-credentials  ▶ see expires_at/last_seen, rotate early
```

## Error Handling

- Expired credential → 401 (distinct log reason "credential expired") so it is
  attributable vs a wrong secret.
- Redis down / nil → fail-open, never 5xx from the limiter.
- Rotate on an unknown/foreign credential → 404; on a revoked one → 409.
- The grace-window `min(existing, now()+grace)` guards against a rotate
  *extending* an already-sooner expiry.

## Testing

Go (unit + gateway integration with a test Redis or a small fake):
- `authenticate()`: valid non-expired ✓; `expires_at` in the past → 401; NULL
  `expires_at` → never expires.
- Rate limit: N calls under limit pass; the (N+1)th in the window → 429; counter
  resets next window. Fail-open when redis nil (no 429, no error).
- Auth-fail: `AGENT_AUTHFAIL_MAX` bad attempts from an IP → subsequent 429;
  distinct IPs independent; window expiry clears the block.
- `clientIP`: prefers `CF-Connecting-IP`, then `X-Forwarded-For` first hop, then
  `c.IP()`.
- Issuance sets `expires_at` when TTL>0, NULL when TTL=0.
- Rotation: new cred created with fresh TTL; old cred `expires_at` set to
  `now()+grace`; both authenticate during grace; old 401s after; foreign/revoked
  guards.
- `ListAgentCredentialsByCompany` returns only the caller's company, no secret.

Live-verify (per `verify`): rotate the real credential and confirm both secrets
work during grace + the agent keeps running; trip the per-cred 429; confirm the
existing agent is unaffected by the migration (still authenticates, sees its
new `expires_at`).

## Files Touched

**Core:**
- `pkg/db/migrations/013_credential_expiry.{up,down}.sql` (new)
- `pkg/db/query/agent_credentials.sql` (Create +expires_at; new List; SELECT * picks up column) + `sqlc generate`
- `internal/gateway/gateway.go` (`authenticate()`: expiry check + rate-limit + auth-fail + `clientIP`; `RateLimitConfig`; `NewHandler` signature)
- `internal/gateway/ratelimit.go` (new — Redis counters, fail-open) + `_test.go`
- `internal/agent/handler.go` + service (rotate endpoint, list endpoint, TTL on issue)
- `internal/config/config.go` (5 env fields + defaults)
- `cmd/gateway/main.go` (pass RateLimitConfig) ; `internal/httpapi/router.go` / `cmd/api` wiring for TTL if needed
- `.env.example` (document the 5 new vars)

**Agent:**
- `src/connector/index.ts` (loud 401 log)

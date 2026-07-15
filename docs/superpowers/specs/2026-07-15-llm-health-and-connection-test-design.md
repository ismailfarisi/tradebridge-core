# LLM Connection Test + Fallback Health Banner — Design

**Date:** 2026-07-15
**Repo:** tradebridge-core
**Status:** Approved, ready for implementation plan

## Problem

The global reasoning/extraction provider (Admin → Settings, `platform_settings`
singleton) can be misconfigured — e.g. a wrong/case-mismatched model id — and the
endpoint returns a non-200. The RFQ intake (`llmIntake`) and extractor
**silently degrade to the deterministic heuristic** on any non-200, logging only a
`warn`. To an operator the buyer chat then looks "not using AI": it re-asks the
canned `"What product or material are you looking to source?"` and titles the RFQ
from the buyer's first message line.

Concretely: the model was saved as `Qwen3.7-Plus`; the endpoint only serves
`qwen3.7-plus` (ids are case-sensitive) → 404 `"Model not exist."` → every intake
fell back. Nothing in the admin UI surfaced this.

## Goals

1. Let an admin **verify** the saved provider config works, with one click.
2. **Surface** silent heuristic fallback so an operator knows the AI is degraded.

Non-goals (YAGNI): fallback history/charts, per-company tracking, a configurable
window size, persistence across restart.

## Feature 1 — Test Connection (tests the saved config)

Decision: the button tests **whatever is currently saved** (edit → Save → Test).
No unsaved-form/key-fallback logic.

### Backend

- **`rfq.PingProvider(ctx context.Context, cfg ProviderConfig, logger zerolog.Logger) error`**
  - Reuses the existing adapters (`newOpenAIChat` / `newAnthropicChat`) behind the
    `llmChat` seam.
  - Makes ONE minimal call: `complete(ctx, "reply with ok", [{role:user, content:"ping"}])`
    under a ~15s context timeout.
  - If `cfg.APIKey == ""` → returns `errors.New("no API key configured")` (truthful:
    that config runs in heuristic mode).
  - Returns `nil` on a 200, else the underlying error. The openai adapter's
    `httpStatusError` is extended to carry the status code and a short response
    snippet so the message is actionable (e.g. `openai chat 404: Model not exist.`).
    (This also improves the existing warn logs.)
- **`settings.Service.TestConnection(ctx) (TestResult, error)`**
  - Resolves the effective/saved config via `ExtractionConfig(ctx, "")`.
  - Times the `PingProvider` call.
  - `TestResult = { Ok bool; Model string; LatencyMs int64; Error string }`.
- **Route:** `POST /admin/v1/settings/test` under the platform-admin guard →
  `TestResult` as JSON. Always HTTP 200 (the result body carries ok/error;
  a failed provider call is not an HTTP error of our API).

### Frontend

- `lib/admin.ts`: `testAdminSettings(token) => { ok, model, latency_ms, error }`.
- `app/admin/page.tsx`: a **"Test connection"** button (ghost) beside **Save**.
  On click: disabled + "Testing…", then inline result:
  - ✅ `Connected — qwen3.7-plus (312 ms)`
  - ❌ `Failed: openai chat 404: Model not exist.`

## Feature 2 — Fallback Health Tracker + Banner

Decision: **in-memory recent window**, resets on restart.

### Backend

- **`rfq.HealthTracker`** — a focused, thread-safe unit (its own file,
  `internal/rfq/health.go`).
  - Holds a ring of the last **N = 20** LLM outcomes (bool: fell back or not),
    plus `lastError string` and `lastFallbackAt time.Time`.
  - `NewHealthTracker(n int) *HealthTracker`
  - `RecordSuccess()` — pushes a success outcome.
  - `RecordFallback(err error)` — pushes a fallback outcome; stores `lastError`
    (err.Error(), trimmed/capped) and `lastFallbackAt`.
  - `Snapshot() HealthSnapshot` where
    `HealthSnapshot = { Window int; RecentFallbacks int; LastError string;
    LastFallbackAt *time.Time; Degraded bool }`.
    - `RecentFallbacks` = count of fallbacks currently in the ring.
    - **`Degraded`** = the single most recent outcome was a fallback. One success
      after a run of failures clears `Degraded` (banner disappears), while
      `RecentFallbacks` still reflects the window until the successes age out.
  - Mutex-guarded; all methods safe for concurrent callers.

- **Wiring (in `internal/httpapi/router.go`):**
  - Create once: `health := rfq.NewHealthTracker(20)`.
  - Inject into `rfq.NewService(...)` → threaded into `BuildIntake(cfg, logger, health)`
    and `BuildExtractor(cfg, logger, health)`.
  - Inject into `settings.NewService(...)` so the admin routes can read it.
  - Both services import `rfq` already (no new import cycle: `rfq` imports neither).

- **Recording sites:**
  - `intake_llm.go`: in `llmIntake.Decide` / `DecideStream`, call
    `health.RecordFallback(err)` on the non-200/stream-error → heuristic path, and
    in `decisionFromRaw` on the unparseable-JSON → heuristic path; call
    `health.RecordSuccess()` when a valid decision is produced from the model.
  - The extractor's equivalent LLM path: `RecordFallback` on its non-200 fallback,
    `RecordSuccess` on a parsed extraction.
  - **Not recorded:** the pure-heuristic agents returned when `APIKey == ""`
    (BuildIntake/BuildExtractor short-circuit before constructing the LLM adapter).
    That "not configured" state is already conveyed by `has_key: false` — the
    banner is strictly for *configured-but-failing*.
  - The `ctx.Err()` (caller-went-away) branches do **not** record — a departed
    client is not a provider failure.

- **Route:** `GET /admin/v1/settings/health` under the admin guard →
  `settings.Service.Health()` which returns `health.Snapshot()` as JSON:
  `{ window, recent_fallbacks, last_error, last_fallback_at, degraded }`.

### Frontend

- `lib/admin.ts`: `getAdminHealth(token) => HealthSnapshot`.
- `app/admin/page.tsx`: poll health on load and every ~30s. When `degraded`,
  render a red banner at the top of the page:
  > ⚠️ **AI is in scripted fallback mode.** Recent model calls failed
  > (`recent_fallbacks` of the last `window`). Last error: `last_error`.
  > Check the model settings below and use **Test connection**.

  When `degraded` is false, no banner.

## Data Flow

```
Buyer RFQ chat ── intakeFor ─▶ BuildIntake(cfg, logger, health)
                                   │  APIKey=="" ─▶ HeuristicIntake (no record)
                                   └▶ llmIntake ──▶ model 200 ─▶ RecordSuccess()
                                                └▶ non-200/parse ─▶ RecordFallback(err) ─▶ heuristic
Admin page ── GET /admin/v1/settings/health ─▶ health.Snapshot() ─▶ banner if degraded
Admin page ── POST /admin/v1/settings/test  ─▶ TestConnection ─▶ PingProvider(saved cfg)
```

## Error Handling

- `/admin/v1/settings/test`: provider failures are reported in the `TestResult`
  body with HTTP 200; only guard/auth failures are non-200 (handled by middleware).
- `/admin/v1/settings/health`: pure read of in-memory state; cannot fail beyond auth.
- Tracker is best-effort telemetry: a nil tracker must be tolerated (builders and
  record sites guard `if health != nil`) so tests and any future caller that omits
  it don't panic.

## Testing

- **`HealthTracker`** (TDD, table tests): empty snapshot (`degraded=false`);
  fallback then success clears `degraded` but keeps `recent_fallbacks` until it ages
  out of the ring; ring eviction after >N outcomes; concurrent access (race test).
- **`PingProvider`**: no-key path returns the "no API key configured" error without
  a network call.
- **Live verification** (manual, per `verify` skill): drive `POST /admin/v1/settings/test`
  against the corrected `qwen3.7-plus` (expect ok) and against a bogus model (expect
  the 404 message); confirm the banner appears after forcing a fallback and clears
  after a good intake.

## Files Touched

- `internal/rfq/health.go` (new) + `internal/rfq/health_test.go` (new)
- `internal/rfq/intake_llm.go` (record sites; `httpStatusError` carries code+snippet)
- `internal/rfq/intake.go` + `internal/rfq/extractor.go` (`BuildIntake`/`BuildExtractor`
  gain a `*HealthTracker` param; extractor record sites)
- `internal/rfq/service.go` (hold + pass the tracker)
- `internal/settings/service.go` (hold tracker; `TestConnection`, `Health`)
- `internal/settings/handler.go` (`POST /test`, `GET /health` routes)
- `internal/httpapi/router.go` (create tracker, inject into both services)
- `web/lib/admin.ts` (`testAdminSettings`, `getAdminHealth`, types)
- `web/app/admin/page.tsx` (Test button + result; health poll + banner)
- Callers of `BuildIntake`/`BuildExtractor` in existing tests updated for the new param.

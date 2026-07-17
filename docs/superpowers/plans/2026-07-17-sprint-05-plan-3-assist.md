# Sprint 5 · Plan 3 — Buyer AI Assistant + Metering Implementation Plan

> **For agentic workers:** TDD, commit per task. Follow `internal/entitlements/usage.go` (quota/paywall),
> `internal/rfq/claude.go` (anthropic-sdk-go usage), `internal/trust` (service+handler split).

**Goal:** A buyer-side AI assistant with two features — sharpen an RFQ draft, and explain a quote
comparison — running on Haiku, **metered** via the entitlements engine (Free capped, Pro/Business
unlimited).

**Architecture:** New `internal/assist` package: a `Service` that calls Anthropic Haiku via the official
SDK (deterministic stub when no key) and a `Handler` with two endpoints, each gated by a new `assist`
usage quota. Entitlements gains an `AssistMonthlyLimit`. Migration 024 adds the plan column.

**Tech Stack:** Go, Fiber, sqlc, `github.com/anthropics/anthropic-sdk-go`.

## Global Constraints

- Merge to `master` LOCALLY ONLY, never push.
- DB tests → `tradebridge_core_test` only (derive URL secret-free as in Plan 1). Migration 024 builds on ≤023.
- `sqlc generate` after query/schema-affecting edits; `go build ./...` clean.
- The assist LLM call must degrade to a **deterministic stub** when no API key is configured, so tests and
  keyless dev work without network. Model pinned to Haiku: `claude-haiku-4-5-20251001`.
- Metering mirrors the RFQ pattern exactly (`usage_counters`, per-month period, `PaywallError`).

## File Structure

- Create: `pkg/db/migrations/024_assist_limit.up.sql`, `024_assist_limit.down.sql`
- Modify: `internal/entitlements/entitlements.go` (AssistMonthlyLimit in struct + For + freeFallback)
- Modify: `internal/entitlements/usage.go` (metricAssist, CheckAssistQuota, IncrementAssistUsage, planRequiredFor case)
- Create: `internal/assist/service.go` (Haiku client + stub), `internal/assist/handler.go`
- Create: `internal/assist/service_test.go` (stub output), `internal/assist/handler_test.go` (metering/paywall)
- Modify: `internal/httpapi/router.go` (construct + register the assist handler)

## Migration 024

**`024_assist_limit.up.sql`:**
```sql
-- Buyer AI assist metering: Free is capped, paid plans unlimited (-1).
ALTER TABLE plans ADD COLUMN assist_monthly_limit INTEGER NOT NULL DEFAULT 20;
UPDATE plans SET assist_monthly_limit = -1 WHERE code IN ('pro', 'business');
-- Free (code='free') keeps the default 20.
```

**`024_assist_limit.down.sql`:**
```sql
ALTER TABLE plans DROP COLUMN IF EXISTS assist_monthly_limit;
```
Run `sqlc generate` (the `plans` query `SELECT *` picks up the new column → `sqlc.Plan.AssistMonthlyLimit int32`).

## Entitlements changes

`internal/entitlements/entitlements.go`:
- Add `AssistMonthlyLimit int \`json:"assist_monthly_limit"\`` to `Entitlements`.
- In `For()`, map `AssistMonthlyLimit: int(plan.AssistMonthlyLimit)`.
- `freeFallback`: add `AssistMonthlyLimit: 20`.

`internal/entitlements/usage.go`:
- `const metricAssist = "assist"`.
- In `planRequiredFor`, add case `"assist_monthly_limit": lim = p.AssistMonthlyLimit`.
- Add (mirror `rfqUsed`/`CheckRFQQuota`/`IncrementRFQUsage`):
```go
func (s *Service) assistUsed(ctx context.Context, companyID string) int { /* GetUsageCounter metricAssist */ }
func (s *Service) CheckAssistQuota(ctx context.Context, companyID string) error {
	limit := s.For(ctx, companyID).AssistMonthlyLimit
	if limit == -1 { return nil }
	used := s.assistUsed(ctx, companyID)
	if used >= limit {
		return &PaywallError{Feature: "assist_monthly_limit", PlanRequired: s.planRequiredFor(ctx, "assist_monthly_limit", used)}
	}
	return nil
}
func (s *Service) IncrementAssistUsage(ctx context.Context, companyID string) { /* IncrementUsageCounter metricAssist, best-effort */ }
```

---

### Task 1: Migration + entitlements metering

**Files:** migration 024, `entitlements.go`, `usage.go`; `internal/entitlements/usage_assist_test.go` (DB).

- [ ] **Step 1: Failing test** — seed a company on Free (no subscription → Free, limit 20). Call
  `IncrementAssistUsage` 20×, then `CheckAssistQuota` → returns a `*PaywallError` with
  `Feature=="assist_monthly_limit"`. A company with a `pro` subscription (limit -1) → `CheckAssistQuota`
  always nil even after increments. (Grant pro via `UpsertSubscription{Plan:"pro",Status:"active"}` as in Plan 1.)
- [ ] **Step 2: Run, verify fail** (compile: field/methods missing). **Step 3: apply migration + regen + implement. Step 4: pass.**
- [ ] **Step 5: Commit** — `feat(entitlements): assist monthly quota (Free capped, paid unlimited)`.

---

### Task 2: assist.Service (Haiku + deterministic stub)

**Files:** `internal/assist/service.go`, `internal/assist/service_test.go`.

**Interfaces (Produces):**
```go
const haikuModel = "claude-haiku-4-5-20251001"

type Service struct { apiKey, model string; logger zerolog.Logger }
func NewService(apiKey string, logger zerolog.Logger) *Service {
	return &Service{apiKey: apiKey, model: haikuModel, logger: logger}
}

type QuoteSummary struct { SellerName string; PriceCents int64; Currency string; DeliveryDays *int32; ReviewCount int64; AvgRating float64; Verified bool }

// ImproveRFQ returns a sharpened, clarified version of a buyer's RFQ draft.
func (s *Service) ImproveRFQ(ctx context.Context, draft string) (string, error)
// ExplainQuotes returns a plain-language comparison of the RFQ's quotes.
func (s *Service) ExplainQuotes(ctx context.Context, rfqTitle string, quotes []QuoteSummary) (string, error)
```

- When `apiKey == ""`: return a **deterministic stub** — `ImproveRFQ` echoes the draft wrapped with a
  checklist prompt (e.g. `"Suggested: <draft>. Consider adding quantity, deadline, and budget."`);
  `ExplainQuotes` returns a template summarizing the cheapest / fastest / best-rated quote from the input
  (pure Go, no LLM). Both must be non-empty and mention the inputs so tests can assert.
- When `apiKey != ""`: call `anthropic.NewClient(option.WithAPIKey(s.apiKey)).Messages.New(ctx,
  anthropic.MessageNewParams{Model: anthropic.Model(s.model), MaxTokens: 1024, System: [...], Messages: [...]})`
  (mirror `internal/rfq/claude.go`), extract the first text block; on any error, fall back to the stub
  (so a provider hiccup never hard-fails the assist call).

- [ ] **Step 1: Failing test** (no key → stub): `NewService("", nop).ImproveRFQ(ctx, "need bolts")` returns
  a non-empty string containing "bolts"; `ExplainQuotes(ctx, "Bolts", []QuoteSummary{cheap, pricey})`
  returns text naming the cheaper seller. (No network — stub path only.)
- [ ] **Step 2–4: fail → implement → pass.**
- [ ] **Step 5: Commit** — `feat(assist): Haiku assist service with deterministic stub fallback`.

---

### Task 3: assist handler (metered endpoints) + wiring

**Files:** `internal/assist/handler.go`, `internal/assist/handler_test.go`, `internal/httpapi/router.go`.

```go
type Handler struct { svc *Service; ent *entitlements.Service; q *sqlc.Queries }
func NewHandler(svc *Service, ent *entitlements.Service, q *sqlc.Queries) *Handler { ... }
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/assist", guard...)
	g.Post("/rfq", h.improveRFQ)
	g.Post("/compare", h.compare)
}
```
- `improveRFQ`: parse `{draft}`; `me := middleware.CompanyID(c)`; `if err := h.ent.CheckAssistQuota(ctx, me); err != nil { return entitlements.Fail(c, err) }`; `out, _ := h.svc.ImproveRFQ(ctx, draft)`;
  `h.ent.IncrementAssistUsage(ctx, me)`; `c.JSON(fiber.Map{"suggestion": out})`.
- `compare`: parse `{rfq_id}`; load RFQ (`GetRFQ`); **buyer-only** (`r.BuyerCompanyID == me` else 404);
  quota check; `RankQuotesByRFQ` → build `[]QuoteSummary`; `h.svc.ExplainQuotes(ctx, r.Title, summaries)`;
  increment; `c.JSON(fiber.Map{"explanation": out})`. Empty quote list → a friendly "no quotes yet" message
  (still counts against quota? NO — skip increment when there are no quotes to explain; return early 200).
- Router: `assistSvc := assist.NewService(cfg.AnthropicAPIKey, cfg.Logger)`;
  `assist.NewHandler(assistSvc, entSvc, q).Register(f, guard...)`.

- [ ] **Step 1: Failing handler test** (fake guard injecting a company id): a Free company can call
  `/v1/assist/rfq` and gets `200 {suggestion}` while under cap; after 20 increments (drive via the
  entitlements service on the same DB), the endpoint returns the paywall status (whatever
  `entitlements.Fail` maps a `PaywallError` to — check it; likely 402/403). `/v1/assist/compare` for a
  non-buyer on the RFQ → 404.
- [ ] **Step 2–4: fail → implement → pass.**
- [ ] **Step 5: Commit** — `feat(assist): metered RFQ-help + quote-compare endpoints + wiring`.

---

## Self-Review checklist

- `go build ./...` clean; full suite `go test ./... -p 1 -count=1` (migration 024 applied) — 0 failures.
- Migration 024 round-trips; Free plan `assist_monthly_limit=20`, pro/business=-1.
- Assist works with NO API key (deterministic stub) — no test hits the network.
- Quota enforced BEFORE the LLM call; usage incremented only on a served (non-empty) assist; unlimited (-1) never blocks.
- `/v1/assist/compare` is buyer-only for the RFQ (non-buyer → 404, no leak).
- No `%widgets%` supplier_profiles fixtures in new tests.

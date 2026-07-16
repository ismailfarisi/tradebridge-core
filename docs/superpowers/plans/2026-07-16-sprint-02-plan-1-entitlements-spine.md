# Sprint 2 — Plan 1: Entitlements Spine (Core) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the feature-gating spine — a seeded, admin-editable `plans` table, an `internal/entitlements` service that resolves a company → its plan → its entitlements + usage, and server-side quota gates on RFQ create / catalog insert / team invite — all fail-closed to Free, with **no Stripe dependency** (every company resolves Free until Plan 2 creates subscriptions).

**Architecture:** Plan values live in a `plans` DB table (source of truth, seeded with defaults, editable by the platform admin). A company's plan is derived from its active `subscriptions` row (none → Free). `internal/entitlements.Service` reads `plans` (cached), exposes `For`/`Usage`/quota checks, and renders a `402 {code,feature,plan_required}` paywall on over-limit. Gates are wired into the existing rfq/catalog/identity handlers.

**Tech Stack:** Go 1.26.4, Fiber v2, sqlc (pgx/v5), golang-migrate, Postgres 16. Mirrors `internal/notify` (service pattern), `internal/admin` (platform-admin auth), `pkg/middleware/verified.go` (structured-code gate).

## Global Constraints

- **Fail-closed to Free:** any resolution failure, missing subscription, or missing plan row ⇒ Free entitlements. Never grant a paid entitlement on error.
- **Paywall contract:** over-limit / not-entitled ⇒ **HTTP 402** with body exactly `{"code":"upgrade_required","feature":<string>,"plan_required":<string>}`. `feature` ∈ {`agent_autonomy`,`catalog_max_items`,`rfq_monthly_limit`,`seats`}. `plan_required` is the cheapest plan whose value clears the limit (`pro` for autonomy; otherwise `pro` then `business`).
- **Unlimited sentinel:** a limit of `-1` means unlimited — never blocks.
- **Plan codes:** exactly `free`, `pro`, `business`.
- **Seeded defaults:** Free `{autonomy:false, catalog:25, rfq:10, seats:2}`; Pro `{true, 500, 100, 5}`; Business `{true, -1, -1, -1}`.
- **sqlc regen:** after editing any `.sql`, run `make generate` (runs `sqlc generate` then `wire ./internal/di`) and commit the generated `pkg/db/sqlc/*` changes with the query.
- **Migrations:** every `*.up.sql` has a matching `*.down.sql` that reverses it exactly.
- **Tests:** integration tests live in `package integration` under `test/integration/`, gated by `TEST_DATABASE_URL` via `poolOrSkip(t)`. `TestMain` truncates `companies CASCADE` between the suite; the seeded `plans` table is reference data and is NOT truncated.
- **Local only:** commit to a feature branch; never push. Do not touch the live stack (ports 8080/3010); tests use the dev DB on `:5442` via `TEST_DATABASE_URL`.

## File Structure

- `pkg/db/migrations/016_billing.{up,down}.sql` — new tables (`plans`, `subscriptions`, `usage_counters`, `processed_stripe_events`) + `companies.stripe_customer_id`; seeds `plans`.
- `pkg/db/query/billing.sql` — queries for plans, subscriptions, usage counters, and the user/invite/catalog counts the gates need.
- `pkg/db/sqlc/*` — regenerated (do not hand-edit).
- `internal/entitlements/entitlements.go` — `Service`, types, plan resolution + cache.
- `internal/entitlements/usage.go` — usage metering + quota checks + `PaywallError` + `Fail`.
- `internal/entitlements/handler.go` — `GET /v1/billing/summary`.
- `internal/entitlements/admin.go` — `GET/PATCH /admin/v1/plans` (platform-admin).
- `internal/httpapi/router.go` — construct the service, pass it to rfq/catalog/identity, register the summary + admin-plans handlers.
- `internal/rfq/handler.go`, `internal/catalog/handler.go`, `internal/identity/handler.go` — accept the service, add gates.
- `test/integration/entitlements_test.go`, `test/integration/api_billing_summary_test.go`, `test/integration/api_admin_plans_test.go`, `test/integration/api_entitlement_gates_test.go` — tests.

---

## Task 1: Migration 016 + billing queries + sqlc regen

**Files:**
- Create: `pkg/db/migrations/016_billing.up.sql`, `pkg/db/migrations/016_billing.down.sql`
- Create: `pkg/db/query/billing.sql`
- Modify (generated): `pkg/db/sqlc/*`
- Test: `test/integration/entitlements_test.go` (first test only)

**Interfaces:**
- Produces (sqlc): `q.ListPlans(ctx) ([]Plan, error)`, `q.GetPlan(ctx, code string) (Plan, error)`, `q.UpdatePlanLimits(ctx, UpdatePlanLimitsParams) (Plan, error)`, `q.GetActiveSubscription(ctx, companyID string) (Subscription, error)`, `q.GetUsageCounter(ctx, GetUsageCounterParams) (int64, error)`, `q.IncrementUsageCounter(ctx, IncrementUsageCounterParams) (int64, error)`, `q.CountCatalogByCompany(ctx, companyID string) (int64, error)`, `q.CountUsersByCompany(ctx, companyID string) (int64, error)`, `q.CountPendingInvitesByCompany(ctx, companyID string) (int64, error)`. `Plan` has fields `Code, Name string; AgentAutonomy bool; CatalogMaxItems, RfqMonthlyLimit, Seats int32; UpdatedAt time.Time`.

- [ ] **Step 1: Write the failing test**

Create `test/integration/entitlements_test.go`:

```go
package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestMigration016_PlansSeeded(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)

	plans, err := q.ListPlans(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 3)

	by := map[string]sqlc.Plan{}
	for _, p := range plans {
		by[p.Code] = p
	}

	require.False(t, by["free"].AgentAutonomy)
	require.Equal(t, int32(25), by["free"].CatalogMaxItems)
	require.Equal(t, int32(10), by["free"].RfqMonthlyLimit)
	require.Equal(t, int32(2), by["free"].Seats)

	require.True(t, by["pro"].AgentAutonomy)
	require.Equal(t, int32(500), by["pro"].CatalogMaxItems)
	require.Equal(t, int32(100), by["pro"].RfqMonthlyLimit)
	require.Equal(t, int32(5), by["pro"].Seats)

	require.True(t, by["business"].AgentAutonomy)
	require.Equal(t, int32(-1), by["business"].CatalogMaxItems)
	require.Equal(t, int32(-1), by["business"].RfqMonthlyLimit)
	require.Equal(t, int32(-1), by["business"].Seats)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go build ./...`
Expected: FAIL — `q.ListPlans` undefined (sqlc not generated yet).

- [ ] **Step 3: Write the migration**

Create `pkg/db/migrations/016_billing.up.sql`:

```sql
-- Sprint 2 — Billing & Entitlements.
--
-- plans: the source of truth for per-plan entitlement values. Seeded with
-- defaults and editable by the platform admin (no deploy needed to tune limits).
-- A limit of -1 means unlimited.
CREATE TABLE plans (
    code              TEXT PRIMARY KEY CHECK (code IN ('free', 'pro', 'business')),
    name              TEXT NOT NULL,
    agent_autonomy    BOOLEAN NOT NULL,
    catalog_max_items INTEGER NOT NULL,
    rfq_monthly_limit INTEGER NOT NULL,
    seats             INTEGER NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO plans (code, name, agent_autonomy, catalog_max_items, rfq_monthly_limit, seats) VALUES
    ('free',     'Free',     FALSE, 25,  10,  2),
    ('pro',      'Pro',      TRUE,  500, 100, 5),
    ('business', 'Business', TRUE,  -1,  -1,  -1);

-- subscriptions: one row per company (its current plan). No row => Free.
-- Populated by Stripe webhooks in Plan 2; nothing writes here in Plan 1.
CREATE TABLE subscriptions (
    id                   TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id           TEXT NOT NULL UNIQUE REFERENCES companies(id),
    stripe_customer_id   TEXT,
    stripe_subscription_id TEXT,
    plan                 TEXT NOT NULL REFERENCES plans(code),
    interval             TEXT NOT NULL DEFAULT 'month' CHECK (interval IN ('month', 'year')),
    status               TEXT NOT NULL CHECK (status IN ('trialing','active','past_due','canceled','unpaid','incomplete')),
    current_period_end   TIMESTAMPTZ,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- usage_counters: incrementing counters for metered limits (currently rfq/month).
-- period is 'YYYY-MM' (UTC). Live counts (catalog, seats) are COUNT queries, not here.
CREATE TABLE usage_counters (
    company_id TEXT NOT NULL REFERENCES companies(id),
    metric     TEXT NOT NULL,
    period     TEXT NOT NULL,
    count      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (company_id, metric, period)
);

-- processed_stripe_events: webhook idempotency ledger (Plan 2 writes here).
CREATE TABLE processed_stripe_events (
    event_id     TEXT PRIMARY KEY,
    type         TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- companies gain a Stripe customer id (set on first checkout in Plan 2).
ALTER TABLE companies ADD COLUMN stripe_customer_id TEXT;
```

Create `pkg/db/migrations/016_billing.down.sql`:

```sql
ALTER TABLE companies DROP COLUMN IF EXISTS stripe_customer_id;
DROP TABLE IF EXISTS processed_stripe_events;
DROP TABLE IF EXISTS usage_counters;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS plans;
```

- [ ] **Step 4: Write the queries**

Create `pkg/db/query/billing.sql`:

```sql
-- name: ListPlans :many
SELECT * FROM plans ORDER BY
    CASE code WHEN 'free' THEN 0 WHEN 'pro' THEN 1 WHEN 'business' THEN 2 ELSE 3 END;

-- name: GetPlan :one
SELECT * FROM plans WHERE code = $1;

-- name: UpdatePlanLimits :one
UPDATE plans
SET agent_autonomy    = $2,
    catalog_max_items = $3,
    rfq_monthly_limit = $4,
    seats             = $5,
    updated_at        = NOW()
WHERE code = $1
RETURNING *;

-- name: GetActiveSubscription :one
SELECT * FROM subscriptions
WHERE company_id = $1
  AND status IN ('trialing', 'active', 'past_due');

-- name: GetUsageCounter :one
SELECT count FROM usage_counters
WHERE company_id = $1 AND metric = $2 AND period = $3;

-- name: IncrementUsageCounter :one
INSERT INTO usage_counters (company_id, metric, period, count)
VALUES ($1, $2, $3, 1)
ON CONFLICT (company_id, metric, period) DO UPDATE
SET count = usage_counters.count + 1
RETURNING count;

-- name: CountCatalogByCompany :one
SELECT COUNT(*) FROM catalog_summaries WHERE company_id = $1;

-- name: CountUsersByCompany :one
SELECT COUNT(*) FROM users WHERE company_id = $1;

-- name: CountPendingInvitesByCompany :one
SELECT COUNT(*) FROM user_invites
WHERE company_id = $1 AND accepted_at IS NULL AND expires_at > NOW();
```

- [ ] **Step 5: Apply the migration and regenerate**

Run:
```bash
make migrate      # applies 016 against the dev DB ($DB_URL, :5442)
make generate     # sqlc generate + wire
```
Expected: migrate reports `016/u billing`; `sqlc generate` succeeds; `pkg/db/sqlc/` gains `Plan`, `Subscription`, `UsageCounter`, `ProcessedStripeEvent` models and the new query methods.

- [ ] **Step 6: Run the test to verify it passes**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestMigration016_PlansSeeded -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/db/migrations/016_billing.up.sql pkg/db/migrations/016_billing.down.sql pkg/db/query/billing.sql pkg/db/sqlc/ test/integration/entitlements_test.go
git commit -m "feat(entitlements): migration 016 + billing queries (plans seeded)"
```

---

## Task 2: Entitlements service — plan resolution + cache

**Files:**
- Create: `internal/entitlements/entitlements.go`
- Test: `test/integration/entitlements_test.go` (add tests)

**Interfaces:**
- Consumes: `q.GetActiveSubscription`, `q.GetPlan` (Task 1).
- Produces:
  - `type Entitlements struct { AgentAutonomy bool; CatalogMaxItems, RFQMonthlyLimit, Seats int }` (JSON tags `agent_autonomy`, `catalog_max_items`, `rfq_monthly_limit`, `seats`).
  - `func NewService(q *sqlc.Queries, logger zerolog.Logger) *Service`
  - `func (s *Service) PlanCode(ctx context.Context, companyID string) string` — active sub's plan, else `"free"`; fail-closed to `"free"` on any error.
  - `func (s *Service) For(ctx context.Context, companyID string) Entitlements` — resolves plan → cached plan row → entitlement values; Free defaults on any error.
  - `func (s *Service) InvalidateCache()` — clears the plan cache (called after an admin edit).

- [ ] **Step 1: Write the failing test**

Add to `test/integration/entitlements_test.go`:

```go
func TestEntitlements_FreeByDefault(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	svc := entitlements.NewService(q, zerolog.Nop())
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "EntCo", Type: "both"})
	require.NoError(t, err)

	require.Equal(t, "free", svc.PlanCode(ctx, co.ID))

	ent := svc.For(ctx, co.ID)
	require.False(t, ent.AgentAutonomy)
	require.Equal(t, 25, ent.CatalogMaxItems)
	require.Equal(t, 10, ent.RFQMonthlyLimit)
	require.Equal(t, 2, ent.Seats)
}

func TestEntitlements_UnknownCompanyIsFree(t *testing.T) {
	pool := poolOrSkip(t)
	svc := entitlements.NewService(sqlc.New(pool), zerolog.Nop())
	ent := svc.For(context.Background(), "01NONEXISTENT0000000000000")
	require.Equal(t, 25, ent.CatalogMaxItems) // fail-closed to Free
	require.False(t, ent.AgentAutonomy)
}
```

Add imports `"github.com/rs/zerolog"` and `"github.com/tradebridge/core/internal/entitlements"` to the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go build ./...`
Expected: FAIL — package `entitlements` does not exist.

- [ ] **Step 3: Write the service**

Create `internal/entitlements/entitlements.go`:

```go
// Package entitlements resolves a company's plan and the feature limits that
// plan grants, and meters usage against those limits. Plan values live in the
// `plans` table (admin-editable); a company's plan comes from its active
// subscription row, defaulting to Free. Everything fails closed to Free.
package entitlements

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Entitlements is the set of feature limits a plan grants. A limit of -1 means
// unlimited.
type Entitlements struct {
	AgentAutonomy   bool `json:"agent_autonomy"`
	CatalogMaxItems int  `json:"catalog_max_items"`
	RFQMonthlyLimit int  `json:"rfq_monthly_limit"`
	Seats           int  `json:"seats"`
}

// freeFallback is returned whenever resolution fails, so callers never receive
// a paid entitlement on error.
var freeFallback = Entitlements{AgentAutonomy: false, CatalogMaxItems: 25, RFQMonthlyLimit: 10, Seats: 2}

// Service resolves plans and entitlements over the query layer, caching plan
// rows in-process (invalidated when the admin edits a plan).
type Service struct {
	q      *sqlc.Queries
	logger zerolog.Logger

	mu    sync.RWMutex
	cache map[string]sqlc.Plan
}

// NewService builds the entitlements service.
func NewService(q *sqlc.Queries, logger zerolog.Logger) *Service {
	return &Service{q: q, logger: logger, cache: map[string]sqlc.Plan{}}
}

// InvalidateCache drops all cached plan rows. Call after a plan edit.
func (s *Service) InvalidateCache() {
	s.mu.Lock()
	s.cache = map[string]sqlc.Plan{}
	s.mu.Unlock()
}

// PlanCode returns the company's current plan code, or "free" when it has no
// active subscription. Fail-closed to "free" on any error.
func (s *Service) PlanCode(ctx context.Context, companyID string) string {
	sub, err := s.q.GetActiveSubscription(ctx, companyID)
	if err != nil {
		return "free" // includes pgx.ErrNoRows (the common case) — no active sub => Free
	}
	return sub.Plan
}

// For resolves the company's entitlements. Free defaults on any error.
func (s *Service) For(ctx context.Context, companyID string) Entitlements {
	plan, err := s.plan(ctx, s.PlanCode(ctx, companyID))
	if err != nil {
		s.logger.Warn().Err(err).Str("company", companyID).Msg("entitlements: plan lookup failed, defaulting to Free")
		return freeFallback
	}
	return Entitlements{
		AgentAutonomy:   plan.AgentAutonomy,
		CatalogMaxItems: int(plan.CatalogMaxItems),
		RFQMonthlyLimit: int(plan.RfqMonthlyLimit),
		Seats:           int(plan.Seats),
	}
}

// plan returns a plan row, using the cache when warm.
func (s *Service) plan(ctx context.Context, code string) (sqlc.Plan, error) {
	s.mu.RLock()
	p, ok := s.cache[code]
	s.mu.RUnlock()
	if ok {
		return p, nil
	}
	p, err := s.q.GetPlan(ctx, code)
	if err != nil {
		return sqlc.Plan{}, err
	}
	s.mu.Lock()
	s.cache[code] = p
	s.mu.Unlock()
	return p, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestEntitlements -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add internal/entitlements/entitlements.go test/integration/entitlements_test.go
git commit -m "feat(entitlements): plan resolution + cached entitlements (fail-closed to Free)"
```

---

## Task 3: Usage metering + quota checks + paywall

**Files:**
- Create: `internal/entitlements/usage.go`
- Test: `test/integration/entitlements_test.go` (add tests)

**Interfaces:**
- Consumes: `q.GetUsageCounter`, `q.IncrementUsageCounter`, `q.CountCatalogByCompany`, `q.CountUsersByCompany`, `q.CountPendingInvitesByCompany` (Task 1); `Service.For` (Task 2).
- Produces:
  - `type PaywallError struct { Feature, PlanRequired string }` implementing `error`.
  - `type Usage struct { RFQUsed, RFQLimit, CatalogUsed, CatalogLimit, SeatsUsed, SeatsLimit int }`
  - `func (s *Service) Usage(ctx, companyID) Usage`
  - `func (s *Service) CheckRFQQuota(ctx, companyID) error` / `func (s *Service) IncrementRFQUsage(ctx, companyID)`
  - `func (s *Service) CheckCatalogQuota(ctx, companyID) error`
  - `func (s *Service) CheckSeatQuota(ctx, companyID) error`
  - `func Fail(c *fiber.Ctx, err error) error` — renders 402 for a `*PaywallError`, else returns `err` unchanged.

- [ ] **Step 1: Write the failing test**

Add to `test/integration/entitlements_test.go`:

```go
func TestEntitlements_QuotaChecks(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	svc := entitlements.NewService(q, zerolog.Nop())
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "QuotaCo", Type: "both"})
	require.NoError(t, err)

	// Free rfq limit is 10. Under the cap: no error.
	require.NoError(t, svc.CheckRFQQuota(ctx, co.ID))

	// Increment to the cap; the 11th check is a paywall.
	for i := 0; i < 10; i++ {
		svc.IncrementRFQUsage(ctx, co.ID)
	}
	err = svc.CheckRFQQuota(ctx, co.ID)
	var pw *entitlements.PaywallError
	require.ErrorAs(t, err, &pw)
	require.Equal(t, "rfq_monthly_limit", pw.Feature)
	require.Equal(t, "pro", pw.PlanRequired)

	// Usage reflects the counter.
	u := svc.Usage(ctx, co.ID)
	require.Equal(t, 10, u.RFQUsed)
	require.Equal(t, 10, u.RFQLimit)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go build ./...`
Expected: FAIL — `CheckRFQQuota`, `PaywallError`, `IncrementRFQUsage`, `Usage` undefined.

- [ ] **Step 3: Write the usage + paywall code**

Create `internal/entitlements/usage.go`:

```go
package entitlements

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
)

const metricRFQ = "rfq"

// PaywallError signals a request blocked by an entitlement limit. Fail renders
// it as HTTP 402.
type PaywallError struct {
	Feature      string
	PlanRequired string
}

func (e *PaywallError) Error() string {
	return "upgrade required: " + e.Feature + " (needs " + e.PlanRequired + ")"
}

// Fail renders a *PaywallError as a 402 with the paywall contract body; any
// other error is returned unchanged for the default error handler.
func Fail(c *fiber.Ctx, err error) error {
	var pw *PaywallError
	if errors.As(err, &pw) {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"code":          "upgrade_required",
			"feature":       pw.Feature,
			"plan_required": pw.PlanRequired,
		})
	}
	return err
}

// Usage is the current usage-vs-limit snapshot for a company.
type Usage struct {
	RFQUsed      int `json:"rfq_used"`
	RFQLimit     int `json:"rfq_limit"`
	CatalogUsed  int `json:"catalog_used"`
	CatalogLimit int `json:"catalog_limit"`
	SeatsUsed    int `json:"seats_used"`
	SeatsLimit   int `json:"seats_limit"`
}

// period is the current 'YYYY-MM' usage bucket in UTC.
func period() string { return time.Now().UTC().Format("2006-01") }

// planRequiredFor returns the cheapest plan whose limit for `feature` clears
// `used`. Boolean features (autonomy) always need Pro. For numeric limits, Pro
// is tried first, then Business.
func (s *Service) planRequiredFor(ctx context.Context, feature string, used int) string {
	for _, code := range []string{"pro", "business"} {
		p, err := s.plan(ctx, code)
		if err != nil {
			continue
		}
		var lim int32
		switch feature {
		case "catalog_max_items":
			lim = p.CatalogMaxItems
		case metricRFQ, "rfq_monthly_limit":
			lim = p.RfqMonthlyLimit
		case "seats":
			lim = p.Seats
		case "agent_autonomy":
			if p.AgentAutonomy {
				return code
			}
			continue
		}
		if lim == -1 || int(lim) > used {
			return code
		}
	}
	return "business"
}

// rfqUsed reads the current-period RFQ counter (0 when absent).
func (s *Service) rfqUsed(ctx context.Context, companyID string) int {
	n, err := s.q.GetUsageCounter(ctx, sqlc.GetUsageCounterParams{
		CompanyID: companyID, Metric: metricRFQ, Period: period(),
	})
	if err != nil {
		return 0 // pgx.ErrNoRows => no usage this period
	}
	return int(n)
}

// CheckRFQQuota returns a *PaywallError when the company is at/over its monthly
// RFQ cap. Unlimited (-1) never blocks.
func (s *Service) CheckRFQQuota(ctx context.Context, companyID string) error {
	limit := s.For(ctx, companyID).RFQMonthlyLimit
	if limit == -1 {
		return nil
	}
	if s.rfqUsed(ctx, companyID) >= limit {
		return &PaywallError{Feature: "rfq_monthly_limit", PlanRequired: s.planRequiredFor(ctx, "rfq_monthly_limit", limit)}
	}
	return nil
}

// IncrementRFQUsage bumps the current-period RFQ counter. Best-effort: a
// failure is logged, not surfaced (the RFQ was already created).
func (s *Service) IncrementRFQUsage(ctx context.Context, companyID string) {
	if _, err := s.q.IncrementUsageCounter(ctx, sqlc.IncrementUsageCounterParams{
		CompanyID: companyID, Metric: metricRFQ, Period: period(),
	}); err != nil {
		s.logger.Warn().Err(err).Str("company", companyID).Msg("entitlements: rfq usage increment failed")
	}
}

// CheckCatalogQuota returns a *PaywallError when adding one more catalog item
// would exceed the plan's cap.
func (s *Service) CheckCatalogQuota(ctx context.Context, companyID string) error {
	limit := s.For(ctx, companyID).CatalogMaxItems
	if limit == -1 {
		return nil
	}
	n, err := s.q.CountCatalogByCompany(ctx, companyID)
	if err != nil {
		return err
	}
	if int(n) >= limit {
		return &PaywallError{Feature: "catalog_max_items", PlanRequired: s.planRequiredFor(ctx, "catalog_max_items", limit)}
	}
	return nil
}

// CheckSeatQuota returns a *PaywallError when the company has no free seat.
// A seat is consumed by an existing user OR a pending (unaccepted) invite.
func (s *Service) CheckSeatQuota(ctx context.Context, companyID string) error {
	limit := s.For(ctx, companyID).Seats
	if limit == -1 {
		return nil
	}
	used, err := s.seatsUsed(ctx, companyID)
	if err != nil {
		return err
	}
	if used >= limit {
		return &PaywallError{Feature: "seats", PlanRequired: s.planRequiredFor(ctx, "seats", limit)}
	}
	return nil
}

func (s *Service) seatsUsed(ctx context.Context, companyID string) (int, error) {
	users, err := s.q.CountUsersByCompany(ctx, companyID)
	if err != nil {
		return 0, err
	}
	pending, err := s.q.CountPendingInvitesByCompany(ctx, companyID)
	if err != nil {
		return 0, err
	}
	return int(users) + int(pending), nil
}

// Usage returns the current usage-vs-limit snapshot. Counts that fail resolve
// to 0 used (fail-open on the read, not the gate).
func (s *Service) Usage(ctx context.Context, companyID string) Usage {
	ent := s.For(ctx, companyID)
	cat, _ := s.q.CountCatalogByCompany(ctx, companyID)
	seats, _ := s.seatsUsed(ctx, companyID)
	return Usage{
		RFQUsed:      s.rfqUsed(ctx, companyID),
		RFQLimit:     ent.RFQMonthlyLimit,
		CatalogUsed:  int(cat),
		CatalogLimit: ent.CatalogMaxItems,
		SeatsUsed:    seats,
		SeatsLimit:   ent.Seats,
	}
}
```

Note: the file references `sqlc` and `pgx` — add `"github.com/tradebridge/core/pkg/db/sqlc"` to the import block (and keep `pgx` only if referenced; it is not directly referenced here, so DO NOT import it — remove the `pgx` import line if `go build` reports it unused). Verify with `go build`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestEntitlements_QuotaChecks -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/entitlements/usage.go test/integration/entitlements_test.go
git commit -m "feat(entitlements): usage metering + quota checks + 402 paywall"
```

---

## Task 4: `GET /v1/billing/summary`

**Files:**
- Create: `internal/entitlements/handler.go`
- Modify: `internal/httpapi/router.go` (construct service, register handler)
- Test: `test/integration/api_billing_summary_test.go`

**Interfaces:**
- Consumes: `Service.For`, `Service.PlanCode`, `Service.Usage`.
- Produces: `func NewHandler(svc *Service) *Handler`, `func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler)` mounting `GET /v1/billing/summary` → `{plan, entitlements, usage}`.

- [ ] **Step 1: Write the failing test**

Create `test/integration/api_billing_summary_test.go`:

```go
package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingSummary_FreeDefaults(t *testing.T) {
	app := buildAPI(t)

	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "SummaryCo", "company_type": "both",
		"email": uniqueEmail("sum-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := reg["token"].(string)

	st, out := doJSON(t, app, "GET", "/v1/billing/summary", token, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "free", out["plan"])

	ent := out["entitlements"].(map[string]any)
	require.Equal(t, false, ent["agent_autonomy"])
	require.Equal(t, float64(25), ent["catalog_max_items"])

	usage := out["usage"].(map[string]any)
	require.Equal(t, float64(0), usage["rfq_used"])
	require.Equal(t, float64(10), usage["rfq_limit"])
	require.Equal(t, float64(1), usage["seats_used"]) // the owner
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestBillingSummary_FreeDefaults -v`
Expected: FAIL — 404 (route not mounted).

- [ ] **Step 3: Write the handler**

Create `internal/entitlements/handler.go`:

```go
package entitlements

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the buyer app's billing/entitlements read model.
type Handler struct {
	svc *Service
}

// NewHandler builds the billing summary handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts GET /v1/billing/summary under the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/billing", guard...)
	g.Get("/summary", h.summary)
}

// summary returns the caller company's plan, entitlements, and usage.
func (h *Handler) summary(c *fiber.Ctx) error {
	companyID := middleware.CompanyID(c)
	return c.JSON(fiber.Map{
		"plan":         h.svc.PlanCode(c.Context(), companyID),
		"entitlements": h.svc.For(c.Context(), companyID),
		"usage":        h.svc.Usage(c.Context(), companyID),
	})
}
```

- [ ] **Step 4: Wire the service into the router**

In `internal/httpapi/router.go`, add the import `"github.com/tradebridge/core/internal/entitlements"`. After `notifier := notify.New(...)` (~line 129) construct the service:

```go
	entSvc := entitlements.NewService(q, cfg.Logger)
```

After the `notify.NewHandler(q).Register(f, guard...)` line (~line 136), register the summary handler:

```go
	entitlements.NewHandler(entSvc).Register(f, guard...)
```

(Leave `entSvc` used by Tasks 5 and 6 as well.)

- [ ] **Step 5: Run the test to verify it passes**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestBillingSummary_FreeDefaults -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/entitlements/handler.go internal/httpapi/router.go test/integration/api_billing_summary_test.go
git commit -m "feat(entitlements): GET /v1/billing/summary"
```

---

## Task 5: Platform-admin plan editing (`GET/PATCH /admin/v1/plans`)

**Files:**
- Create: `internal/entitlements/admin.go`
- Modify: `internal/httpapi/router.go` (register inside the admin-guard block)
- Test: `test/integration/api_admin_plans_test.go`

**Interfaces:**
- Consumes: `q.ListPlans`, `q.UpdatePlanLimits`; `Service.InvalidateCache`.
- Produces: `func NewAdminHandler(q *sqlc.Queries, svc *Service) *AdminHandler`, `func (h *AdminHandler) Register(app *fiber.App, guard ...fiber.Handler)` mounting `GET /admin/v1/plans` and `PATCH /admin/v1/plans/:code`.

Global-constraint note for the reviewer: `PATCH` must call `svc.InvalidateCache()` after a successful update, or admin edits won't take effect until process restart.

- [ ] **Step 1: Write the failing test**

Create `test/integration/api_admin_plans_test.go`:

```go
package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminPlans_ListAndPatch(t *testing.T) {
	const email, pass = "plan-admin@test.local", "admin-supersecret"
	app := buildAPIWithAdmin(t, email, pass)

	st, login := doJSON(t, app, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": pass})
	require.Equal(t, http.StatusOK, st)
	adminTok := login["token"].(string)

	st, list := doJSON(t, app, "GET", "/admin/v1/plans", adminTok, nil)
	require.Equal(t, http.StatusOK, st)
	require.Len(t, list["plans"].([]any), 3)

	// Lower Free's RFQ limit to 1 and confirm it round-trips.
	st, patched := doJSON(t, app, "PATCH", "/admin/v1/plans/free", adminTok, map[string]any{
		"agent_autonomy": false, "catalog_max_items": 1, "rfq_monthly_limit": 1, "seats": 1,
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, float64(1), patched["rfq_monthly_limit"])

	// A company now sees the edited limit (cache was invalidated).
	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AdminSeeCo", "company_type": "both",
		"email": uniqueEmail("asee-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	st, sum := doJSON(t, app, "GET", "/v1/billing/summary", reg["token"].(string), nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, float64(1), sum["entitlements"].(map[string]any)["rfq_monthly_limit"])

	// Restore defaults so other tests are unaffected.
	doJSON(t, app, "PATCH", "/admin/v1/plans/free", adminTok, map[string]any{
		"agent_autonomy": false, "catalog_max_items": 25, "rfq_monthly_limit": 10, "seats": 2,
	})
}
```

Reviewer note: this test both lists/patches AND asserts cache invalidation via the summary read — the summary handler and admin handler must share one `*Service` instance (they do: both built from `entSvc` in the router).

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestAdminPlans_ListAndPatch -v`
Expected: FAIL — 404 (routes not mounted).

- [ ] **Step 3: Write the admin handler**

Create `internal/entitlements/admin.go`:

```go
package entitlements

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// AdminHandler serves the platform-admin plan-limit editor.
type AdminHandler struct {
	q   *sqlc.Queries
	svc *Service
}

// NewAdminHandler builds the admin plan handler.
func NewAdminHandler(q *sqlc.Queries, svc *Service) *AdminHandler {
	return &AdminHandler{q: q, svc: svc}
}

// Register mounts the plan routes behind the caller-supplied admin guard.
func (h *AdminHandler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/admin/v1/plans", guard...)
	g.Get("/", h.list)
	g.Patch("/:code", h.patch)
}

type planView struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	AgentAutonomy   bool   `json:"agent_autonomy"`
	CatalogMaxItems int32  `json:"catalog_max_items"`
	RFQMonthlyLimit int32  `json:"rfq_monthly_limit"`
	Seats           int32  `json:"seats"`
}

func toPlanView(p sqlc.Plan) planView {
	return planView{
		Code: p.Code, Name: p.Name, AgentAutonomy: p.AgentAutonomy,
		CatalogMaxItems: p.CatalogMaxItems, RFQMonthlyLimit: p.RfqMonthlyLimit, Seats: p.Seats,
	}
}

func (h *AdminHandler) list(c *fiber.Ctx) error {
	plans, err := h.q.ListPlans(c.Context())
	if err != nil {
		return err
	}
	out := make([]planView, len(plans))
	for i, p := range plans {
		out[i] = toPlanView(p)
	}
	return c.JSON(fiber.Map{"plans": out})
}

type patchPlanReq struct {
	AgentAutonomy   bool  `json:"agent_autonomy"`
	CatalogMaxItems int32 `json:"catalog_max_items"`
	RFQMonthlyLimit int32 `json:"rfq_monthly_limit"`
	Seats           int32 `json:"seats"`
}

func (h *AdminHandler) patch(c *fiber.Ctx) error {
	code := c.Params("code")
	if code != "free" && code != "pro" && code != "business" {
		return fiber.NewError(fiber.StatusBadRequest, "code must be free|pro|business")
	}
	var req patchPlanReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if err := validLimit(req.CatalogMaxItems); err != nil {
		return err
	}
	if err := validLimit(req.RFQMonthlyLimit); err != nil {
		return err
	}
	if err := validLimit(req.Seats); err != nil {
		return err
	}

	p, err := h.q.UpdatePlanLimits(c.Context(), sqlc.UpdatePlanLimitsParams{
		Code: code, AgentAutonomy: req.AgentAutonomy,
		CatalogMaxItems: req.CatalogMaxItems, RfqMonthlyLimit: req.RFQMonthlyLimit, Seats: req.Seats,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "plan not found")
	} else if err != nil {
		return err
	}
	h.svc.InvalidateCache() // edits take effect immediately, no restart
	return c.JSON(toPlanView(p))
}

// validLimit rejects values below -1 (only -1 or a non-negative cap are valid).
func validLimit(v int32) error {
	if v < -1 {
		return fiber.NewError(fiber.StatusBadRequest, "limit must be -1 (unlimited) or >= 0")
	}
	return nil
}
```

- [ ] **Step 4: Register inside the admin-guard block**

In `internal/httpapi/router.go`, inside the `if cfg.AdminJWTSecret != "" {` block (after `settings.NewHandler(settingsSvc).Register(f, adminGuard)`, ~line 187), add:

```go
		entitlements.NewAdminHandler(q, entSvc).Register(f, adminGuard)
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestAdminPlans_ListAndPatch -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/entitlements/admin.go internal/httpapi/router.go test/integration/api_admin_plans_test.go
git commit -m "feat(entitlements): platform-admin plan-limit editor (cache-invalidating)"
```

---

## Task 6: Wire quota gates into RFQ / catalog / invite

**Files:**
- Modify: `internal/rfq/handler.go` (gate `create`, increment on success)
- Modify: `internal/catalog/handler.go` (gate `create`)
- Modify: `internal/identity/handler.go` (gate `invite`)
- Modify: `internal/httpapi/router.go` (pass `entSvc` to the three handlers' `NewHandler`)
- Test: `test/integration/api_entitlement_gates_test.go`

**Interfaces:**
- Consumes: `Service.CheckRFQQuota`, `Service.IncrementRFQUsage`, `Service.CheckCatalogQuota`, `Service.CheckSeatQuota`, `Fail` (Task 3).
- The three `NewHandler` constructors gain a trailing `ent *entitlements.Service` parameter. Struct gains an `ent` field.

Reviewer note (global constraint): the RFQ gate must **check before create** and **increment only after a successful create**; a failed increment must not fail the request (best-effort, already logged inside `IncrementRFQUsage`).

- [ ] **Step 1: Write the failing test**

Create `test/integration/api_entitlement_gates_test.go`:

```go
package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntitlementGates_RFQCatalogSeats(t *testing.T) {
	const email, pass = "gate-admin@test.local", "admin-supersecret"
	pool := poolOrSkip(t) // same DB buildAPIWithAdmin uses; needed for verifyUser
	app := buildAPIWithAdmin(t, email, pass)

	st, login := doJSON(t, app, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": pass})
	require.Equal(t, http.StatusOK, st)
	adminTok := login["token"].(string)

	// Lower Free to 1/1/1.
	st, _ = doJSON(t, app, "PATCH", "/admin/v1/plans/free", adminTok, map[string]any{
		"agent_autonomy": false, "catalog_max_items": 1, "rfq_monthly_limit": 1, "seats": 1,
	})
	require.Equal(t, http.StatusOK, st)
	t.Cleanup(func() {
		doJSON(t, app, "PATCH", "/admin/v1/plans/free", adminTok, map[string]any{
			"agent_autonomy": false, "catalog_max_items": 25, "rfq_monthly_limit": 10, "seats": 2,
		})
	})

	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "GateCo", "company_type": "both",
		"email": uniqueEmail("gate-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := reg["token"].(string)
	verifyUser(t, pool, reg["user_id"].(string)) // RFQ create sits behind RequireVerified

	// Catalog: 1st ok, 2nd paywalled.
	st, _ = doJSON(t, app, "POST", "/v1/catalog", token, map[string]any{"name": "A", "category": "c"})
	require.Equal(t, http.StatusCreated, st)
	st, pw := doJSON(t, app, "POST", "/v1/catalog", token, map[string]any{"name": "B", "category": "c"})
	require.Equal(t, http.StatusPaymentRequired, st)
	require.Equal(t, "upgrade_required", pw["code"])
	require.Equal(t, "catalog_max_items", pw["feature"])

	// RFQ: 1st ok, 2nd paywalled (limit 1).
	st, _ = doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{"title": "need widgets", "text": "100 blue widgets"})
	require.Equal(t, http.StatusCreated, st)
	st, pw = doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{"title": "need more", "text": "more widgets"})
	require.Equal(t, http.StatusPaymentRequired, st)
	require.Equal(t, "rfq_monthly_limit", pw["feature"])

	// Seats: owner already fills the single seat, so an invite is paywalled.
	st, pw = doJSON(t, app, "POST", "/v1/users/invite", token, map[string]any{"email": uniqueEmail("inv-"), "role": "member"})
	require.Equal(t, http.StatusPaymentRequired, st)
	require.Equal(t, "seats", pw["feature"])
}
```

Two facts the test relies on (both confirmed in the codebase):
- The RFQ create route is `POST /v1/rfqs` (plural) and sits behind `RequireVerified`; brand-new signups are **unverified** (Sprint 1 grandfathered only pre-migration users), so the test calls `verifyUser(t, pool, reg["user_id"].(string))` right after register to get past the verify gate before the quota gate. `verifyUser` lives in `test/integration/db_test.go` with signature `func verifyUser(t *testing.T, pool *pgxpool.Pool, userID string)`.
- `POST /v1/rfqs` runs the extractor; with no `ANTHROPIC_API_KEY` in the test config it matches nothing, but the RFQ is still created (see `api_rfq_test.go`) — a 201, which is all this test needs.

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestEntitlementGates_RFQCatalogSeats -v`
Expected: FAIL — creates succeed past the cap (gates not wired); 2nd catalog returns 201, not 402.

- [ ] **Step 3: Gate the catalog create**

In `internal/catalog/handler.go`: add the import `"github.com/tradebridge/core/internal/entitlements"`; add field `ent *entitlements.Service` to `Handler`; change `NewHandler` to `func NewHandler(q *sqlc.Queries, ent *entitlements.Service) *Handler { return &Handler{q: q, ent: ent} }`. In `create`, before `h.q.CreateCatalogSummary`:

```go
	if err := entitlements.Fail(c, h.ent.CheckCatalogQuota(c.Context(), middleware.CompanyID(c))); err != nil {
		return err
	}
```

- [ ] **Step 4: Gate the RFQ create + increment on success**

In `internal/rfq/handler.go`: add the import; add `ent *entitlements.Service` to `Handler`; thread it through `NewHandler` (add a trailing `ent *entitlements.Service` param and store it). In `create`, before calling `h.svc.CreateAndMatch`:

```go
	if err := entitlements.Fail(c, h.ent.CheckRFQQuota(c.Context(), middleware.CompanyID(c))); err != nil {
		return err
	}
```

After a successful create (immediately before the `return c.Status(fiber.StatusCreated)...` line, once `err` is confirmed nil):

```go
	h.ent.IncrementRFQUsage(c.Context(), middleware.CompanyID(c))
```

- [ ] **Step 5: Gate the invite**

In `internal/identity/handler.go`: add the import; add `ent *entitlements.Service` to `Handler`; add a trailing `ent *entitlements.Service` param to `NewHandler` and store it. In `invite`, after the role validation and before `randomToken()`:

```go
	if err := entitlements.Fail(c, h.ent.CheckSeatQuota(c.Context(), middleware.CompanyID(c))); err != nil {
		return err
	}
```

- [ ] **Step 6: Update the router constructor calls**

In `internal/httpapi/router.go`, pass `entSvc` to the three constructors:
- `identity.NewHandler(q, tokens, notifier, entSvc).Register(f, guard...)`
- `catalog.NewHandler(q, entSvc).Register(f, guard...)`
- `rfq.NewHandler(rfq.NewService(...), q, entSvc).Register(f, middleware.RequireVerified(q), guard...)`

(Match each existing call's other args exactly; only append `entSvc`.)

- [ ] **Step 7: Fix any other callers + run the full suite**

Other call sites of these constructors (e.g. unit tests, `cmd/`) must pass the new arg. Find them:

```bash
grep -rn "identity.NewHandler\|catalog.NewHandler\|rfq.NewHandler" --include=*.go .
```

For any non-router caller, pass a real service (`entitlements.NewService(q, zerolog.Nop())`) — never `nil` (the handlers dereference `h.ent`). Then:

Run: `go build ./... && TEST_DATABASE_URL="$DB_URL" go test ./...`
Expected: PASS — the new gate test passes and no pre-existing test regresses.

- [ ] **Step 8: Commit**

```bash
git add internal/rfq/handler.go internal/catalog/handler.go internal/identity/handler.go internal/httpapi/router.go test/integration/api_entitlement_gates_test.go
git commit -m "feat(entitlements): server-side quota gates on rfq/catalog/invite (402 paywall)"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** entitlement definitions (Task 1 seed) ✓; `RequireEntitlement`-style helpers (Task 3 `Check*`) ✓; usage metering + counters (Tasks 1,3) ✓; server-side gate fail-closed (Tasks 2,3,6) ✓; both-role = one plan (company-keyed, inherent) ✓; admin-editable limits (Task 5) ✓; `GET /v1/billing/summary` (Task 4) ✓. **Deferred to later plans (correctly):** the `agent_autonomy` *behavioral* gate + gateway endpoint (Plan 3), all Stripe (Plan 2), the frontend (Plan 4). No Core route toggles autonomy, so no boolean route-gate exists in Plan 1 — `AgentAutonomy` is surfaced via `summary`/`For` for Plan 3 to consume.
- **Placeholder scan:** every code step contains complete code. The one scaffold (`lowerFreePlan`) is explicitly flagged for deletion in Task 6 Step 1.
- **Type consistency:** `Entitlements`/`Usage`/`PaywallError` names and `Service.*` signatures are consistent across Tasks 2–6; sqlc names (`RfqMonthlyLimit`, `GetUsageCounterParams`, etc.) follow sqlc's Go casing from the Task 1 queries. `NewHandler` arity changes are threaded through Task 6 Step 7's caller sweep.
- **Known caveat surfaced to executor:** RFQ create sits behind `RequireVerified`; the gate test must verify the user first (Task 6 Step 1 note). Confirm the `verifyUser` helper signature before use.

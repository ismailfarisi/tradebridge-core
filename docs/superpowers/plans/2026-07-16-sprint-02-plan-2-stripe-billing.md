# Sprint 2 — Plan 2: Stripe Billing (Core) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> ## ⚠️ ACTIVATION CHECKLIST — fix BEFORE setting `STRIPE_SECRET_KEY` or exposing on a public origin
> Implemented + reviewed stub-only; the whole-branch review flagged these in the (untested) real-Stripe path. Fix them at activation:
> - **[FIXED in-branch] C1 — fail closed when unconfigured.** Keyless deploy previously stubbed the PUBLIC webhook, which parses UNSIGNED JSON → anyone could POST `{company_id,plan:pro,status:active}` to make any company Pro. Fixed (`fec0c91`): checkout/portal/webhook now 503 unless `provider.Enabled()` (real key) OR `BILLING_DEV_MODE=true`. **NEVER set `BILLING_DEV_MODE=true` on any internet-reachable env.**
> - **I1 — out-of-order delivery can reactivate a canceled sub.** `UpsertSubscription` overwrites status/plan unconditionally; Stripe delivers at-least-once + out-of-order, so a stale `subscription.updated active` arriving after `deleted` re-grants Pro (incl. agent autonomy). Fix: refetch the subscription from Stripe as source of truth on each event, or store & compare the event `created` timestamp and skip older events.
> - **I2 — partial events clobber good columns.** `invoice.payment_failed`/`checkout.session.completed` carry no plan, so `service.go` defaults plan→`pro`; a Business company in dunning is rewritten to `pro`. Fix: refetch from Stripe, or preserve unspecified columns (COALESCE-style), or ensure acted-on events always carry plan/interval.
> - **I3 — `checkout.session.completed` grants active without checking `payment_status`.** Gate on `payment_status == "paid"` or rely solely on `customer.subscription.*` events.
> - Minors: add UNIQUE on `companies.stripe_customer_id`; add a Stripe idempotency key to `EnsureCustomer` (avoid duplicate customers); consider a dedicated unguarded prefix `/v1/webhooks/stripe` so webhook publicness doesn't depend on route-registration order; consolidate the two guarded `/v1/billing` groups (billing + entitlements) into one.

**Goal:** Turn a Free company into a paying Pro/Business one and back, driven by Stripe — checkout, customer portal, and idempotent signed webhooks that write the `subscriptions` row Plan 1's entitlements resolver already reads. Build and fully test **stub-only** (no Stripe account); real Stripe is an activation flip.

**Architecture:** A `Billing` interface (provider port) with two implementations: `StripeBilling` (wraps `stripe-go/v82`, active when `STRIPE_SECRET_KEY` is set) and `StubBilling` (deterministic, no network — used in tests and when the key is empty). All Stripe types are confined to the `stripebilling.go` adapter; the rest of the system speaks a normalized `WebhookEvent`. A `billing.Service` applies normalized events to the `subscriptions` table (upsert-by-company, idempotent). Plan resolution is unchanged — `entitlements` reads `GetActiveSubscription`, so the moment a webhook writes `status=active plan=pro`, entitlements flip to Pro with no cache coupling.

**Tech Stack:** Go 1.26.4, Fiber v2, sqlc (pgx/v5), `github.com/stripe/stripe-go/v82`, Postgres 16. Mirrors `internal/notify` (interface + stub/real selection by config key) and Plan 1's `internal/entitlements`.

## Global Constraints

- **Stub-only this session.** Every test uses `StubBilling`; no network, no Stripe keys. `StripeBilling` (Task 3) is written and compiled but only unit-tested for construction/selection — its live behavior is an **activation follow-up**.
- **Fail-closed:** a company with no active subscription is Free (Plan 1 already guarantees this). A canceled/unpaid subscription must resolve to Free — never leave a stale `active` row.
- **Webhook idempotency:** every processed Stripe event id is recorded in `processed_stripe_events`; a re-delivered event is a no-op. Idempotency is checked **before** mutating the subscription.
- **Webhook security:** the webhook route is PUBLIC (no tenant guard) but `StripeBilling.ParseWebhook` MUST verify the Stripe signature (`STRIPE_WEBHOOK_SECRET`); a bad signature ⇒ 400, no mutation. `StubBilling.ParseWebhook` skips signature (dev/test only) and parses our normalized JSON envelope.
- **One subscription row per company:** `subscriptions.company_id` is UNIQUE (Plan 1). All writes are UPSERT on `company_id` — never insert a second row.
- **Entitlement-honored statuses:** `{trialing, active, past_due}` keep the plan; `{canceled, unpaid, incomplete}` (or no row) ⇒ Free. This matches `GetActiveSubscription`'s filter from Plan 1 — do not change that filter.
- **Plan/interval vocab:** plan ∈ `{pro, business}` for checkout (you cannot "buy" Free); interval ∈ `{month, year}`.
- **sqlc regen:** after editing `pkg/db/query/billing.sql`, run `sqlc generate` (the host has no `make`; run `sqlc generate` directly — do NOT run `wire`, billing is hand-wired in `router.go` like notify/entitlements). Commit regenerated `pkg/db/sqlc/*`.
- **No secrets committed.** All `STRIPE_*` config defaults to `""`.
- **Local only:** feature branch, never push. Dev DB on `:5442` via `TEST_DATABASE_URL`. Do not touch the live stack.

## File Structure

- `internal/config/config.go` — add `STRIPE_*` fields + defaults.
- `internal/billing/billing.go` — `Billing` interface, normalized `WebhookEvent`/`CheckoutParams` types, `NewBilling` selector.
- `internal/billing/stub.go` — `StubBilling` (deterministic, JSON-envelope webhook parsing).
- `internal/billing/stripebilling.go` — `StripeBilling` (stripe-go v82 adapter). **Only file importing stripe-go.**
- `internal/billing/service.go` — `Service.ApplyWebhookEvent` state machine over the query layer.
- `internal/billing/handler.go` — `POST /v1/billing/checkout`, `/portal`, `/webhook`.
- `pkg/db/query/billing.sql` — add subscription/customer/event queries; regenerate.
- `internal/httpapi/router.go`, `internal/httpapi` `Config`, `cmd/api/main.go` — thread config, construct, register.
- Tests: `test/integration/api_billing_stripe_test.go`, plus package tests `internal/billing/stub_test.go`, `internal/billing/service_test.go` (the service test may live in `test/integration` since it needs a DB — see tasks).

---

## Task 1: Config + billing interface + stub + queries

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/billing/billing.go`, `internal/billing/stub.go`
- Modify: `pkg/db/query/billing.sql`; regenerate `pkg/db/sqlc/*`
- Test: `internal/billing/stub_test.go`

**Interfaces:**
- Produces:
  - Types `WebhookEvent`, `CheckoutParams`, `Billing` interface (below).
  - `func NewStub() *StubBilling`.
  - sqlc: `q.UpsertSubscription`, `q.GetSubscriptionByCompany`, `q.SetCompanyStripeCustomer`, `q.GetCompanyByStripeCustomer`, `q.IsStripeEventProcessed`, `q.MarkStripeEventProcessed`.

- [ ] **Step 1: Write the failing test**

Create `internal/billing/stub_test.go`:

```go
package billing

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStub_RoundTripsWebhookEnvelope(t *testing.T) {
	s := NewStub()
	require.False(t, s.Enabled())

	ev := WebhookEvent{
		ID: "evt_1", CompanyID: "co_1", CustomerID: "cus_1", SubscriptionID: "sub_1",
		Plan: "pro", Interval: "month", Status: "active",
	}
	payload, err := json.Marshal(ev)
	require.NoError(t, err)

	got, err := s.ParseWebhook(payload, "")
	require.NoError(t, err)
	require.Equal(t, ev.ID, got.ID)
	require.Equal(t, "pro", got.Plan)
	require.Equal(t, "active", got.Status)
}

func TestStub_CheckoutAndCustomerAreDeterministic(t *testing.T) {
	s := NewStub()
	cust, err := s.EnsureCustomer(context.Background(), "co_42", "a@b.com", "")
	require.NoError(t, err)
	require.Equal(t, "cus_stub_co_42", cust)

	url, err := s.CreateCheckoutSession(context.Background(), CheckoutParams{
		CompanyID: "co_42", CustomerID: cust, Plan: "pro", Interval: "month",
		SuccessURL: "s", CancelURL: "c",
	})
	require.NoError(t, err)
	require.Contains(t, url, "stub")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go build ./...`
Expected: FAIL — package `billing` does not exist.

- [ ] **Step 3: Write the interface + types**

Create `internal/billing/billing.go`:

```go
// Package billing integrates Stripe for subscriptions and drives the
// subscriptions table that internal/entitlements reads. Stripe types are
// confined to stripebilling.go; the rest of the system speaks WebhookEvent.
package billing

import "context"

// CheckoutParams describes a Stripe Checkout session to open.
type CheckoutParams struct {
	CompanyID     string
	CustomerID    string // Stripe customer id (ensure it first)
	CustomerEmail string
	Plan          string // pro | business
	Interval      string // month | year
	SuccessURL    string
	CancelURL     string
}

// WebhookEvent is the normalized, provider-agnostic result of parsing a Stripe
// webhook. Status uses our subscription vocabulary
// (trialing|active|past_due|canceled|unpaid|incomplete). Type is informational.
type WebhookEvent struct {
	ID                string    `json:"id"`
	Type              string    `json:"type"`
	CompanyID         string    `json:"company_id"`
	CustomerID        string    `json:"customer_id"`
	SubscriptionID    string    `json:"subscription_id"`
	Plan              string    `json:"plan"`     // free|pro|business
	Interval          string    `json:"interval"` // month|year
	Status            string    `json:"status"`
	CurrentPeriodEnd  int64     `json:"current_period_end"`  // unix seconds; 0 = unknown
	CancelAtPeriodEnd bool      `json:"cancel_at_period_end"`
	// Ignore marks an event we recognize but don't act on (keeps handler simple).
	Ignore bool `json:"ignore"`
}

// Billing is the provider port. StripeBilling and StubBilling implement it.
type Billing interface {
	// Enabled reports whether a real Stripe provider is configured.
	Enabled() bool
	// EnsureCustomer returns a Stripe customer id for the company, creating one
	// if existingCustomerID is empty.
	EnsureCustomer(ctx context.Context, companyID, email, existingCustomerID string) (string, error)
	// CreateCheckoutSession returns a redirect URL for the given plan/interval.
	CreateCheckoutSession(ctx context.Context, p CheckoutParams) (string, error)
	// CreatePortalSession returns a Stripe Customer Portal deep-link.
	CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, error)
	// ParseWebhook verifies + normalizes a webhook request body.
	ParseWebhook(payload []byte, sigHeader string) (WebhookEvent, error)
}
```

Create `internal/billing/stub.go`:

```go
package billing

import (
	"context"
	"encoding/json"
	"fmt"
)

// StubBilling is a deterministic, network-free Billing used in tests and when
// STRIPE_SECRET_KEY is unset. Its ParseWebhook reads a normalized WebhookEvent
// straight from the JSON body (no signature), so tests can drive the full
// checkout→webhook→entitlement flow without Stripe.
type StubBilling struct{}

// NewStub builds the stub provider.
func NewStub() *StubBilling { return &StubBilling{} }

func (s *StubBilling) Enabled() bool { return false }

func (s *StubBilling) EnsureCustomer(_ context.Context, companyID, _ , existing string) (string, error) {
	if existing != "" {
		return existing, nil
	}
	return "cus_stub_" + companyID, nil
}

func (s *StubBilling) CreateCheckoutSession(_ context.Context, p CheckoutParams) (string, error) {
	return fmt.Sprintf("https://stub.local/checkout?company=%s&plan=%s&interval=%s", p.CompanyID, p.Plan, p.Interval), nil
}

func (s *StubBilling) CreatePortalSession(_ context.Context, customerID, returnURL string) (string, error) {
	return fmt.Sprintf("https://stub.local/portal?customer=%s&return=%s", customerID, returnURL), nil
}

func (s *StubBilling) ParseWebhook(payload []byte, _ string) (WebhookEvent, error) {
	var ev WebhookEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return WebhookEvent{}, err
	}
	return ev, nil
}
```

- [ ] **Step 4: Add the queries**

Append to `pkg/db/query/billing.sql`:

```sql
-- name: UpsertSubscription :one
INSERT INTO subscriptions (
    company_id, stripe_customer_id, stripe_subscription_id, plan, interval,
    status, current_period_end, cancel_at_period_end, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
ON CONFLICT (company_id) DO UPDATE SET
    stripe_customer_id     = EXCLUDED.stripe_customer_id,
    stripe_subscription_id = EXCLUDED.stripe_subscription_id,
    plan                   = EXCLUDED.plan,
    interval               = EXCLUDED.interval,
    status                 = EXCLUDED.status,
    current_period_end     = EXCLUDED.current_period_end,
    cancel_at_period_end   = EXCLUDED.cancel_at_period_end,
    updated_at             = NOW()
RETURNING *;

-- name: GetSubscriptionByCompany :one
SELECT * FROM subscriptions WHERE company_id = $1;

-- name: SetCompanyStripeCustomer :exec
UPDATE companies SET stripe_customer_id = $2 WHERE id = $1;

-- name: GetCompanyByStripeCustomer :one
SELECT * FROM companies WHERE stripe_customer_id = $1;

-- name: IsStripeEventProcessed :one
SELECT EXISTS(SELECT 1 FROM processed_stripe_events WHERE event_id = $1);

-- name: MarkStripeEventProcessed :exec
INSERT INTO processed_stripe_events (event_id, type)
VALUES ($1, $2)
ON CONFLICT (event_id) DO NOTHING;
```

- [ ] **Step 5: Regenerate sqlc**

Run: `sqlc generate`
Expected: success; `pkg/db/sqlc/billing.sql.go` gains the six methods. Note the generated param types — `stripe_customer_id`, `stripe_subscription_id`, `current_period_end` are nullable, so sqlc emits pointer fields (`*string`, `*string`, `*time.Time`) in `UpsertSubscriptionParams`. Record the exact generated field names/types; Task 2 depends on them.

- [ ] **Step 6: Run the stub test**

Run: `go test ./internal/billing/ -run TestStub -v`
Expected: PASS (3 subtests).

- [ ] **Step 7: Add config fields**

In `internal/config/config.go`, add to the `Config` struct (after `SentryDSN`):

```go
	// Stripe billing (Sprint 2). All empty => StubBilling (dev/test); checkout
	// returns a stub URL. Set these to go live (activation follow-up).
	StripeSecretKey          string `mapstructure:"STRIPE_SECRET_KEY"`
	StripeWebhookSecret      string `mapstructure:"STRIPE_WEBHOOK_SECRET"`
	StripePriceProMonth      string `mapstructure:"STRIPE_PRICE_PRO_MONTH"`
	StripePriceProYear       string `mapstructure:"STRIPE_PRICE_PRO_YEAR"`
	StripePriceBusinessMonth string `mapstructure:"STRIPE_PRICE_BUSINESS_MONTH"`
	StripePriceBusinessYear  string `mapstructure:"STRIPE_PRICE_BUSINESS_YEAR"`
```

And in `Load()`, add defaults alongside the others:

```go
	v.SetDefault("STRIPE_SECRET_KEY", "")
	v.SetDefault("STRIPE_WEBHOOK_SECRET", "")
	v.SetDefault("STRIPE_PRICE_PRO_MONTH", "")
	v.SetDefault("STRIPE_PRICE_PRO_YEAR", "")
	v.SetDefault("STRIPE_PRICE_BUSINESS_MONTH", "")
	v.SetDefault("STRIPE_PRICE_BUSINESS_YEAR", "")
```

Run `go build ./...` — clean.

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/billing/billing.go internal/billing/stub.go internal/billing/stub_test.go pkg/db/query/billing.sql pkg/db/sqlc/
git commit -m "feat(billing): Billing interface + StubBilling + subscription queries + config"
```

---

## Task 2: Subscription state machine (`Service.ApplyWebhookEvent`)

**Files:**
- Create: `internal/billing/service.go`
- Test: `test/integration/api_billing_stripe_test.go` (service-level test; needs a DB)

**Interfaces:**
- Consumes: `q.IsStripeEventProcessed`, `q.MarkStripeEventProcessed`, `q.UpsertSubscription`, `q.SetCompanyStripeCustomer`, `q.GetCompanyByStripeCustomer` (Task 1); `entitlements.Service` (Plan 1, to assert the effect); `WebhookEvent` (Task 1).
- Produces: `func NewService(q *sqlc.Queries, logger zerolog.Logger) *Service`; `func (s *Service) ApplyWebhookEvent(ctx, ev WebhookEvent) error`.

- [ ] **Step 1: Write the failing test**

Create `test/integration/api_billing_stripe_test.go`:

```go
package integration

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/billing"
	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestBillingService_ApplyEventDrivesPlanAndIsIdempotent(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	svc := billing.NewService(q, zerolog.Nop())
	ent := entitlements.NewService(q, zerolog.Nop())
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "PayCo", Type: "both"})
	require.NoError(t, err)
	require.Equal(t, "free", ent.PlanCode(ctx, co.ID))

	// checkout.session.completed → active Pro.
	activate := billing.WebhookEvent{
		ID: "evt_activate_" + co.ID, CompanyID: co.ID, CustomerID: "cus_" + co.ID,
		SubscriptionID: "sub_" + co.ID, Plan: "pro", Interval: "month", Status: "active",
	}
	require.NoError(t, svc.ApplyWebhookEvent(ctx, activate))
	require.Equal(t, "pro", ent.PlanCode(ctx, co.ID))
	require.True(t, ent.For(ctx, co.ID).AgentAutonomy)

	// Re-delivery of the SAME event id is a no-op (idempotent).
	require.NoError(t, svc.ApplyWebhookEvent(ctx, activate))
	require.Equal(t, "pro", ent.PlanCode(ctx, co.ID))

	// past_due keeps the plan (grace).
	require.NoError(t, svc.ApplyWebhookEvent(ctx, billing.WebhookEvent{
		ID: "evt_pastdue_" + co.ID, CompanyID: co.ID, CustomerID: "cus_" + co.ID,
		SubscriptionID: "sub_" + co.ID, Plan: "pro", Interval: "month", Status: "past_due",
	}))
	require.Equal(t, "pro", ent.PlanCode(ctx, co.ID))

	// canceled → Free.
	require.NoError(t, svc.ApplyWebhookEvent(ctx, billing.WebhookEvent{
		ID: "evt_cancel_" + co.ID, CompanyID: co.ID, CustomerID: "cus_" + co.ID,
		SubscriptionID: "sub_" + co.ID, Plan: "pro", Interval: "month", Status: "canceled",
	}))
	require.Equal(t, "free", ent.PlanCode(ctx, co.ID))
	require.False(t, ent.For(ctx, co.ID).AgentAutonomy)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go build ./...`
Expected: FAIL — `billing.NewService`/`ApplyWebhookEvent` undefined.

- [ ] **Step 3: Write the service**

Create `internal/billing/service.go`. Adjust the pointer-field construction to match the EXACT sqlc-generated `UpsertSubscriptionParams` types recorded in Task 1 Step 5 (nullable columns → pointers):

```go
package billing

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Service applies normalized webhook events to the subscriptions table. It is
// idempotent per Stripe event id and upserts the single row per company.
type Service struct {
	q      *sqlc.Queries
	logger zerolog.Logger
}

// NewService builds the billing service.
func NewService(q *sqlc.Queries, logger zerolog.Logger) *Service {
	return &Service{q: q, logger: logger}
}

// ApplyWebhookEvent records the event (idempotent) and updates the company's
// subscription row. Unknown/ignored events are recorded and skipped.
func (s *Service) ApplyWebhookEvent(ctx context.Context, ev WebhookEvent) error {
	if ev.ID != "" {
		already, err := s.q.IsStripeEventProcessed(ctx, ev.ID)
		if err != nil {
			return err
		}
		if already {
			return nil // idempotent no-op
		}
	}

	if !ev.Ignore {
		if err := s.applyState(ctx, ev); err != nil {
			return err
		}
	}

	if ev.ID != "" {
		if err := s.q.MarkStripeEventProcessed(ctx, sqlc.MarkStripeEventProcessedParams{
			EventID: ev.ID, Type: ev.Type,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyState(ctx context.Context, ev WebhookEvent) error {
	companyID, err := s.resolveCompany(ctx, ev)
	if err != nil {
		return err
	}
	if companyID == "" {
		s.logger.Warn().Str("event", ev.ID).Str("customer", ev.CustomerID).Msg("billing: event with no resolvable company, skipping")
		return nil
	}

	// Keep companies.stripe_customer_id in sync (first time we learn it).
	if ev.CustomerID != "" {
		if err := s.q.SetCompanyStripeCustomer(ctx, sqlc.SetCompanyStripeCustomerParams{
			ID: companyID, StripeCustomerID: ptr(ev.CustomerID),
		}); err != nil {
			return err
		}
	}

	status := ev.Status
	if status == "" {
		status = "incomplete"
	}
	var periodEnd *time.Time
	if ev.CurrentPeriodEnd > 0 {
		t := time.Unix(ev.CurrentPeriodEnd, 0).UTC()
		periodEnd = &t
	}
	interval := ev.Interval
	if interval != "month" && interval != "year" {
		interval = "month"
	}
	plan := ev.Plan
	if plan == "" {
		plan = "pro"
	}

	_, err = s.q.UpsertSubscription(ctx, sqlc.UpsertSubscriptionParams{
		CompanyID:            companyID,
		StripeCustomerID:     ptrOrNil(ev.CustomerID),
		StripeSubscriptionID: ptrOrNil(ev.SubscriptionID),
		Plan:                 plan,
		Interval:             interval,
		Status:               status,
		CurrentPeriodEnd:     periodEnd,
		CancelAtPeriodEnd:    ev.CancelAtPeriodEnd,
	})
	return err
}

// resolveCompany prefers the event's explicit company id, falling back to a
// lookup by Stripe customer id.
func (s *Service) resolveCompany(ctx context.Context, ev WebhookEvent) (string, error) {
	if ev.CompanyID != "" {
		return ev.CompanyID, nil
	}
	if ev.CustomerID == "" {
		return "", nil
	}
	co, err := s.q.GetCompanyByStripeCustomer(ctx, ptr(ev.CustomerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return co.ID, nil
}

func ptr(s string) *string { return &s }

// ptrOrNil returns nil for an empty string so the column stores NULL.
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
```

**Reviewer/implementer note:** the `SetCompanyStripeCustomerParams.StripeCustomerID` and `GetCompanyByStripeCustomer` argument types depend on whether sqlc made `companies.stripe_customer_id` nullable (it is) → `*string`. If sqlc generated a non-pointer or a `pgtype`, adjust `ptr(...)`/`ptrOrNil(...)` accordingly so the file builds. `go build` is the arbiter.

- [ ] **Step 4: Run the test**

Run: `export DB_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable"; TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestBillingService_ApplyEventDrivesPlanAndIsIdempotent -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/billing/service.go test/integration/api_billing_stripe_test.go
git commit -m "feat(billing): idempotent subscription state machine (webhook -> plan)"
```

---

## Task 3: Stripe adapter + provider selection

**Files:**
- Create: `internal/billing/stripebilling.go` (the ONLY file importing stripe-go)
- Modify: `internal/billing/billing.go` (add `NewBilling` selector + `Prices` config struct)
- Test: `internal/billing/stub_test.go` (add a selector test)

**Interfaces:**
- Produces: `type Prices struct { ProMonth, ProYear, BusinessMonth, BusinessYear string }`; `type StripeConfig struct { SecretKey, WebhookSecret string; Prices Prices }`; `func NewBilling(cfg StripeConfig, logger zerolog.Logger) Billing` — returns `*StripeBilling` when `cfg.SecretKey != ""`, else `NewStub()`.

- [ ] **Step 1: Write the failing test**

Add to `internal/billing/stub_test.go`:

```go
func TestNewBilling_SelectsStubWhenNoKey(t *testing.T) {
	b := NewBilling(StripeConfig{}, zerolog.Nop())
	require.False(t, b.Enabled())
}

func TestNewBilling_SelectsStripeWhenKeySet(t *testing.T) {
	b := NewBilling(StripeConfig{SecretKey: "sk_test_x"}, zerolog.Nop())
	require.True(t, b.Enabled())
}
```

Add imports `"github.com/rs/zerolog"` to the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go build ./...`
Expected: FAIL — `NewBilling`/`StripeConfig` undefined.

- [ ] **Step 3: Add the dependency**

Run:
```bash
go get github.com/stripe/stripe-go/v82@latest
```
Expected: `go.mod`/`go.sum` gain the module. (Network is available.) If v82 resolves to a beta only, pin the latest **stable** v82 tag instead; record the resolved version in your report.

- [ ] **Step 4: Add the selector**

In `internal/billing/billing.go`, add:

```go
// Prices maps our plan/interval pairs to Stripe price ids.
type Prices struct {
	ProMonth      string
	ProYear       string
	BusinessMonth string
	BusinessYear  string
}

// priceFor returns the Stripe price id for a plan/interval, or "" if unknown.
func (p Prices) priceFor(plan, interval string) string {
	switch {
	case plan == "pro" && interval == "month":
		return p.ProMonth
	case plan == "pro" && interval == "year":
		return p.ProYear
	case plan == "business" && interval == "month":
		return p.BusinessMonth
	case plan == "business" && interval == "year":
		return p.BusinessYear
	}
	return ""
}

// StripeConfig configures the real provider.
type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	Prices        Prices
}
```

Then add `NewBilling` — put it in `billing.go` (imports `zerolog`):

```go
// NewBilling selects the real Stripe provider when a secret key is configured,
// otherwise the deterministic stub.
func NewBilling(cfg StripeConfig, logger zerolog.Logger) Billing {
	if cfg.SecretKey == "" {
		return NewStub()
	}
	return newStripeBilling(cfg, logger)
}
```

Add `"github.com/rs/zerolog"` to `billing.go` imports.

- [ ] **Step 5: Write the Stripe adapter**

Create `internal/billing/stripebilling.go`. **This is the only place stripe-go is imported. The exact stripe-go v82 API (package paths, param struct fields, `stripe.String`/`stripe.Int64` helpers) must match the version `go get` installed — adapt the code below to compile against it; the `Billing` interface contract is fixed.** Intended implementation:

```go
package billing

import (
	"context"
	"errors"

	"github.com/rs/zerolog"
	stripe "github.com/stripe/stripe-go/v82"
	billingportal "github.com/stripe/stripe-go/v82/billingportal/session"
	checkout "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/webhook"
)

// StripeBilling is the real provider. Constructed only when a secret key is set.
type StripeBilling struct {
	cfg    StripeConfig
	logger zerolog.Logger
}

func newStripeBilling(cfg StripeConfig, logger zerolog.Logger) *StripeBilling {
	stripe.Key = cfg.SecretKey
	return &StripeBilling{cfg: cfg, logger: logger}
}

func (b *StripeBilling) Enabled() bool { return true }

func (b *StripeBilling) EnsureCustomer(ctx context.Context, companyID, email, existing string) (string, error) {
	if existing != "" {
		return existing, nil
	}
	params := &stripe.CustomerParams{Email: stripe.String(email)}
	params.AddMetadata("company_id", companyID)
	c, err := customer.New(params)
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

func (b *StripeBilling) CreateCheckoutSession(ctx context.Context, p CheckoutParams) (string, error) {
	price := b.cfg.Prices.priceFor(p.Plan, p.Interval)
	if price == "" {
		return "", errors.New("no Stripe price configured for " + p.Plan + "/" + p.Interval)
	}
	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		Customer:          stripe.String(p.CustomerID),
		ClientReferenceID: stripe.String(p.CompanyID),
		SuccessURL:        stripe.String(p.SuccessURL),
		CancelURL:         stripe.String(p.CancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(price), Quantity: stripe.Int64(1)},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{},
	}
	params.SubscriptionData.AddMetadata("company_id", p.CompanyID)
	params.SubscriptionData.AddMetadata("plan", p.Plan)
	// Enable automatic tax (real Tax needs the live account; safe to request).
	params.AutomaticTax = &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)}
	sess, err := checkout.New(params)
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}

func (b *StripeBilling) CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	sess, err := billingportal.New(params)
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}

// ParseWebhook verifies the Stripe signature and normalizes the event. Only the
// subscription-lifecycle events we act on are mapped; others are marked Ignore.
func (b *StripeBilling) ParseWebhook(payload []byte, sigHeader string) (WebhookEvent, error) {
	event, err := webhook.ConstructEvent(payload, sigHeader, b.cfg.WebhookSecret)
	if err != nil {
		return WebhookEvent{}, err
	}
	out := WebhookEvent{ID: event.ID, Type: string(event.Type)}

	switch event.Type {
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return WebhookEvent{}, err
		}
		out.CustomerID = sub.Customer.ID
		out.SubscriptionID = sub.ID
		out.CompanyID = sub.Metadata["company_id"]
		out.Plan = sub.Metadata["plan"]
		out.Status = normalizeStatus(string(sub.Status))
		out.CancelAtPeriodEnd = sub.CancelAtPeriodEnd
		out.CurrentPeriodEnd = sub.CurrentPeriodEnd
		out.Interval = subInterval(&sub)
		if event.Type == "customer.subscription.deleted" {
			out.Status = "canceled"
		}
	case "checkout.session.completed":
		var cs stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &cs); err != nil {
			return WebhookEvent{}, err
		}
		out.CompanyID = cs.ClientReferenceID
		if cs.Customer != nil {
			out.CustomerID = cs.Customer.ID
		}
		if cs.Subscription != nil {
			out.SubscriptionID = cs.Subscription.ID
		}
		out.Status = "active"
	case "invoice.payment_failed":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return WebhookEvent{}, err
		}
		if inv.Customer != nil {
			out.CustomerID = inv.Customer.ID
		}
		out.Status = "past_due"
	default:
		out.Ignore = true
	}
	return out, nil
}

// normalizeStatus maps Stripe subscription statuses to our vocabulary.
func normalizeStatus(s string) string {
	switch s {
	case "trialing", "active", "past_due", "canceled", "unpaid", "incomplete":
		return s
	case "incomplete_expired":
		return "canceled"
	default:
		return "incomplete"
	}
}

// subInterval extracts month/year from the subscription's first price recurrence.
func subInterval(sub *stripe.Subscription) string {
	if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil && sub.Items.Data[0].Price.Recurring != nil {
		if string(sub.Items.Data[0].Price.Recurring.Interval) == "year" {
			return "year"
		}
	}
	return "month"
}
```

Add `"encoding/json"` to the imports. **Adapt field names** (`CurrentPeriodEnd`, `Customer.ID`, `Subscription.ID`, `Data.Raw`, metadata access, the `stripe.CheckoutSessionMode*` constants) to the installed v82 API — some may differ (e.g. `event.Data.Raw` vs `event.Data.Object`); use `go build`/`go doc github.com/stripe/stripe-go/v82` to resolve. Keep the normalization semantics identical.

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go vet ./... && go test ./internal/billing/ -v`
Expected: PASS (stub round-trip, deterministic, selector-picks-stub, selector-picks-stripe). The Stripe adapter has no live test (no keys) — construction + `Enabled()` is all that's exercised.

- [ ] **Step 7: Commit**

```bash
git add internal/billing/stripebilling.go internal/billing/billing.go internal/billing/stub_test.go go.mod go.sum
git commit -m "feat(billing): Stripe adapter (stripe-go v82) + provider selection"
```

---

## Task 4: HTTP handlers + router/cmd wiring + E2E

**Files:**
- Create: `internal/billing/handler.go`
- Modify: `internal/httpapi/router.go` (Config fields + construction + registration)
- Modify: `cmd/api/main.go` (map config)
- Test: `test/integration/api_billing_stripe_test.go` (add the HTTP E2E)

**Interfaces:**
- Consumes: `Billing` (Task 1/3), `billing.Service` (Task 2), `entitlements.Service` (Plan 1, for summary already), sqlc customer/subscription queries.
- Produces: `func NewHandler(q *sqlc.Queries, provider Billing, svc *Service, appBaseURL string) *Handler`; routes `POST /v1/billing/checkout`, `POST /v1/billing/portal` (authed, owner/admin), `POST /v1/billing/webhook` (public).

- [ ] **Step 1: Write the failing test**

Add to `test/integration/api_billing_stripe_test.go`:

```go
func TestBillingHTTP_CheckoutWebhookUnlocksPro(t *testing.T) {
	app := buildAPI(t)

	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "HttpPayCo", "company_type": "both",
		"email": uniqueEmail("pay-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := reg["token"].(string)
	companyID := reg["company_id"].(string)

	// Free before.
	st, sum := doJSON(t, app, "GET", "/v1/billing/summary", token, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "free", sum["plan"])

	// Checkout returns a URL (stub) and stores the Stripe customer id.
	st, co := doJSON(t, app, "POST", "/v1/billing/checkout", token, map[string]any{"plan": "pro", "interval": "month"})
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, co["url"].(string), "stub")

	// Stripe confirms via webhook (stub parses the normalized envelope directly).
	st, _ = doJSON(t, app, "POST", "/v1/billing/webhook", "", map[string]any{
		"id": "evt_http_" + companyID, "type": "checkout.session.completed",
		"company_id": companyID, "customer_id": "cus_stub_" + companyID,
		"subscription_id": "sub_http", "plan": "pro", "interval": "month", "status": "active",
	})
	require.Equal(t, http.StatusOK, st)

	// Now Pro: autonomy unlocked, rfq limit 100.
	st, sum = doJSON(t, app, "GET", "/v1/billing/summary", token, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "pro", sum["plan"])
	ent := sum["entitlements"].(map[string]any)
	require.Equal(t, true, ent["agent_autonomy"])
	require.Equal(t, float64(100), ent["rfq_monthly_limit"])

	// Portal deep-link works once a customer exists.
	st, portal := doJSON(t, app, "POST", "/v1/billing/portal", token, nil)
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, portal["url"].(string), "stub")

	// Cancellation downgrades to Free.
	st, _ = doJSON(t, app, "POST", "/v1/billing/webhook", "", map[string]any{
		"id": "evt_cancel_http_" + companyID, "type": "customer.subscription.deleted",
		"company_id": companyID, "customer_id": "cus_stub_" + companyID, "status": "canceled",
	})
	require.Equal(t, http.StatusOK, st)
	st, sum = doJSON(t, app, "GET", "/v1/billing/summary", token, nil)
	require.Equal(t, "free", sum["plan"])
}
```

Ensure the test file imports `net/http`.

- [ ] **Step 2: Run test to verify it fails**

Run: `TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestBillingHTTP_CheckoutWebhookUnlocksPro -v`
Expected: FAIL — 404 (routes not mounted).

- [ ] **Step 3: Write the handler**

Create `internal/billing/handler.go`:

```go
package billing

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves checkout/portal (authed) and the webhook (public).
type Handler struct {
	q          *sqlc.Queries
	provider   Billing
	svc        *Service
	appBaseURL string
}

// NewHandler builds the billing handler.
func NewHandler(q *sqlc.Queries, provider Billing, svc *Service, appBaseURL string) *Handler {
	return &Handler{q: q, provider: provider, svc: svc, appBaseURL: appBaseURL}
}

// Register mounts authed checkout/portal under the tenant guard and the PUBLIC
// webhook (Stripe calls it with a signature, not a JWT).
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/billing", guard...)
	g.Post("/checkout", middleware.RequireRole("owner", "admin"), h.checkout)
	g.Post("/portal", middleware.RequireRole("owner", "admin"), h.portal)

	app.Post("/v1/billing/webhook", h.webhook) // PUBLIC — signature-verified inside
}

type checkoutReq struct {
	Plan     string `json:"plan"`
	Interval string `json:"interval"`
}

func (h *Handler) checkout(c *fiber.Ctx) error {
	var req checkoutReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Plan != "pro" && req.Plan != "business" {
		return fiber.NewError(fiber.StatusBadRequest, "plan must be pro|business")
	}
	if req.Interval != "month" && req.Interval != "year" {
		return fiber.NewError(fiber.StatusBadRequest, "interval must be month|year")
	}

	ctx := c.Context()
	companyID := middleware.CompanyID(c)

	co, err := h.q.GetCompanyByID(ctx, companyID)
	if err != nil {
		return err
	}
	existing := ""
	if co.StripeCustomerID != nil {
		existing = *co.StripeCustomerID
	}
	email := h.callerEmail(c)

	customerID, err := h.provider.EnsureCustomer(ctx, companyID, email, existing)
	if err != nil {
		return err
	}
	if existing == "" && customerID != "" {
		if err := h.q.SetCompanyStripeCustomer(ctx, sqlc.SetCompanyStripeCustomerParams{
			ID: companyID, StripeCustomerID: &customerID,
		}); err != nil {
			return err
		}
	}

	url, err := h.provider.CreateCheckoutSession(ctx, CheckoutParams{
		CompanyID: companyID, CustomerID: customerID, CustomerEmail: email,
		Plan: req.Plan, Interval: req.Interval,
		SuccessURL: h.appBaseURL + "/settings/billing?checkout=success",
		CancelURL:  h.appBaseURL + "/settings/billing?checkout=cancel",
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"url": url})
}

func (h *Handler) portal(c *fiber.Ctx) error {
	ctx := c.Context()
	co, err := h.q.GetCompanyByID(ctx, middleware.CompanyID(c))
	if err != nil {
		return err
	}
	if co.StripeCustomerID == nil || *co.StripeCustomerID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "no billing account yet")
	}
	url, err := h.provider.CreatePortalSession(ctx, *co.StripeCustomerID, h.appBaseURL+"/settings/billing")
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"url": url})
}

func (h *Handler) webhook(c *fiber.Ctx) error {
	ev, err := h.provider.ParseWebhook(c.Body(), c.Get("Stripe-Signature"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid webhook")
	}
	if err := h.svc.ApplyWebhookEvent(c.Context(), ev); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"received": true})
}

// callerEmail best-effort fetches the acting user's email for the Stripe customer.
func (h *Handler) callerEmail(c *fiber.Ctx) string {
	u, err := h.q.GetUserByID(c.Context(), middleware.UserID(c))
	if err != nil || errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	return u.Email
}
```

**Note:** `co.StripeCustomerID` is `*string` (nullable column). If sqlc typed it differently, adjust. `GetCompanyByID`, `GetUserByID` already exist.

- [ ] **Step 4: Thread config through the router**

In `internal/httpapi/router.go` `Config` struct, add:

```go
	// Stripe billing (Plan 2). Empty SecretKey => StubBilling.
	StripeSecretKey     string
	StripeWebhookSecret string
	StripePriceProMonth      string
	StripePriceProYear       string
	StripePriceBusinessMonth string
	StripePriceBusinessYear  string
```

In `New(...)`, after `entSvc := entitlements.NewService(q, cfg.Logger)` and its handler registration, construct + register billing (add the import `"github.com/tradebridge/core/internal/billing"`):

```go
	billingProvider := billing.NewBilling(billing.StripeConfig{
		SecretKey:     cfg.StripeSecretKey,
		WebhookSecret: cfg.StripeWebhookSecret,
		Prices: billing.Prices{
			ProMonth:      cfg.StripePriceProMonth,
			ProYear:       cfg.StripePriceProYear,
			BusinessMonth: cfg.StripePriceBusinessMonth,
			BusinessYear:  cfg.StripePriceBusinessYear,
		},
	}, cfg.Logger)
	billingSvc := billing.NewService(q, cfg.Logger)
	billing.NewHandler(q, billingProvider, billingSvc, appBaseURLOr(cfg.AppBaseURL)).Register(f, guard...)
```

Add a small helper near the bottom of the file (or inline the default):

```go
// appBaseURLOr returns the configured base URL or the local dev default.
func appBaseURLOr(u string) string {
	if u == "" {
		return "http://localhost:3010"
	}
	return u
}
```

- [ ] **Step 5: Map config in cmd/api**

In `cmd/api/main.go`, add to the `httpapi.Config{...}` literal:

```go
		StripeSecretKey:          app.Config.StripeSecretKey,
		StripeWebhookSecret:      app.Config.StripeWebhookSecret,
		StripePriceProMonth:      app.Config.StripePriceProMonth,
		StripePriceProYear:       app.Config.StripePriceProYear,
		StripePriceBusinessMonth: app.Config.StripePriceBusinessMonth,
		StripePriceBusinessYear:  app.Config.StripePriceBusinessYear,
```

Check `cmd/gateway/` for a second `httpapi.New` call; if present, it will get billing routes too (harmless — stub with empty keys). No change needed unless it constructs `httpapi.Config` and you want parity; leave it if it doesn't set billing (empty => stub).

- [ ] **Step 6: Run the E2E + full suite**

Run:
```bash
export DB_URL="postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable"
go build ./... && go vet ./...
TEST_DATABASE_URL="$DB_URL" go test ./test/integration/ -run TestBillingHTTP_CheckoutWebhookUnlocksPro -v
TEST_DATABASE_URL="$DB_URL" go test ./...
```
Expected: the E2E passes (Free → checkout → webhook → Pro → portal → cancel → Free); full suite green.

- [ ] **Step 7: Commit**

```bash
git add internal/billing/handler.go internal/httpapi/router.go cmd/api/main.go test/integration/api_billing_stripe_test.go
git commit -m "feat(billing): checkout/portal/webhook endpoints + wiring (Free->Pro via webhook)"
```

---

## Self-Review (completed by plan author)

- **Spec coverage:** Stripe customers + subscriptions + webhooks (idempotent, signed) ✓ (Tasks 1–4); plans monthly+annual via price ids ✓ (Prices map); Customer Portal deep-links ✓ (portal); dunning/grace/downgrade ✓ (status honored set + canceled→Free, Task 2 test); checkout flow ✓. **Deferred (correctly):** Stripe Tax verification (flag built, needs live account); trials (the `trialing` status is honored — creating a trial is a Stripe-config/price concern at activation); the frontend (Plan 4); the agent autonomy consumption (Plan 3). Stripe Tax + real dunning retries are activation follow-ups, documented.
- **Placeholder scan:** all steps carry complete code. The single knowingly-adaptable file is `stripebilling.go` (Task 3) — explicitly flagged as version-dependent with `go build`/`go doc` as the arbiter and the `Billing` contract fixed; its logic is not exercised by stub tests, so adaptation risk is contained.
- **Type consistency:** `WebhookEvent`, `CheckoutParams`, `Billing`, `Service`, `NewBilling`, `Prices`, `StripeConfig` are consistent across tasks. Nullable columns (`stripe_customer_id`, `stripe_subscription_id`, `current_period_end`) are `*` pointers in sqlc — flagged at each construction site with "adjust to generated types; `go build` is the arbiter."
- **Cross-plan seam:** entitlements resolution is untouched — billing only writes `subscriptions`; the `GetActiveSubscription` status filter from Plan 1 is the single source of the grace/downgrade semantics. Plan 3 (agent gateway) will read `agent_autonomy`, which flips the instant a Pro webhook lands (verified in the Task 2 + Task 4 tests).
- **Gate-pattern reminder honored:** no `entitlements.Fail(c, Check())` misuse here (billing has no quota gate); checkout is role-gated via `middleware.RequireRole`, matching the invite pattern.

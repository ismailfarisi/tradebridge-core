# Sprint 7 · Plan 1 — Core Activation Spine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repo: `/home/deploy/b2b/tradebridge-core` (Go/Fiber, sqlc, golang-migrate, pgx/v5). Commit per task.

**Goal:** A `GET /v1/activation` endpoint that reports each onboarding step's real state, plus an
`activation_events` table + a best-effort `Emit` helper fired at real activation milestones (the data
Sprint 8's funnels consume).

**Architecture:** Migration 027 adds `activation_events` (UNIQUE per company+event). A new
`internal/activation` package holds: `GetActivationSignals` (one query of all step signals),
`Service.Status(role, signals)` (builds the ordered step list + percent), the `GET /v1/activation` handler,
and a free-function `Emit(ctx, q, companyID, userID, event, props)` (INSERT ... ON CONFLICT DO NOTHING,
errors swallowed). Nine existing handlers each gain one `Emit(...)` line at their milestone.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5, golang-migrate.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**. `.env`/secrets never committed.
- DB tests → `TEST_DATABASE_URL` (`tradebridge_core_test`) only. Setup for every shell:
  `export PATH="$HOME/.local/bin:$PATH"; export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- **Emit is best-effort:** it must NEVER break the request it rides on — swallow all errors, and at
  transactional call sites emit AFTER the surrounding tx commits, using the pool-backed `*sqlc.Queries`
  (never the tx handle), so a duplicate-milestone conflict or emit failure cannot roll back real work.
- New migration number is **027** (test DB at v26, live at v24).
- After editing `.sql`, `sqlc generate`; match generated types exactly (don't guess).
- Full suite green under `go test ./... -p 1`.

## Reference (verified emit sites + signals)

- Register: `internal/identity/handler.go:register` — after `CreateCompany`/`CreateUserWithPassword`; has
  `h.q`, `company.ID`, `user.ID`, `req.CompanyType`.
- Profile: `internal/suppliers/handler.go:upsertMine` — after `UpsertSupplierProfile`; `middleware.CompanyID(c)`.
- Catalog: `internal/catalog/handler.go:create` — after create; `middleware.CompanyID(c)`.
- Verified: `internal/trust/verification.go` — `Check` (after `SetCompanyVerified`, ~line 208, post-commit)
  and `SetStatus` (when `status=="verified"`, ~line 248); Service has `s.q`.
- Agent connected: `internal/gateway/gateway.go:heartbeat` — after `TouchAgentCredentialLastSeen`; has `h.q`,
  `agent.CompanyID`.
- First RFQ: `internal/rfq/handler.go:create` — after `svc.CreateAndMatch`; `middleware.CompanyID(c)`,
  `middleware.UserID(c)`.
- First quote: `internal/quotes/service.go` — the seller quote submit path (`Record`), after the tx commits;
  `sellerCompanyID`.
- First order: `internal/quotes/service.go:Accept` — after the order tx commits (~line 441 creates the
  order); emit keyed on the BUYER (`order.BuyerCompanyID`).
- Upgrade: `internal/billing/` — where a subscription becomes active on a paid plan (find the service method
  that writes the active paid subscription; emit `upgrade` for that company). If the exact site is unclear,
  emit in the billing webhook handler after a paid subscription is persisted.
- Company type (role): `q.GetCompanyByID(ctx, companyID).Type` ∈ `buyer|seller|both`.

## File Structure

- Create: `pkg/db/migrations/027_activation_events.up.sql`, `.down.sql`
- Create: `pkg/db/query/activation.sql` (InsertActivationEvent, GetActivationSignals)
- Create: `internal/activation/emit.go` (`Emit`), `internal/activation/service.go` (`Service`, steps),
  `internal/activation/handler.go` (`GET /v1/activation`), `internal/activation/activation_test.go`
- Modify: `internal/httpapi/router.go` (mount handler)
- Modify (Task 3): the nine emit-site files above

---

### Task 1: Migration 027 + queries

**Files:** `pkg/db/migrations/027_activation_events.up.sql`, `.down.sql`, `pkg/db/query/activation.sql`.

**Interfaces:**
- Produces: `activation_events` table; `sqlc.InsertActivationEvent(ctx, params)`,
  `sqlc.GetActivationSignals(ctx, companyID)`.

- [ ] **Step 1: `027_activation_events.up.sql`:**
```sql
-- Sprint 7: activation funnel events. One row per (company, milestone) — the
-- UNIQUE makes Emit idempotent so funnels count first-reach, not repeats.
CREATE TABLE activation_events (
    id         TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id TEXT NOT NULL REFERENCES companies(id),
    user_id    TEXT REFERENCES users(id),
    event      TEXT NOT NULL,
    props      JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (company_id, event)
);

CREATE INDEX idx_activation_events_funnel ON activation_events (event, created_at);
```

- [ ] **Step 2: `027_activation_events.down.sql`:**
```sql
DROP TABLE IF EXISTS activation_events;
```

- [ ] **Step 3: `pkg/db/query/activation.sql`:**
```sql
-- name: InsertActivationEvent :exec
-- Idempotent milestone record; the UNIQUE(company_id,event) makes a repeat a no-op.
INSERT INTO activation_events (company_id, user_id, event, props)
VALUES (@company_id, @user_id, @event, @props)
ON CONFLICT (company_id, event) DO NOTHING;

-- name: GetActivationSignals :one
-- All onboarding-step signals for a company in one round-trip. (verification_status
-- is read from the company row in Go — Service.Status already fetches it — so it is
-- intentionally NOT selected here, avoiding a nullable scalar-subquery type.)
SELECT
    EXISTS (SELECT 1 FROM supplier_profiles sp
            WHERE sp.company_id = @company_id
              AND COALESCE(sp.description, '') <> ''
              AND COALESCE(array_length(sp.categories, 1), 0) >= 1) AS has_profile,
    (SELECT COUNT(*) FROM catalog_summaries WHERE company_id = @company_id)::bigint AS catalog_count,
    EXISTS (SELECT 1 FROM agent_credentials
            WHERE company_id = @company_id AND status = 'active'
              AND (expires_at IS NULL OR expires_at > NOW())
              AND last_seen IS NOT NULL) AS agent_connected,
    (SELECT COUNT(*) FROM quotes WHERE seller_company_id = @company_id)::bigint AS quotes_submitted,
    (SELECT COUNT(*) FROM rfqs WHERE buyer_company_id = @company_id)::bigint AS rfqs_posted,
    (SELECT COUNT(*) FROM quotes q JOIN rfqs r ON r.id = q.rfq_id
       WHERE r.buyer_company_id = @company_id)::bigint AS quotes_received,
    (SELECT COUNT(*) FROM orders WHERE buyer_company_id = @company_id)::bigint AS orders_as_buyer;
```

- [ ] **Step 4: Apply + round-trip + generate:**
```bash
export PATH="$HOME/.local/bin:$PATH"
export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" version   # expect 27
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" down 1 && migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
sqlc generate && go build ./...
```
Expected: version 27, clean round-trip, build exit 0. Confirm `GetActivationSignalsRow` field types
(`HasProfile bool`, `CatalogCount int64`, `VerificationStatus` likely `string` or `*string`,
`AgentConnected bool`, the four counts `int64`).

- [ ] **Step 5: Commit** — `feat(core): migration 027 — activation_events + signals query`.

---

### Task 2: activation package (Emit + Service + endpoint) + unit tests

**Files:** `internal/activation/{emit,service,handler,activation_test}.go`, `internal/httpapi/router.go`.

**Interfaces:**
- Produces: `activation.Emit(ctx, q, companyID, userID, event string, props map[string]any)`;
  `activation.NewService(q).Status(ctx, companyID) (Status, error)`; `GET /v1/activation`.

- [ ] **Step 1: `internal/activation/emit.go`:**
```go
// Package activation computes onboarding progress and records funnel milestones.
package activation

import (
	"context"
	"encoding/json"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Milestone event names (funnel vocabulary).
const (
	EventSignup         = "signup"
	EventProfileSet     = "profile_set"
	EventCatalogAdded   = "catalog_added"
	EventVerified       = "verified"
	EventAgentConnected = "agent_connected"
	EventFirstRFQ       = "first_rfq"
	EventFirstQuote     = "first_quote"
	EventFirstOrder     = "first_order"
	EventUpgrade        = "upgrade"
)

// Emit records a milestone for a company. Best-effort and idempotent: the
// UNIQUE(company_id,event) makes repeats a no-op, and ALL errors are swallowed
// so a failed emit never breaks the request it rides on. Call it AFTER the
// surrounding write/tx has committed, with the pool-backed queries.
func Emit(ctx context.Context, q *sqlc.Queries, companyID, userID, event string, props map[string]any) {
	if q == nil || companyID == "" || event == "" {
		return
	}
	raw := []byte("{}")
	if props != nil {
		if b, err := json.Marshal(props); err == nil {
			raw = b
		}
	}
	var uid *string
	if userID != "" {
		uid = &userID
	}
	_ = q.InsertActivationEvent(ctx, sqlc.InsertActivationEventParams{
		CompanyID: companyID,
		UserID:    uid,
		Event:     event,
		Props:     raw,
	})
}
```
> If `InsertActivationEventParams.Props` generates as `[]byte` (pgtype `jsonb` → `[]byte`), the above fits.
> If it generates as `pgtype.JSONB`/other, adapt the assignment to the generated type.

- [ ] **Step 2: `internal/activation/service.go`:**
```go
package activation

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Step is a single onboarding step.
type Step struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Done    bool   `json:"done"`
	CTAHref string `json:"cta_href"`
	Hint    string `json:"hint"`
}

// Status is the activation snapshot for a company.
type Status struct {
	Role            string `json:"role"`
	PercentComplete int    `json:"percent_complete"`
	Steps           []Step `json:"steps"`
}

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

// Status computes the ordered step list + percent for a company. sellers/both
// get the seller path; buyers get the buyer path.
func (s *Service) Status(ctx context.Context, companyID string) (Status, error) {
	company, err := s.q.GetCompanyByID(ctx, companyID)
	if err != nil {
		return Status{}, err
	}
	sig, err := s.q.GetActivationSignals(ctx, companyID)
	if err != nil {
		return Status{}, err
	}
	role := "buyer"
	if company.Type == "seller" || company.Type == "both" {
		role = "seller"
	}
	steps := buildSteps(role, company.VerificationStatus, sig)
	done := 0
	for _, st := range steps {
		if st.Done {
			done++
		}
	}
	pct := 0
	if len(steps) > 0 {
		pct = int(float64(done) / float64(len(steps)) * 100.0)
	}
	return Status{Role: role, PercentComplete: pct, Steps: steps}, nil
}

func buildSteps(role, verificationStatus string, sig sqlc.GetActivationSignalsRow) []Step {
	verified := verificationStatus == "verified"
	if role == "seller" {
		return []Step{
			{Key: "profile", Label: "Complete your profile", Done: sig.HasProfile, CTAHref: "/seller/profile", Hint: "Add a description and at least one category."},
			{Key: "catalog", Label: "Add catalog items", Done: sig.CatalogCount > 0, CTAHref: "/seller/catalog", Hint: "Upload products so your agent can quote."},
			{Key: "verified", Label: "Verify your domain", Done: verified, CTAHref: "/settings/verification", Hint: "Verify to earn the trust badge and rank higher."},
			{Key: "agent_connected", Label: "Connect your agent", Done: sig.AgentConnected, CTAHref: "/seller/agent", Hint: "Issue a credential and connect your seller agent."},
			{Key: "first_quote", Label: "Send your first quote", Done: sig.QuotesSubmitted > 0, CTAHref: "/seller/agent", Hint: "Respond to an incoming RFQ to go live."},
		}
	}
	return []Step{
		{Key: "first_rfq", Label: "Post your first RFQ", Done: sig.RfqsPosted > 0, CTAHref: "/chat", Hint: "Describe what you need in plain language."},
		{Key: "received_quote", Label: "Receive a quote", Done: sig.QuotesReceived > 0, CTAHref: "/rfqs", Hint: "Sellers' agents quote your matched RFQs."},
		{Key: "first_order", Label: "Place your first order", Done: sig.OrdersAsBuyer > 0, CTAHref: "/rfqs", Hint: "Accept a quote to create a purchase order."},
	}
}
```
> `company.VerificationStatus` is a plain `string` on the generated `Company` model (verified confirmed), so
> `verificationStatus == "verified"` compiles directly — no helper needed.

- [ ] **Step 3: `internal/activation/handler.go`:**
```go
package activation

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/activation", guard...)
	g.Get("/", h.status)
	g.Get("", h.status)
}

func (h *Handler) status(c *fiber.Ctx) error {
	st, err := h.svc.Status(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(st)
}
```

- [ ] **Step 4: Wire in `internal/httpapi/router.go`** (near the dashboard handler registration):
```go
	activation.NewHandler(activation.NewService(q)).Register(f, guard...)
```
  Add import `"github.com/tradebridge/core/internal/activation"`.

- [ ] **Step 5: Failing tests** `internal/activation/activation_test.go` — DB-backed (mirrors the dashboard
  test harness). Cover: a fresh seller has all steps todo (percent 0); after profile+catalog+verify+agent+
  quote the seller is 100%; a buyer gets the buyer step set; `both` → seller path; `Emit` is idempotent
  (second identical emit does not error and does not duplicate).
```go
package activation

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func queriesOrSkip(t *testing.T) *sqlc.Queries {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool)
}

func TestFreshSellerHasStepsTodo(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-seller-fresh", Type: "seller"})
	require.NoError(t, err)
	st, err := NewService(q).Status(ctx, co.ID)
	require.NoError(t, err)
	require.Equal(t, "seller", st.Role)
	require.Equal(t, 0, st.PercentComplete)
	require.Len(t, st.Steps, 5)
	for _, s := range st.Steps {
		require.False(t, s.Done)
	}
}

func TestBuyerRoleSteps(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-buyer-fresh", Type: "buyer"})
	require.NoError(t, err)
	st, err := NewService(q).Status(ctx, co.ID)
	require.NoError(t, err)
	require.Equal(t, "buyer", st.Role)
	require.Equal(t, "first_rfq", st.Steps[0].Key)
}

func TestBothIsSellerPath(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-both", Type: "both"})
	require.NoError(t, err)
	st, err := NewService(q).Status(ctx, co.ID)
	require.NoError(t, err)
	require.Equal(t, "seller", st.Role)
}

func TestEmitIsIdempotent(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-emit", Type: "seller"})
	require.NoError(t, err)
	Emit(ctx, q, co.ID, "", EventSignup, map[string]any{"k": "v"})
	Emit(ctx, q, co.ID, "", EventSignup, nil) // repeat is a no-op, no error
	// A second distinct event is recorded.
	Emit(ctx, q, co.ID, "", EventProfileSet, nil)
	// (No count query is required; the assertion is that neither call panicked
	// or errored. If a ListActivationEvents query exists, assert exactly 2 rows.)
}
```
> To assert the profile→100% progression you need to set the seller's signals. Reuse existing queries:
> `UpsertSupplierProfile` (description+categories), `CreateCatalogSummary`, `SetCompanyVerified` or
> `SetCompanyVerificationStatus`, `CreateAgentCredential`+`TouchAgentCredentialLastSeen`, and `UpsertQuote`
> (needs an RFQ). Add a `TestSellerReaches100` that seeds all five and asserts `PercentComplete == 100`.
> Confirm each param struct against the generated sqlc before writing the seed.

- [ ] **Step 6: Run** `go test ./internal/activation/ -v` → PASS.
- [ ] **Step 7: Commit** — `feat(core): activation service + GET /v1/activation + Emit helper`.

---

### Task 3: Wire Emit into the nine milestone sites + idempotency proof

**Files:** `internal/identity/handler.go`, `internal/suppliers/handler.go`, `internal/catalog/handler.go`,
`internal/trust/verification.go`, `internal/gateway/gateway.go`, `internal/rfq/handler.go`,
`internal/quotes/service.go`, `internal/billing/*.go`.

**Interfaces:** each site adds one `activation.Emit(...)` call after its milestone write succeeds. Import
`"github.com/tradebridge/core/internal/activation"` in each file.

- [ ] **Step 1: register** (`internal/identity/handler.go`, after `h.issue` prep / before return, once user
  exists):
```go
	activation.Emit(ctx, h.q, company.ID, user.ID, activation.EventSignup, map[string]any{"company_type": req.CompanyType})
```

- [ ] **Step 2: profile** (`internal/suppliers/handler.go:upsertMine`, after `UpsertSupplierProfile` succeeds):
```go
	activation.Emit(c.Context(), h.q, middleware.CompanyID(c), middleware.UserID(c), activation.EventProfileSet, nil)
```

- [ ] **Step 3: catalog** (`internal/catalog/handler.go:create`, after the item is created):
```go
	activation.Emit(c.Context(), h.q, middleware.CompanyID(c), middleware.UserID(c), activation.EventCatalogAdded, nil)
```

- [ ] **Step 4: verified** (`internal/trust/verification.go`) — in `Check` after the tx commits and the
  result is `verified`, and in `SetStatus` when `status == "verified"` after commit:
```go
	activation.Emit(ctx, s.q, companyID, "", activation.EventVerified, nil)
```
> Place these AFTER the transaction commit points, using `s.q` (pool), not the `qtx` handle.

- [ ] **Step 5: agent_connected** (`internal/gateway/gateway.go:heartbeat`, after
  `TouchAgentCredentialLastSeen`):
```go
	activation.Emit(c.Context(), h.q, agent.CompanyID, "", activation.EventAgentConnected, nil)
```

- [ ] **Step 6: first_rfq** (`internal/rfq/handler.go:create`, after `CreateAndMatch` succeeds):
```go
	activation.Emit(c.Context(), h.q, middleware.CompanyID(c), middleware.UserID(c), activation.EventFirstRFQ, nil)
```

- [ ] **Step 7: first_quote + first_order** (`internal/quotes/service.go`):
  `Record` is a **free function** `func Record(ctx, q *sqlc.Queries, pool *pgxpool.Pool, rfqID,
  sellerCompanyID, commandID string, n Normalized) (sqlc.Quote, error)` (NOT a method). After its
  `tx.Commit` (~line 148), emit with the function's `q` param:
```go
	activation.Emit(ctx, q, sellerCompanyID, "", activation.EventFirstQuote, nil)
```
  `Accept` is a method `func (s *Service) Accept(...)`; after its `tx.Commit` (~line 512), the committed
  `order` var is in scope — emit keyed on the buyer using `s.q`:
```go
	activation.Emit(ctx, s.q, order.BuyerCompanyID, "", activation.EventFirstOrder, nil)
```
> Place both emits strictly AFTER the respective `tx.Commit(ctx)` — never inside the tx. `Record` uses its
> free-function `q`; `Accept` uses `s.q`.

- [ ] **Step 8: upgrade** (`internal/billing/service.go`): in `applyState`, after
  `s.q.UpsertSubscription(...)` (~line 92) succeeds, emit for the resolved company when the plan is PAID and
  active. The company id comes from `resolveCompany(ctx, ev)` (already called in `applyState`); `ev.Plan` ∈
  `free|pro|business` and `ev.Status` carries the subscription status:
```go
	if ev.Plan == "pro" || ev.Plan == "business" {
		activation.Emit(ctx, s.q, companyID, "", activation.EventUpgrade, map[string]any{"plan": ev.Plan})
	}
```
> Use the `companyID` variable already resolved in `applyState` (from `resolveCompany`); if it is named
> differently, match it. `s.q` is the billing service's queries handle. Do NOT emit for the free tier.

- [ ] **Step 9: Emit-idempotency integration proof.** Add to `internal/activation/activation_test.go` (or a
  focused test) that emitting the same (company,event) twice leaves exactly one row. If no list/count query
  exists, add a tiny test-only query `CountActivationEvents` or query the table via the pool directly:
```go
func TestEmitDedupesPerCompanyEvent(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "act-dedupe", Type: "seller"})
	require.NoError(t, err)
	Emit(ctx, q, co.ID, "", EventFirstQuote, nil)
	Emit(ctx, q, co.ID, "", EventFirstQuote, map[string]any{"again": true})
	// Query the table directly to assert exactly one row for (co, first_quote).
	// Use a raw pool query or a CountActivationEvents sqlc query if you add one.
}
```
  (Adding a `-- name: CountActivationEventsFor :one` query `SELECT COUNT(*) FROM activation_events WHERE
  company_id=@company_id AND event=@event` is the clean way — regenerate and assert `== 1`.)

- [ ] **Step 10: Build + full suite:**
```bash
go build ./... && go test ./... -p 1
```
Expected: green (existing register/quote/order/verification tests still pass — emits are additive and
best-effort).

- [ ] **Step 11: Commit** — `feat(core): emit activation milestones at signup/profile/catalog/verify/agent/rfq/quote/order/upgrade`.

---

## Self-Review checklist

- Migration 027 round-trips; `activation_events` UNIQUE(company_id,event); funnel index present.
- `GET /v1/activation` returns role-correct ordered steps + percent; `both` → seller path; each step has a
  cta_href + hint; percent = round(done/total*100).
- `Emit` is best-effort (swallows errors), idempotent (ON CONFLICT), and called AFTER commits with the
  pool `*sqlc.Queries` — proven by the dedupe test.
- All nine milestones wired; existing tests still green; emits never alter request success/failure.
- Full suite green under `-p 1`; nothing pushed.

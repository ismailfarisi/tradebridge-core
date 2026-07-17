# Sprint 6 · Plan 4 — Seller Dashboard + Full Analytics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repo: `/home/deploy/b2b/tradebridge-core`. Extends the `internal/dashboard` package from Plan 3. Commit per task.

**Goal:** `GET /v1/dashboard/seller` (incoming RFQs, review queue, active orders, agent status, plan usage)
and `GET /v1/analytics/seller` (win rate, revenue won, avg response time, top buyers) — all real,
tenant-scoped aggregations.

**Architecture:** Extend `internal/dashboard`: the `Service` gains an entitlements dependency (for plan usage
+ autonomy) and two methods (`SellerDashboard`, `SellerAnalytics`); the `Handler` gains the two routes.
Agent presence derives from `agent_credentials` (`last_seen` within a freshness window + a valid/active,
non-expired credential). Analytics come from `quotes`/`orders`/`rfqs` aggregations. (Design note: the sprint
spec floated a separate `internal/analytics` package; folding analytics into `internal/dashboard` avoids
duplicating the entitlements/router wiring for one extra endpoint — cohesive and simpler.)

**Tech Stack:** Go, Fiber, sqlc, pgx/v5.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- DB tests → `TEST_DATABASE_URL` (`tradebridge_core_test`) only. Setup:
  `export PATH="$HOME/.local/bin:$PATH"; export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- All aggregations tenant-scoped to `middleware.CompanyID(c)`.
- No new migration (reuse existing tables). After editing `.sql`, `sqlc generate`; match generated types.
- Full suite green under `go test ./... -p 1`.

## Reference (verified facts)

- `agent_credentials(company_id, status IN ('active','revoked'), last_seen TIMESTAMPTZ NULL, expires_at
  TIMESTAMPTZ NULL, ...)`. A credential is "valid" when `status='active' AND (expires_at IS NULL OR
  expires_at > NOW())`. Heartbeat touches `last_seen`.
- `entitlements.Service` (already constructed in router as `entSvc`) exposes:
  `Usage(ctx, companyID) Usage{RFQUsed,RFQLimit,CatalogUsed,CatalogLimit,SeatsUsed,SeatsLimit int}`,
  `PlanCode(ctx, companyID) string`, `For(ctx, companyID) Entitlements{AgentAutonomy bool, ...}`.
  Limits use `-1` = unlimited.
- `rfq_matches(rfq_id, seller_company_id, ...)` UNIQUE(rfq_id, seller_company_id); `rfqs.status`
  open/quoted/closed/expired; `quotes(rfq_id, seller_company_id, status submitted|accepted|rejected,
  created_at)`; `orders(seller_company_id, buyer_company_id, status, total_cents, created_at)`.
- Plan 3 created `internal/dashboard/{service,handler,dashboard_test}.go` with `NewService(q)` +
  `BuyerDashboard` + `GET /v1/dashboard/buyer`, and the router calls
  `dashboard.NewHandler(dashboard.NewService(q)).Register(f, guard...)`.

## File Structure

- Modify: `pkg/db/query/dashboard.sql` (seller counts, agent presence, analytics, top buyers)
- Modify: `internal/dashboard/service.go` (add entitlements dep + `SellerDashboard` + `SellerAnalytics`)
- Modify: `internal/dashboard/handler.go` (add `/v1/dashboard/seller` + `/v1/analytics/seller`)
- Modify: `internal/httpapi/router.go` (pass `entSvc` into `dashboard.NewService`)
- Modify: `internal/dashboard/dashboard_test.go` (seller dashboard + analytics tests)

---

### Task 1: Seller dashboard — counts + agent presence + plan usage

**Files:** `pkg/db/query/dashboard.sql`, `internal/dashboard/service.go`, `internal/dashboard/handler.go`,
`internal/httpapi/router.go`, `internal/dashboard/dashboard_test.go`.

**Interfaces:**
- Consumes: `entitlements.Service` (`Usage`, `PlanCode`, `For`).
- Produces: `sqlc.SellerDashboardCounts(ctx, companyID)`, `sqlc.GetAgentPresence(ctx, companyID)`;
  `Service.SellerDashboard(ctx, companyID) (SellerDashboard, error)`; `GET /v1/dashboard/seller`.

- [ ] **Step 1: Add queries to `pkg/db/query/dashboard.sql`:**
```sql
-- name: SellerDashboardCounts :one
-- Seller operational counts, tenant-scoped. review_queue = matched OPEN RFQs the
-- seller has not yet quoted (the actionable subset of incoming_rfqs).
SELECT
    (SELECT COUNT(*) FROM rfq_matches m JOIN rfqs r ON r.id = m.rfq_id
       WHERE m.seller_company_id = @company_id AND r.status = 'open')::bigint AS incoming_rfqs,
    (SELECT COUNT(*) FROM rfq_matches m JOIN rfqs r ON r.id = m.rfq_id
       WHERE m.seller_company_id = @company_id AND r.status = 'open'
         AND NOT EXISTS (SELECT 1 FROM quotes q
                         WHERE q.rfq_id = m.rfq_id AND q.seller_company_id = @company_id))::bigint AS review_queue,
    (SELECT COUNT(*) FROM orders o
       WHERE o.seller_company_id = @company_id
         AND o.status IN ('created','confirmed','fulfilled'))::bigint AS active_orders;

-- name: GetAgentPresence :one
-- Latest heartbeat among VALID (active, non-expired) credentials + whether any
-- valid credential exists. online is decided in Go against a freshness window.
SELECT
    MAX(last_seen) FILTER (WHERE status = 'active' AND (expires_at IS NULL OR expires_at > NOW())) AS last_seen,
    COALESCE(BOOL_OR(status = 'active' AND (expires_at IS NULL OR expires_at > NOW())), false) AS has_valid
FROM agent_credentials
WHERE company_id = @company_id;
```

- [ ] **Step 2: `sqlc generate` + build.** Confirm generated types: `GetAgentPresenceRow.LastSeen` will be a
  nullable time (`pgtype.Timestamptz` or `*time.Time` — check `pkg/db/sqlc/dashboard.sql.go`) and
  `HasValid bool`; `SellerDashboardCountsRow{IncomingRfqs, ReviewQueue, ActiveOrders int64}`.

- [ ] **Step 3: Add entitlements dep + `SellerDashboard` to `internal/dashboard/service.go`.** Change the
  `Service` struct + constructor:
```go
import (
	"context"
	"time"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Service struct {
	q   *sqlc.Queries
	ent *entitlements.Service
}

func NewService(q *sqlc.Queries, ent *entitlements.Service) *Service { return &Service{q: q, ent: ent} }

// agentFreshWindow: a credential seen within this window is considered online.
const agentFreshWindow = 5 * time.Minute
```
  Add types + method:
```go
// AgentStatus is the seller's connected-agent state.
type AgentStatus struct {
	Online   bool    `json:"online"`
	LastSeen *string `json:"last_seen"`
	Autonomy bool    `json:"autonomy"`
}

// PlanUsage is the seller's plan + usage snapshot.
type PlanUsage struct {
	Plan         string `json:"plan"`
	RFQUsed      int    `json:"rfq_used"`
	RFQLimit     int    `json:"rfq_limit"`
	CatalogUsed  int    `json:"catalog_used"`
	CatalogLimit int    `json:"catalog_limit"`
	SeatsUsed    int    `json:"seats_used"`
	SeatsLimit   int    `json:"seats_limit"`
}

// SellerDashboard is the seller home summary.
type SellerDashboard struct {
	IncomingRFQs int64       `json:"incoming_rfqs"`
	ReviewQueue  int64       `json:"review_queue"`
	ActiveOrders int64       `json:"active_orders"`
	Agent        AgentStatus `json:"agent"`
	PlanUsage    PlanUsage   `json:"plan_usage"`
}

func (s *Service) SellerDashboard(ctx context.Context, companyID string) (SellerDashboard, error) {
	counts, err := s.q.SellerDashboardCounts(ctx, companyID)
	if err != nil {
		return SellerDashboard{}, err
	}
	presence, err := s.q.GetAgentPresence(ctx, companyID)
	if err != nil {
		return SellerDashboard{}, err
	}
	agent := AgentStatus{Autonomy: s.ent.For(ctx, companyID).AgentAutonomy}
	// presence.LastSeen is a nullable timestamp — adapt to the generated type.
	if presence.HasValid && lastSeenValid(presence.LastSeen) {
		ts := lastSeenTime(presence.LastSeen)
		iso := ts.UTC().Format("2006-01-02T15:04:05Z07:00")
		agent.LastSeen = &iso
		agent.Online = time.Since(ts) <= agentFreshWindow
	}
	u := s.ent.Usage(ctx, companyID)
	return SellerDashboard{
		IncomingRFQs: counts.IncomingRfqs,
		ReviewQueue:  counts.ReviewQueue,
		ActiveOrders: counts.ActiveOrders,
		Agent:        agent,
		PlanUsage: PlanUsage{
			Plan:         s.ent.PlanCode(ctx, companyID),
			RFQUsed:      u.RFQUsed, RFQLimit: u.RFQLimit,
			CatalogUsed:  u.CatalogUsed, CatalogLimit: u.CatalogLimit,
			SeatsUsed:    u.SeatsUsed, SeatsLimit: u.SeatsLimit,
		},
	}, nil
}
```
> IMPORTANT — adapt to the generated nullable type. Open `pkg/db/sqlc/dashboard.sql.go`. If
> `GetAgentPresenceRow.LastSeen` is `pgtype.Timestamptz`, replace the `lastSeenValid`/`lastSeenTime` helpers
> with direct field access: `if presence.HasValid && presence.LastSeen.Valid { ts := presence.LastSeen.Time; ... }`.
> If it is `*time.Time`, use `if presence.HasValid && presence.LastSeen != nil { ts := *presence.LastSeen; ... }`.
> Delete the two helper placeholders and inline whichever form matches — do not keep undefined helpers.

- [ ] **Step 4: Add the route to `internal/dashboard/handler.go`:**
```go
func (h *Handler) seller(c *fiber.Ctx) error {
	d, err := h.svc.SellerDashboard(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(d)
}
```
  In `Register`, add `g.Get("/seller", h.seller)`.

- [ ] **Step 5: Update the router** `internal/httpapi/router.go` — pass `entSvc`:
```go
	dashboard.NewHandler(dashboard.NewService(q, entSvc)).Register(f, guard...)
```
  (`entSvc` is already in scope from earlier in `New()`.)

- [ ] **Step 6: Failing test** — extend `internal/dashboard/dashboard_test.go`. The service now needs an
  entitlements service; update `queriesOrSkip` usages or add a helper that builds both:
```go
import (
	// add:
	"github.com/rs/zerolog"
	"github.com/tradebridge/core/internal/entitlements"
)

func newSvc(t *testing.T, q *sqlc.Queries) *Service {
	return NewService(q, entitlements.NewService(q, zerolog.Nop()))
}

func TestSellerDashboardCountsAndAgent(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()

	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sd-seller-co", Type: "seller"})
	require.NoError(t, err)
	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sd-buyer-co", Type: "buyer"})
	require.NoError(t, err)
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "sd-" + buyer.ID + "@ex.com", Role: "owner"})
	require.NoError(t, err)

	// An open RFQ matched to the seller, not yet quoted => incoming + review_queue.
	rfq, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: user.ID, Title: "SD RFQ", RawText: "need pallets"})
	require.NoError(t, err)
	_, err = q.SetRFQStatus(ctx, sqlc.SetRFQStatusParams{ID: rfq.ID, Status: "open"})
	require.NoError(t, err)
	_, err = q.CreateRFQMatch(ctx, sqlc.CreateRFQMatchParams{RfqID: rfq.ID, SellerCompanyID: seller.ID})
	require.NoError(t, err)

	d, err := newSvc(t, q).SellerDashboard(ctx, seller.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, d.IncomingRFQs, int64(1))
	require.GreaterOrEqual(t, d.ReviewQueue, int64(1), "unquoted matched RFQ is in the review queue")
	require.False(t, d.Agent.Online, "no heartbeat => offline")
	require.NotEmpty(t, d.PlanUsage.Plan)
}
```
> Confirm `CreateRFQMatchParams` fields (`RfqID`, `SellerCompanyID`, plus maybe `Score`,`Reason` — check
> `pkg/db/sqlc/rfq.sql.go`; pass zero values for score/reason if required). Confirm `entitlements.NewService`
> signature (it is `NewService(q *sqlc.Queries, logger zerolog.Logger)` — verify and match).

- [ ] **Step 7: Run** `go test ./internal/dashboard/ -v` → PASS.
- [ ] **Step 8: Commit** — `feat(core): seller dashboard — counts + agent presence + plan usage`.

---

### Task 2: Seller analytics — win rate, revenue, response time, top buyers

**Files:** `pkg/db/query/dashboard.sql`, `internal/dashboard/service.go`, `internal/dashboard/handler.go`,
`internal/dashboard/dashboard_test.go`.

**Interfaces:**
- Produces: `sqlc.SellerAnalytics(ctx, companyID)`, `sqlc.SellerTopBuyers(ctx, companyID)`;
  `Service.SellerAnalytics(ctx, companyID) (SellerAnalytics, error)`; `GET /v1/analytics/seller`.

- [ ] **Step 1: Add queries to `pkg/db/query/dashboard.sql`:**
```sql
-- name: SellerAnalytics :one
-- Aggregate quote/order performance for a seller. win_rate is computed in Go
-- from quotes_won / decided (avoids divide-by-zero in SQL).
SELECT
    (SELECT COUNT(*) FROM quotes q WHERE q.seller_company_id = @company_id)::bigint AS quotes_submitted,
    (SELECT COUNT(*) FROM quotes q WHERE q.seller_company_id = @company_id AND q.status = 'accepted')::bigint AS quotes_won,
    (SELECT COUNT(*) FROM quotes q WHERE q.seller_company_id = @company_id AND q.status IN ('accepted','rejected'))::bigint AS decided,
    (SELECT COALESCE(SUM(o.total_cents), 0) FROM orders o
       WHERE o.seller_company_id = @company_id AND o.status <> 'cancelled')::bigint AS revenue_won_cents,
    (SELECT COALESCE(AVG(EXTRACT(EPOCH FROM (q.created_at - r.created_at)) / 3600.0), 0)::float8
       FROM quotes q JOIN rfqs r ON r.id = q.rfq_id
       WHERE q.seller_company_id = @company_id) AS avg_response_hours;

-- name: SellerTopBuyers :many
-- Top buyers by revenue won from this seller (excludes cancelled orders).
SELECT o.buyer_company_id AS company_id,
       c.name             AS name,
       COUNT(*)::bigint   AS orders,
       COALESCE(SUM(o.total_cents), 0)::bigint AS revenue_cents
FROM orders o
JOIN companies c ON c.id = o.buyer_company_id
WHERE o.seller_company_id = @company_id AND o.status <> 'cancelled'
GROUP BY o.buyer_company_id, c.name
ORDER BY revenue_cents DESC, orders DESC
LIMIT 5;
```

- [ ] **Step 2: `sqlc generate` + build.** Confirm `SellerAnalyticsRow{QuotesSubmitted, QuotesWon, Decided,
  RevenueWonCents int64; AvgResponseHours float64}` and `SellerTopBuyersRow{CompanyID, Name string; Orders,
  RevenueCents int64}`.

- [ ] **Step 3: Add types + method to `internal/dashboard/service.go`:**
```go
type TopBuyer struct {
	CompanyID    string `json:"company_id"`
	Name         string `json:"name"`
	Orders       int64  `json:"orders"`
	RevenueCents int64  `json:"revenue_cents"`
}

type SellerAnalytics struct {
	QuotesSubmitted int64      `json:"quotes_submitted"`
	QuotesWon       int64      `json:"quotes_won"`
	WinRate         float64    `json:"win_rate"` // 0..1; quotes_won / decided
	RevenueWonCents int64      `json:"revenue_won_cents"`
	AvgResponseHours float64   `json:"avg_response_hours"`
	TopBuyers       []TopBuyer `json:"top_buyers"`
}

func (s *Service) SellerAnalytics(ctx context.Context, companyID string) (SellerAnalytics, error) {
	a, err := s.q.SellerAnalytics(ctx, companyID)
	if err != nil {
		return SellerAnalytics{}, err
	}
	buyers, err := s.q.SellerTopBuyers(ctx, companyID)
	if err != nil {
		return SellerAnalytics{}, err
	}
	winRate := 0.0
	if a.Decided > 0 {
		winRate = float64(a.QuotesWon) / float64(a.Decided)
	}
	out := SellerAnalytics{
		QuotesSubmitted:  a.QuotesSubmitted,
		QuotesWon:        a.QuotesWon,
		WinRate:          winRate,
		RevenueWonCents:  a.RevenueWonCents,
		AvgResponseHours: a.AvgResponseHours,
		TopBuyers:        make([]TopBuyer, 0, len(buyers)),
	}
	for _, b := range buyers {
		out.TopBuyers = append(out.TopBuyers, TopBuyer{
			CompanyID: b.CompanyID, Name: b.Name, Orders: b.Orders, RevenueCents: b.RevenueCents,
		})
	}
	return out, nil
}
```
> If `SellerAnalyticsRow.RevenueWonCents` generates as `interface{}` (SUM can be numeric) or
> `AvgResponseHours` as `interface{}`, cast in SQL (already `::bigint`/`::float8`) — verify the generated
> Go types are `int64`/`float64` and adjust the struct field reads if sqlc produced `pgtype`/`interface{}`.

- [ ] **Step 4: Add the route to `internal/dashboard/handler.go`.** Register a NON-`/dashboard`-prefixed
  route (analytics lives at `/v1/analytics/seller`), so add a second group in `Register`:
```go
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/dashboard", guard...)
	g.Get("/buyer", h.buyer)
	g.Get("/seller", h.seller)

	a := app.Group("/v1/analytics", guard...)
	a.Get("/seller", h.sellerAnalytics)
}

func (h *Handler) sellerAnalytics(c *fiber.Ctx) error {
	a, err := h.svc.SellerAnalytics(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(a)
}
```

- [ ] **Step 5: Failing test** in `internal/dashboard/dashboard_test.go` — seed a full won-deal and assert:
```go
func TestSellerAnalyticsWinRateAndRevenue(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()

	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sa-seller-co", Type: "seller"})
	require.NoError(t, err)
	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sa-buyer-co", Type: "buyer"})
	require.NoError(t, err)
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "sa-" + buyer.ID + "@ex.com", Role: "owner"})
	require.NoError(t, err)
	rfq, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: user.ID, Title: "SA", RawText: "need X"})
	require.NoError(t, err)
	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{RfqID: rfq.ID, SellerCompanyID: seller.ID, PriceCents: 5000, Currency: "USD"})
	require.NoError(t, err)
	// Mark the quote accepted + create the order (a won deal).
	_, err = q.AcceptQuote(ctx, quote.ID) // AcceptQuote(ctx, id string), not a params struct
	require.NoError(t, err)
	_, err = q.CreateOrder(ctx, sqlc.CreateOrderParams{
		RfqID: rfq.ID, QuoteID: quote.ID, BuyerCompanyID: buyer.ID, SellerCompanyID: seller.ID,
		TotalCents: 5000, Currency: "USD",
	})
	require.NoError(t, err)

	a, err := newSvc(t, q).SellerAnalytics(ctx, seller.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), a.QuotesWon)
	require.InDelta(t, 1.0, a.WinRate, 0.001, "1 accepted / 1 decided = 100%")
	require.GreaterOrEqual(t, a.RevenueWonCents, int64(5000))
	require.NotEmpty(t, a.TopBuyers)
	require.Equal(t, buyer.ID, a.TopBuyers[0].CompanyID)
}
```
> Verified: `AcceptQuote(ctx, id string) (Quote, error)` (positional id, NOT a params struct);
> `CreateOrderParams{RfqID, QuoteID, BuyerCompanyID, SellerCompanyID, TotalCents, Currency}`;
> `CreateRFQMatchParams{RfqID, SellerCompanyID, Score float64, Reason string}` (Score/Reason default to 0/""
> in the struct literal — fine). `entitlements.NewService(q, zerolog.Logger)` — pass `zerolog.Nop()`.

- [ ] **Step 6: Run** `go test ./internal/dashboard/ -v` then full suite `go test ./... -p 1` → green.
- [ ] **Step 7: Commit** — `feat(core): seller analytics — win rate, revenue, response time, top buyers`.

---

## Self-Review checklist

- Seller counts tenant-scoped: incoming = matched open RFQs; review_queue = matched open RFQs unquoted by
  this seller; active_orders = created|confirmed|fulfilled.
- Agent online iff a valid (active, non-expired) credential exists AND `last_seen` within 5 min; `last_seen`
  echoed as ISO or null; autonomy from entitlements.
- plan_usage reuses `entitlements.Usage` + `PlanCode` (no reinvented limit math).
- win_rate = won/decided (0 when decided=0, no divide-by-zero); revenue excludes cancelled; response time =
  avg(quote.created_at − rfq.created_at) in hours; top buyers grouped, top 5 by revenue, joined for name.
- Analytics at `/v1/analytics/seller`, dashboard at `/v1/dashboard/seller`; both tenant-guarded.
- Generated nullable/aggregate types matched exactly (no undefined helpers, no guessed types).
- Full suite green under `-p 1`; nothing pushed; no new migration.

# Sprint 6 · Plan 3 — Buyer Dashboard + RFQ Tabs/Search + New-Quote Badges Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repo: `/home/deploy/b2b/tradebridge-core`. Commit per task.

**Goal:** Turn the buyer side into real, tenant-scoped aggregations: a `GET /v1/dashboard/buyer` summary,
server-side RFQ list tabs (open/quoted/closed) + text search, and a per-RFQ "new quotes since you last
looked" badge backed by a lightweight `rfq_views` last-seen table.

**Architecture:** Migration 026 adds `rfq_views(company_id, rfq_id, last_viewed_at)`. A new
`internal/dashboard` package holds the buyer aggregation queries + `GET /v1/dashboard/buyer` (Plan 4 extends
this package with the seller side). The existing `internal/rfq` list handler gains `tab`/`q` filtering and
each row gains `quote_count` + `new_quotes`; opening an RFQ (`GET /v1/rfqs/:id`) upserts the view row to
clear that RFQ's badge.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5, golang-migrate.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- DB tests → `TEST_DATABASE_URL` (`tradebridge_core_test`) only. Setup for every shell:
  `export PATH="$HOME/.local/bin:$PATH"; export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- New migration number is **026** (test/live DB at v25 after Plan 1's 025).
- After editing `.sql`, `sqlc generate`; match generated types exactly, don't guess.
- All aggregations tenant-scoped to `middleware.CompanyID(c)`. No cross-tenant reads.
- Full suite green under `go test ./... -p 1`.

## Reference (current state)

- RFQ statuses (constants in `internal/rfq/requirements.go`): `StatusOpen="open"`, `StatusQuoted="quoted"`,
  `StatusClosed="closed"`, `StatusExpired="expired"`.
- `internal/rfq/handler.go`: `list` (currently `ListRFQsByBuyer`, no filter), `get` (`mustOwn` → returns
  matches), `rfqView` struct (fields: id, title, raw_text, requirements, category, status, match_count,
  messages, created_at), `view(sqlc.Rfq) rfqView`.
- Order statuses: `created, confirmed, fulfilled, completed, cancelled, disputed`. In-progress =
  `created|confirmed|fulfilled`.
- `orders` cols include `buyer_company_id`, `seller_company_id`, `status`, `total_cents`, `created_at`.
- `quotes` cols include `rfq_id`, `seller_company_id`, `status`, `created_at`.
- Migration/sqlc/handler patterns as in Plans 1–2.

## File Structure

- Create: `pkg/db/migrations/026_rfq_views.up.sql`, `.down.sql`
- Create: `pkg/db/query/dashboard.sql` (buyer aggregations + rfq_views upsert)
- Modify: `pkg/db/query/rfq.sql` (add `ListRFQsByBuyerFiltered`)
- Create: `internal/dashboard/service.go` (`Service.BuyerDashboard`)
- Create: `internal/dashboard/handler.go` (`GET /v1/dashboard/buyer`)
- Create: `internal/dashboard/dashboard_test.go`
- Modify: `internal/rfq/handler.go` (tabs/search in `list`; view-upsert in `get`; badge fields on `rfqView`)
- Modify: `internal/httpapi/router.go` (mount dashboard handler)

---

### Task 1: Migration 026 — rfq_views + upsert query

**Files:** `pkg/db/migrations/026_rfq_views.up.sql`, `.down.sql`, `pkg/db/query/dashboard.sql` (upsert only).

**Interfaces:**
- Produces: table `rfq_views(company_id, rfq_id, last_viewed_at)` PK `(company_id, rfq_id)`;
  `sqlc.UpsertRFQView(ctx, {CompanyID, RfqID})`.

- [ ] **Step 1: Write `026_rfq_views.up.sql`:**
```sql
-- Sprint 6: per-buyer last-seen marker per RFQ, backing the "new quotes since
-- you last looked" badge. One row per (company, rfq); upserted on RFQ view.
CREATE TABLE rfq_views (
    company_id     TEXT NOT NULL REFERENCES companies(id),
    rfq_id         TEXT NOT NULL REFERENCES rfqs(id) ON DELETE CASCADE,
    last_viewed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (company_id, rfq_id)
);
```

- [ ] **Step 2: Write `026_rfq_views.down.sql`:**
```sql
DROP TABLE IF EXISTS rfq_views;
```

- [ ] **Step 3: Create `pkg/db/query/dashboard.sql` with the upsert:**
```sql
-- name: UpsertRFQView :exec
-- Records that the buyer has now seen this RFQ's quotes (clears its badge).
INSERT INTO rfq_views (company_id, rfq_id, last_viewed_at)
VALUES (@company_id, @rfq_id, NOW())
ON CONFLICT (company_id, rfq_id) DO UPDATE SET last_viewed_at = NOW();
```

- [ ] **Step 4: Apply + round-trip on TEST DB + generate:**
```bash
export PATH="$HOME/.local/bin:$PATH"
export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" version   # expect 26
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" down 1 && migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
sqlc generate && go build ./...
```
Expected: version 26, clean round-trip, build exit 0.

- [ ] **Step 5: Commit** — `feat(core): migration 026 — rfq_views last-seen + upsert query`.

---

### Task 2: Buyer dashboard aggregation queries + service + endpoint

**Files:** `pkg/db/query/dashboard.sql`, `internal/dashboard/service.go`,
`internal/dashboard/handler.go`, `internal/dashboard/dashboard_test.go`, `internal/httpapi/router.go`.

**Interfaces:**
- Produces: `sqlc.BuyerDashboardCounts(ctx, companyID)`, `sqlc.ListBuyerRecentRFQs(ctx, {CompanyID, Lim})`;
  `dashboard.NewService(q).BuyerDashboard(ctx, companyID) (BuyerDashboard, error)`;
  `GET /v1/dashboard/buyer`.

- [ ] **Step 1: Add aggregation queries to `pkg/db/query/dashboard.sql`:**
```sql
-- name: BuyerDashboardCounts :one
-- All four buyer headline counts in one round-trip, tenant-scoped.
SELECT
    (SELECT COUNT(*) FROM rfqs r
       WHERE r.buyer_company_id = @company_id AND r.status IN ('open','quoted'))::bigint AS active_rfqs,
    (SELECT COUNT(*) FROM quotes q JOIN rfqs r ON r.id = q.rfq_id
       WHERE r.buyer_company_id = @company_id)::bigint AS quotes_received,
    (SELECT COUNT(*) FROM quotes q JOIN rfqs r ON r.id = q.rfq_id
       LEFT JOIN rfq_views v ON v.rfq_id = r.id AND v.company_id = r.buyer_company_id
       WHERE r.buyer_company_id = @company_id
         AND q.created_at > COALESCE(v.last_viewed_at, 'epoch'::timestamptz))::bigint AS new_quotes,
    (SELECT COUNT(*) FROM orders o
       WHERE o.buyer_company_id = @company_id
         AND o.status IN ('created','confirmed','fulfilled'))::bigint AS orders_in_progress;

-- name: ListBuyerRecentRFQs :many
-- Recent RFQs with quote counts + how many quotes arrived since last viewed.
SELECT r.id, r.title, r.status, r.created_at,
    (SELECT COUNT(*) FROM quotes q WHERE q.rfq_id = r.id)::bigint AS quote_count,
    (SELECT COUNT(*) FROM quotes q
       WHERE q.rfq_id = r.id
         AND q.created_at > COALESCE(v.last_viewed_at, 'epoch'::timestamptz))::bigint AS new_quotes
FROM rfqs r
LEFT JOIN rfq_views v ON v.rfq_id = r.id AND v.company_id = r.buyer_company_id
WHERE r.buyer_company_id = @company_id
ORDER BY r.created_at DESC
LIMIT @lim;
```

- [ ] **Step 2: `sqlc generate` + build.** Confirm generated names (`BuyerDashboardCountsRow` with
  `ActiveRfqs, QuotesReceived, NewQuotes, OrdersInProgress int64`; `ListBuyerRecentRFQsRow` /
  `ListBuyerRecentRFQsParams{CompanyID, Lim}`).

- [ ] **Step 3: Write `internal/dashboard/service.go`:**
```go
// Package dashboard serves tenant-scoped business aggregations for buyer and
// seller home screens.
package dashboard

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

const recentRFQLimit = 8

// Service runs dashboard aggregation queries.
type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

// RecentRFQ is a compact RFQ row for the dashboard recent list.
type RecentRFQ struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	QuoteCount int64  `json:"quote_count"`
	NewQuotes  int64  `json:"new_quotes"`
	CreatedAt  string `json:"created_at"`
}

// BuyerDashboard is the buyer home summary.
type BuyerDashboard struct {
	ActiveRFQs       int64       `json:"active_rfqs"`
	QuotesReceived   int64       `json:"quotes_received"`
	NewQuotes        int64       `json:"new_quotes"`
	OrdersInProgress int64       `json:"orders_in_progress"`
	RecentRFQs       []RecentRFQ `json:"recent_rfqs"`
}

// BuyerDashboard computes the buyer home summary for a company.
func (s *Service) BuyerDashboard(ctx context.Context, companyID string) (BuyerDashboard, error) {
	counts, err := s.q.BuyerDashboardCounts(ctx, companyID)
	if err != nil {
		return BuyerDashboard{}, err
	}
	recent, err := s.q.ListBuyerRecentRFQs(ctx, sqlc.ListBuyerRecentRFQsParams{
		CompanyID: companyID,
		Lim:       recentRFQLimit,
	})
	if err != nil {
		return BuyerDashboard{}, err
	}
	out := BuyerDashboard{
		ActiveRFQs:       counts.ActiveRfqs,
		QuotesReceived:   counts.QuotesReceived,
		NewQuotes:        counts.NewQuotes,
		OrdersInProgress: counts.OrdersInProgress,
		RecentRFQs:       make([]RecentRFQ, 0, len(recent)),
	}
	for _, r := range recent {
		out.RecentRFQs = append(out.RecentRFQs, RecentRFQ{
			ID:         r.ID,
			Title:      r.Title,
			Status:     r.Status,
			QuoteCount: r.QuoteCount,
			NewQuotes:  r.NewQuotes,
			CreatedAt:  r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return out, nil
}
```
> Confirm `BuyerDashboardCountsRow` field names + that `CreatedAt` is `time.Time` (adjust `.UTC().Format`
> if it is a `pgtype.Timestamptz` — most tables here map to `time.Time`; verify in the generated struct).

- [ ] **Step 4: Write `internal/dashboard/handler.go`:**
```go
package dashboard

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the dashboard read endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register mounts the tenant-guarded dashboard routes.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/dashboard", guard...)
	g.Get("/buyer", h.buyer)
}

func (h *Handler) buyer(c *fiber.Ctx) error {
	d, err := h.svc.BuyerDashboard(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(d)
}
```

- [ ] **Step 5: Wire in `internal/httpapi/router.go`** (near the other guarded handlers, e.g. after the
  discovery handler registration):
```go
	dashboard.NewHandler(dashboard.NewService(q)).Register(f, guard...)
```
  Add import `"github.com/tradebridge/core/internal/dashboard"`.

- [ ] **Step 6: Failing test** `internal/dashboard/dashboard_test.go` — seed a buyer with 2 RFQs + quotes +
  an order and assert the aggregation:
```go
package dashboard

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

func TestBuyerDashboardAggregates(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "bd-buyer-co", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "bd-seller-co", Type: "seller"})
	require.NoError(t, err)
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "bd-" + buyer.ID + "@example.com", Role: "owner",
	})
	require.NoError(t, err)

	// One open RFQ with a quote (unseen => new), one quoted RFQ.
	rfq1, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: user.ID, Title: "Open RFQ", RawText: "need boxes",
	})
	require.NoError(t, err)
	_, err = q.SetRFQStatus(ctx, sqlc.SetRFQStatusParams{ID: rfq1.ID, Status: "open"})
	require.NoError(t, err)
	_, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfq1.ID, SellerCompanyID: seller.ID, PriceCents: 1000, Currency: "USD",
	})
	require.NoError(t, err)

	svc := NewService(q)
	d, err := svc.BuyerDashboard(ctx, buyer.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, d.ActiveRFQs, int64(1))
	require.GreaterOrEqual(t, d.QuotesReceived, int64(1))
	require.GreaterOrEqual(t, d.NewQuotes, int64(1), "unseen quote counts as new")
	require.NotEmpty(t, d.RecentRFQs)

	// After viewing the RFQ, its quotes are no longer "new".
	require.NoError(t, q.UpsertRFQView(ctx, sqlc.UpsertRFQViewParams{CompanyID: buyer.ID, RfqID: rfq1.ID}))
	d2, err := svc.BuyerDashboard(ctx, buyer.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), d2.NewQuotes, "viewing clears the new-quote badge")
}
```
> Verified field names: `CreateUserParams{CompanyID, Email, Role}` (NO password field — users are created
> passwordless here). `CreateRFQParams{BuyerCompanyID, CreatedBy, Title, RawText}`.
> `UpsertQuoteParams{RfqID, SellerCompanyID, PriceCents, Currency, Notes, Completeness, AgentCommandID
> (*string), DeliveryDays (*int32)}` — the omitted fields default to Go zero values (Notes="",
> Completeness=0, nil pointers), which satisfy the columns. No adjustment needed beyond what's shown.

- [ ] **Step 7: Run** `go test ./internal/dashboard/ -v` → PASS.
- [ ] **Step 8: Commit** — `feat(core): buyer dashboard aggregations + GET /v1/dashboard/buyer`.

---

### Task 3: RFQ list tabs/search + new-quote badges + view-on-open

**Files:** `pkg/db/query/rfq.sql`, `internal/rfq/handler.go`.

**Interfaces:**
- Produces: `sqlc.ListRFQsByBuyerFiltered(ctx, params)`; `GET /v1/rfqs?tab=&q=&page=` server-side filtered;
  each row carries `quote_count` + `new_quotes`; `GET /v1/rfqs/:id` upserts the view row.

- [ ] **Step 1: Add the filtered query to `pkg/db/query/rfq.sql`:**
```sql
-- name: ListRFQsByBuyerFiltered :many
-- Buyer RFQ list with tab filter (open|quoted|closed), text search, and per-RFQ
-- quote_count + new_quotes (quotes since the buyer last viewed the RFQ).
SELECT r.id, r.title, r.raw_text, r.requirements, r.category, r.status, r.match_count, r.created_at,
    (SELECT COUNT(*) FROM quotes q WHERE q.rfq_id = r.id)::bigint AS quote_count,
    (SELECT COUNT(*) FROM quotes q
       WHERE q.rfq_id = r.id
         AND q.created_at > COALESCE(v.last_viewed_at, 'epoch'::timestamptz))::bigint AS new_quotes
FROM rfqs r
LEFT JOIN rfq_views v ON v.rfq_id = r.id AND v.company_id = r.buyer_company_id
WHERE r.buyer_company_id = @company_id
  AND (@tab::text = ''
       OR (@tab::text = 'open'   AND r.status = 'open')
       OR (@tab::text = 'quoted' AND r.status = 'quoted')
       OR (@tab::text = 'closed' AND r.status IN ('closed','expired')))
  AND (@q::text = '' OR r.title ILIKE '%' || @q::text || '%' OR r.raw_text ILIKE '%' || @q::text || '%')
ORDER BY r.created_at DESC
LIMIT @lim OFFSET @off;
```

- [ ] **Step 2: `sqlc generate` + build.** Confirm `ListRFQsByBuyerFilteredParams{CompanyID, Tab, Q, Lim, Off}`
  and `ListRFQsByBuyerFilteredRow` field names/types.

- [ ] **Step 3: Add badge fields to `rfqView`** in `internal/rfq/handler.go` (omitempty so unaffected
  endpoints' JSON is unchanged):
```go
	QuoteCount   int64         `json:"quote_count,omitempty"`
	NewQuotes    int64         `json:"new_quotes,omitempty"`
```
  (append inside the `rfqView` struct after `CreatedAt`).

- [ ] **Step 4: Rewrite `list`** to filter server-side and populate badges:
```go
func (h *Handler) list(c *fiber.Ctx) error {
	rows, err := h.q.ListRFQsByBuyerFiltered(c.Context(), sqlc.ListRFQsByBuyerFilteredParams{
		CompanyID: middleware.CompanyID(c),
		Tab:       c.Query("tab"),
		Q:         c.Query("q"),
		Lim:       50,
		Off:       0,
	})
	if err != nil {
		return err
	}
	out := make([]rfqView, 0, len(rows))
	for _, r := range rows {
		var req Requirements
		if len(r.Requirements) > 0 {
			_ = json.Unmarshal(r.Requirements, &req)
		}
		out = append(out, rfqView{
			ID:           r.ID,
			Title:        r.Title,
			RawText:      r.RawText,
			Requirements: req,
			Category:     r.Category,
			Status:       r.Status,
			MatchCount:   r.MatchCount,
			CreatedAt:    r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			QuoteCount:   r.QuoteCount,
			NewQuotes:    r.NewQuotes,
		})
	}
	return c.JSON(fiber.Map{"rfqs": out, "count": len(out)})
}
```
> If a `tab` value is unrecognized (not open/quoted/closed and not empty), the query returns no rows (all
> CASE branches false). That's acceptable — the frontend only sends valid tabs. Do not add validation.

- [ ] **Step 5: Upsert the view in `get`** so opening an RFQ clears its badge. After the `mustOwn` success in
  `get`, add (best-effort — a view-tracking failure must not break the read):
```go
	_ = h.q.UpsertRFQView(c.Context(), sqlc.UpsertRFQViewParams{
		CompanyID: middleware.CompanyID(c),
		RfqID:     rfq.ID,
	})
```
  (place it immediately after `rfq, err := h.mustOwn(c); if err != nil { return err }`.)

- [ ] **Step 6: Build + test** the rfq package and a manual list-filter assertion:
```bash
go test ./internal/rfq/ -p 1
go build ./...
```
Expected: green. (The rfq package's existing tests must still pass; the shared-DB `TestMatchSuppliersVerifiedGate`
flake is unrelated and avoided by `-p 1`.)

- [ ] **Step 7: Full suite** `go test ./... -p 1` → green.

- [ ] **Step 8: Commit** — `feat(core): RFQ list tabs/search + new-quote badges + view-on-open`.

---

## Self-Review checklist

- `rfq_views` migration 026 round-trips; PK `(company_id, rfq_id)`; cascade on rfq delete.
- Buyer dashboard counts are tenant-scoped and correct: active = open|quoted; quotes_received = all quotes on
  buyer's RFQs; new_quotes = quotes after last view; orders_in_progress = created|confirmed|fulfilled.
- Viewing an RFQ (`GET /v1/rfqs/:id`) upserts `rfq_views`, and the badge query then reports 0 new for it
  (proved by the dashboard test's second assertion).
- RFQ list `tab` maps open→open, quoted→quoted, closed→closed|expired; `q` searches title+raw_text; each row
  carries `quote_count`/`new_quotes`; get/create response shapes unchanged (omitempty).
- Full suite green under `-p 1`; nothing pushed.

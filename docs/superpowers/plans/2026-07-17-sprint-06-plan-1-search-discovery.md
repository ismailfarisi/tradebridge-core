# Sprint 6 · Plan 1 — Search Infra + Discovery Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repo: `/home/deploy/b2b/tradebridge-core` (Go/Fiber, sqlc, golang-migrate, pgx/v5). Commit per task.

**Goal:** Postgres-FTS + trigram supplier discovery with a transparent weighted rank, exposed both publicly
(`GET /public/v1/discovery`, unauthenticated + rate-limited) and to logged-in users (`GET /v1/discovery`).

**Architecture:** Migration 025 adds `pg_trgm` + generated `tsvector` columns/GIN indexes on
`supplier_profiles` and `catalog_summaries`. A new `internal/discovery` package holds a `DiscoverSuppliers`
sqlc query (relevance via `ts_rank_cd`, blended with rating + verified via **weights passed as query args
from Go constants**), a thin `Service`, and a `Handler` mounting the two endpoints.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5, golang-migrate, zerolog. Postgres FTS (`websearch_to_tsquery`,
`ts_rank_cd`) + `pg_trgm`.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**. `.env`/secrets never committed.
- DB tests use `TEST_DATABASE_URL` → `tradebridge_core_test`, **NEVER** live. Set it before running:
  `export PATH="$HOME/.local/bin:$PATH"; export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- Public discovery returns **only** whitelisted public columns (company_id, name, verification_status,
  verified_domain, description, categories, location, avg_rating, review_count, score). Never `SELECT *` of
  `companies`.
- Rank weights are **Go constants in one place** (`internal/discovery/rank.go`), passed to the query as args.
  No scattered magic numbers.
- Migration number is **025** (test+live DB currently at v24).
- Regenerate sqlc after editing `.sql` (`sqlc generate` from repo root, config `sqlc.yaml`). Never hand-edit
  `pkg/db/sqlc/*.go`.
- Reserved-word param names fail sqlc (e.g. `@from`); use non-reserved names (`@query`, `@lim`, `@off`).

## Backend patterns (follow exactly)

- Handler: `func NewHandler(q *sqlc.Queries) *Handler` + `func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler)`; public routes via `RegisterPublic(app *fiber.App)` with NO guard (see `internal/suppliers/public.go`).
- Query int params clamp with `parseIntDefault` (already in `internal/suppliers/handler.go`; replicate a small local copy in discovery — do not cross-import handler internals).
- Rate limiter: in `internal/httpapi/router.go`, limiters are built via
  `mkRL(scope, spec, def, key)` → `fiber.Handler` (see lines ~172-178). Public discovery uses `middleware.KeyIP`.
- DB-backed unit test harness: `TEST_DATABASE_URL` → `pgxpool.New` → `sqlc.New(pool)`, `t.Skip` when unset
  (see `internal/suppliers/public_test.go:17`). Use company names unique to each test to stay isolated from
  the shared test DB (no TRUNCATE in unit tests).

## File Structure

- Create: `pkg/db/migrations/025_search_discovery.up.sql`, `025_search_discovery.down.sql`
- Modify: `pkg/db/query/discovery.sql` (new file)
- Create (generated): `pkg/db/sqlc/discovery.sql.go` (via `sqlc generate`)
- Create: `internal/discovery/rank.go` (weight constants + `Params` normalization)
- Create: `internal/discovery/service.go` (`Service` wrapping the query)
- Create: `internal/discovery/handler.go` (`/v1/discovery` + `/public/v1/discovery`)
- Create: `internal/discovery/discovery_test.go` (rank ordering + projection safety)
- Modify: `internal/httpapi/router.go` (mount handler + public route + rate limiter)
- Modify: `internal/config/config.go` + router `Config` (add `RateLimitDiscovery string`)

---

### Task 1: Migration 025 — pg_trgm + generated tsvector columns + indexes

**Files:** `pkg/db/migrations/025_search_discovery.up.sql`, `.down.sql`.

**Interfaces:**
- Produces: `supplier_profiles.search_vector tsvector` (generated), `catalog_summaries.search_vector`,
  GIN indexes `idx_supplier_profiles_fts`, `idx_catalog_summaries_fts`, trigram index
  `idx_companies_name_trgm`. Later tasks query `sp.search_vector`, `cs.search_vector`, `c.name` (trgm).

- [ ] **Step 1: Write `025_search_discovery.up.sql`:**

```sql
-- Sprint 6: Postgres FTS + trigram discovery. Generated tsvector columns keep
-- the index in sync automatically (no triggers); pg_trgm gives typo tolerance
-- on company names. Additive + safe: no existing rows change semantics.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE supplier_profiles
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english',
            coalesce(description, '') || ' ' ||
            coalesce(location, '') || ' ' ||
            array_to_string(categories, ' ')
        )
    ) STORED;

CREATE INDEX idx_supplier_profiles_fts ON supplier_profiles USING GIN (search_vector);

ALTER TABLE catalog_summaries
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        to_tsvector('english',
            coalesce(name, '') || ' ' ||
            coalesce(category, '') || ' ' ||
            coalesce(summary, '')
        )
    ) STORED;

CREATE INDEX idx_catalog_summaries_fts ON catalog_summaries USING GIN (search_vector);

CREATE INDEX idx_companies_name_trgm ON companies USING GIN (name gin_trgm_ops);
```

- [ ] **Step 2: Write `025_search_discovery.down.sql`:**

```sql
DROP INDEX IF EXISTS idx_companies_name_trgm;
DROP INDEX IF EXISTS idx_catalog_summaries_fts;
ALTER TABLE catalog_summaries DROP COLUMN IF EXISTS search_vector;
DROP INDEX IF EXISTS idx_supplier_profiles_fts;
ALTER TABLE supplier_profiles DROP COLUMN IF EXISTS search_vector;
-- pg_trgm left installed (other features may rely on it); dropping the extension
-- is intentionally omitted to stay safe on shared databases.
```

- [ ] **Step 3: Apply up + verify down/up round-trips on the TEST DB:**

Run:
```bash
export PATH="$HOME/.local/bin:$PATH"
export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" version   # expect 25
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" down 1
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
```
Expected: version `25`, no dirty state, round-trip clean.

- [ ] **Step 4: Commit** — `feat(core): migration 025 — pg_trgm + generated tsvector search columns`.

---

### Task 2: DiscoverSuppliers query + ranking service

**Files:** `pkg/db/query/discovery.sql`, `internal/discovery/rank.go`, `internal/discovery/service.go`,
`internal/discovery/discovery_test.go`. Then `sqlc generate`.

**Interfaces:**
- Produces: `sqlc.DiscoverSuppliers(ctx, DiscoverSuppliersParams) ([]DiscoverSuppliersRow, error)`;
  `discovery.Service.Search(ctx, discovery.Query) ([]discovery.Result, error)`;
  `discovery.Query{Q, Category, Location string; VerifiedOnly bool; MinRating float64; Sort string; Page, PerPage int}`;
  `discovery.Result` mirroring the row (public columns + `Score float64`). Consumed by Task 3's handler.

- [ ] **Step 1: Write `pkg/db/query/discovery.sql`:**

```sql
-- name: DiscoverSuppliers :many
-- Weighted discovery ranking. Weights (w_rel, w_rating, w_verified) are passed
-- from Go constants (internal/discovery/rank.go) so ranking lives in ONE place
-- and is unit-testable. Relevance uses ts_rank_cd clamped to [0,1]; empty query
-- => relevance 0 (browse-all ordered by trust + recency).
WITH rep AS (
    SELECT reviewee_company_id AS company_id,
           AVG(rating)::float8  AS avg_rating,
           COUNT(*)::bigint     AS review_count
    FROM reviews
    WHERE status = 'visible'
    GROUP BY reviewee_company_id
)
SELECT c.id                       AS company_id,
       c.name                     AS name,
       c.verification_status      AS verification_status,
       c.verified_domain          AS verified_domain,
       sp.description             AS description,
       sp.categories              AS categories,
       sp.location                AS location,
       sp.verified                AS verified,
       COALESCE(rep.avg_rating, 0)::float8   AS avg_rating,
       COALESCE(rep.review_count, 0)::bigint AS review_count,
       (
            sqlc.arg(w_rel)::float8 *
              CASE WHEN sqlc.arg(query)::text = '' THEN 0
                   ELSE LEAST(ts_rank_cd(sp.search_vector,
                        websearch_to_tsquery('english', sqlc.arg(query)::text)), 1.0) END
          + sqlc.arg(w_rating)::float8 * (COALESCE(rep.avg_rating, 0) / 5.0)
          + sqlc.arg(w_verified)::float8 * (CASE WHEN sp.verified THEN 1 ELSE 0 END)
       )::float8 AS score
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
LEFT JOIN rep ON rep.company_id = sp.company_id
WHERE (
        sqlc.arg(query)::text = ''
     OR sp.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
     OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
     OR EXISTS (
            SELECT 1 FROM catalog_summaries cs
            WHERE cs.company_id = sp.company_id
              AND cs.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
        )
    )
  AND (sqlc.arg(category)::text = ''
       OR EXISTS (SELECT 1 FROM unnest(sp.categories) cat WHERE cat ILIKE sqlc.arg(category)::text))
  AND (NOT sqlc.arg(verified_only)::bool OR sp.verified)
  AND (sqlc.arg(location)::text = '' OR sp.location ILIKE '%' || sqlc.arg(location)::text || '%')
  AND (sqlc.arg(min_rating)::float8 <= 0 OR COALESCE(rep.avg_rating, 0) >= sqlc.arg(min_rating)::float8)
ORDER BY
    CASE WHEN sqlc.arg(sort)::text = 'rating' THEN COALESCE(rep.avg_rating, 0) END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort)::text = 'newest' THEN sp.updated_at END DESC NULLS LAST,
    score DESC,
    sp.updated_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: CountDiscoverSuppliers :one
-- Total matches for the same filter set (pagination). Mirrors the WHERE above;
-- no ranking/ordering.
WITH rep AS (
    SELECT reviewee_company_id AS company_id, AVG(rating)::float8 AS avg_rating
    FROM reviews WHERE status = 'visible' GROUP BY reviewee_company_id
)
SELECT COUNT(*)::bigint AS total
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
LEFT JOIN rep ON rep.company_id = sp.company_id
WHERE (
        sqlc.arg(query)::text = ''
     OR sp.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
     OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
     OR EXISTS (SELECT 1 FROM catalog_summaries cs WHERE cs.company_id = sp.company_id
                AND cs.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text))
    )
  AND (sqlc.arg(category)::text = ''
       OR EXISTS (SELECT 1 FROM unnest(sp.categories) cat WHERE cat ILIKE sqlc.arg(category)::text))
  AND (NOT sqlc.arg(verified_only)::bool OR sp.verified)
  AND (sqlc.arg(location)::text = '' OR sp.location ILIKE '%' || sqlc.arg(location)::text || '%')
  AND (sqlc.arg(min_rating)::float8 <= 0 OR COALESCE(rep.avg_rating, 0) >= sqlc.arg(min_rating)::float8);
```

- [ ] **Step 2: Run `sqlc generate`** (repo root):
```bash
export PATH="$HOME/.local/bin:$PATH"
sqlc generate
```
Expected: `pkg/db/sqlc/discovery.sql.go` created; build still compiles (`go build ./...`).

- [ ] **Step 3: Write `internal/discovery/rank.go`:**

```go
// Package discovery implements Postgres-FTS supplier search with a transparent
// weighted rank blending text relevance, rating, and verification.
package discovery

// Rank weights — the single source of truth for discovery ranking. Passed to
// the DiscoverSuppliers query as args so scoring is tunable in one place.
const (
	WeightRelevance = 0.60
	WeightRating    = 0.25
	WeightVerified  = 0.15
)

const (
	defaultPerPage = 20
	maxPerPage     = 50
)

// Query is a normalized discovery request.
type Query struct {
	Q            string
	Category     string
	Location     string
	VerifiedOnly bool
	MinRating    float64
	Sort         string // "relevance" (default) | "rating" | "newest"
	Page         int    // 1-based
	PerPage      int
}

// normalize clamps paging and defaults sort.
func (q *Query) normalize() {
	if q.PerPage <= 0 {
		q.PerPage = defaultPerPage
	}
	if q.PerPage > maxPerPage {
		q.PerPage = maxPerPage
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	switch q.Sort {
	case "rating", "newest":
	default:
		q.Sort = "relevance"
	}
}

func (q Query) limit() int32  { return int32(q.PerPage) }
func (q Query) offset() int32 { return int32((q.Page - 1) * q.PerPage) }
```

- [ ] **Step 4: Write `internal/discovery/service.go`:**

```go
package discovery

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Service runs discovery queries.
type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

// Result is a single ranked supplier (public columns only).
type Result struct {
	CompanyID          string   `json:"company_id"`
	Name               string   `json:"name"`
	VerificationStatus string   `json:"verification_status"`
	VerifiedDomain     *string  `json:"verified_domain"`
	Description        *string  `json:"description"`
	Categories         []string `json:"categories"`
	Location           *string  `json:"location"`
	Verified           bool     `json:"verified"`
	AvgRating          float64  `json:"avg_rating"`
	ReviewCount        int64    `json:"review_count"`
	Score              float64  `json:"score"`
}

// Page bundles results with pagination metadata.
type Page struct {
	Results []Result `json:"results"`
	Page    int      `json:"page"`
	PerPage int      `json:"per_page"`
	Total   int64    `json:"total"`
}

// Search executes the weighted discovery query.
func (s *Service) Search(ctx context.Context, q Query) (Page, error) {
	q.normalize()
	rows, err := s.q.DiscoverSuppliers(ctx, sqlc.DiscoverSuppliersParams{
		WRel:         WeightRelevance,
		WRating:      WeightRating,
		WVerified:    WeightVerified,
		Query:        q.Q,
		Category:     q.Category,
		VerifiedOnly: q.VerifiedOnly,
		Location:     q.Location,
		MinRating:    q.MinRating,
		Sort:         q.Sort,
		Lim:          q.limit(),
		Off:          q.offset(),
	})
	if err != nil {
		return Page{}, err
	}
	total, err := s.q.CountDiscoverSuppliers(ctx, sqlc.CountDiscoverSuppliersParams{
		Query:        q.Q,
		Category:     q.Category,
		VerifiedOnly: q.VerifiedOnly,
		Location:     q.Location,
		MinRating:    q.MinRating,
	})
	if err != nil {
		return Page{}, err
	}
	results := make([]Result, 0, len(rows))
	for _, r := range rows {
		results = append(results, Result{
			CompanyID:          r.CompanyID,
			Name:               r.Name,
			VerificationStatus: r.VerificationStatus,
			VerifiedDomain:     r.VerifiedDomain,
			Description:        r.Description,
			Categories:         r.Categories,
			Location:           r.Location,
			Verified:           r.Verified,
			AvgRating:          r.AvgRating,
			ReviewCount:        r.ReviewCount,
			Score:              r.Score,
		})
	}
	return Page{Results: results, Page: q.Page, PerPage: q.PerPage, Total: total}, nil
}
```

> NOTE: the generated `DiscoverSuppliersParams` field names come from sqlc's camelCasing of the arg names
> (`w_rel`→`WRel`, `verified_only`→`VerifiedOnly`, etc.). After `sqlc generate`, open
> `pkg/db/sqlc/discovery.sql.go` and confirm the exact field + result-column Go types (e.g. `VerifiedDomain`
> may be `*string`, `Categories` `[]string`). Adjust `Result`/mapping to match the generated types — do not
> guess; make the package compile against the real generated struct.

- [ ] **Step 5: Write the failing rank-ordering test** `internal/discovery/discovery_test.go`:

```go
package discovery

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func queriesOrSkip(t *testing.T) (*sqlc.Queries, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool), pool
}

func sp(s string) *string { return &s }

// A verified, well-rated, on-topic supplier must outrank an unverified,
// unrated one for the same query term.
func TestDiscoverRanksVerifiedRatedHigher(t *testing.T) {
	q, _ := queriesOrSkip(t)
	ctx := context.Background()
	tag := "rankprobe" // unique token so this test's rows are isolated

	// Verified + will receive a 5-star review.
	top, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rankprobe-top-co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: top.ID, Description: sp("premium " + tag + " supplier"), Categories: []string{tag}, Location: sp("NYC"),
	})
	require.NoError(t, err)
	require.NoError(t, q.SyncSupplierVerified(ctx, sqlc.SyncSupplierVerifiedParams{CompanyID: top.ID, Verified: true}))

	// Unverified, unrated, same term.
	low, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rankprobe-low-co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: low.ID, Description: sp("basic " + tag + " supplier"), Categories: []string{tag}, Location: sp("LA"),
	})
	require.NoError(t, err)

	svc := NewService(q)
	page, err := svc.Search(ctx, Query{Q: tag})
	require.NoError(t, err)

	var topIdx, lowIdx = -1, -1
	for i, r := range page.Results {
		if r.CompanyID == top.ID {
			topIdx = i
		}
		if r.CompanyID == low.ID {
			lowIdx = i
		}
	}
	require.GreaterOrEqual(t, topIdx, 0, "verified supplier present")
	require.GreaterOrEqual(t, lowIdx, 0, "unverified supplier present")
	require.Less(t, topIdx, lowIdx, "verified+rated ranks above unverified")
}
```

> The test uses the EXISTING query `SyncSupplierVerified` (`pkg/db/query/verification.sql:38`,
> `:exec`, params `Verified bool` + `CompanyID string`) to flip `supplier_profiles.verified`. Do NOT add a
> new verified-setting query — reuse this one.

- [ ] **Step 6: Run the test — expect FAIL (compile error / missing query) then implement to green:**
```bash
go build ./... && go test ./internal/discovery/ -run TestDiscoverRanksVerifiedRatedHigher -v
```
Expected first: FAIL. After `sqlc generate` + mapping fixes: PASS.

- [ ] **Step 7: Commit** — `feat(core): DiscoverSuppliers weighted-rank query + discovery service`.

---

### Task 3: Discovery handler + public + authenticated endpoints + rate limit

**Files:** `internal/discovery/handler.go`, `internal/discovery/discovery_test.go` (add projection test),
`internal/httpapi/router.go`, `internal/config/config.go`.

**Interfaces:**
- Consumes: `discovery.NewService(q)`, `Service.Search`.
- Produces: `GET /v1/discovery` (guarded), `GET /public/v1/discovery` (public, rate-limited). JSON shape =
  `discovery.Page`.

- [ ] **Step 1: Write `internal/discovery/handler.go`:**

```go
package discovery

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

// Handler serves discovery search (public + authenticated share one projection).
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register mounts the guarded route. Call RegisterPublic separately for the
// unauthenticated route (with a rate limiter).
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/discovery", guard...)
	g.Get("/", h.search)
	g.Get("", h.search)
}

// RegisterPublic mounts the unauthenticated route with the supplied middleware
// (rate limiter) and NO tenant guard.
func (h *Handler) RegisterPublic(app *fiber.App, mw ...fiber.Handler) {
	handlers := append(append([]fiber.Handler{}, mw...), h.search)
	app.Get("/public/v1/discovery", handlers...)
}

func (h *Handler) search(c *fiber.Ctx) error {
	page, err := h.svc.Search(c.Context(), parseQuery(c))
	if err != nil {
		return err
	}
	return c.JSON(page)
}

func parseQuery(c *fiber.Ctx) Query {
	return Query{
		Q:            c.Query("q"),
		Category:     c.Query("category"),
		Location:     c.Query("location"),
		VerifiedOnly: c.Query("verified") == "true" || c.Query("verified") == "1",
		MinRating:    parseFloatDefault(c.Query("min_rating"), 0),
		Sort:         c.Query("sort"),
		Page:         parseIntDefault(c.Query("page"), 1, 100000),
		PerPage:      parseIntDefault(c.Query("per_page"), defaultPerPage, maxPerPage),
	}
}

func parseIntDefault(s string, def, maxVal int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

func parseFloatDefault(s string, def float64) float64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 {
		return def
	}
	return n
}
```

- [ ] **Step 2: Add `RateLimitDiscovery` config.** In `internal/config/config.go`:
  - add the struct field next to `RateLimitReport` (line ~98):
    `RateLimitDiscovery string ` + "`mapstructure:\"RATE_LIMIT_DISCOVERY\"`"
  - add the default next to the others (line ~148): `v.SetDefault("RATE_LIMIT_DISCOVERY", "120/1h")`.
  Then map it into the router `Config` where the other `RateLimit*` strings are copied (find where
  `RateLimitReport: cfg.RateLimitReport` or equivalent is assigned in `cmd/api` / the config→router wiring
  and add `RateLimitDiscovery`). In `internal/httpapi/router.go` `Config` struct add
  `RateLimitDiscovery string` next to `RateLimitReport`.

- [ ] **Step 3: Wire the handler in `internal/httpapi/router.go`.** After the existing limiters block
  (after `rlVerify := ...`), add:
```go
	rlDiscovery := mkRL("discovery", cfg.RateLimitDiscovery, middleware.RateLimit{Limit: 120, Window: time.Hour}, middleware.KeyIP)
```
  Near the public storefront registration (after `suppliers.NewPublicHandler(q).RegisterPublic(f)`), add:
```go
	discoverySvc := discovery.NewService(q)
	discoveryHandler := discovery.NewHandler(discoverySvc)
	discoveryHandler.RegisterPublic(f, rlDiscovery)
```
  And in the guarded-handlers section (near `suppliers.NewHandler(q).Register(f, guard...)`):
```go
	discoveryHandler.Register(f, guard...)
```
  Add the import `"github.com/tradebridge/core/internal/discovery"`.

- [ ] **Step 4: Add the projection-safety test** to `internal/discovery/discovery_test.go`. This guards that
  the JSON a public caller receives carries no private company columns. Assert against the marshaled result
  shape:
```go
import (
	"encoding/json"
	// ...existing imports
)

// The public discovery projection must expose only whitelisted keys — never
// private company columns (stripe_customer_id, type, created_at, ...).
func TestDiscoveryProjectionHasNoPrivateColumns(t *testing.T) {
	q, _ := queriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "projprobe-co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: co.ID, Description: sp("projprobe supplier"), Categories: []string{"projprobe"}, Location: sp("NYC"),
	})
	require.NoError(t, err)

	page, err := NewService(q).Search(ctx, Query{Q: "projprobe"})
	require.NoError(t, err)
	require.NotEmpty(t, page.Results)

	b, err := json.Marshal(page.Results[0])
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	allowed := map[string]bool{
		"company_id": true, "name": true, "verification_status": true, "verified_domain": true,
		"description": true, "categories": true, "location": true, "verified": true,
		"avg_rating": true, "review_count": true, "score": true,
	}
	for k := range m {
		require.True(t, allowed[k], "unexpected public field leaked: %s", k)
	}
	forbidden := []string{"stripe_customer_id", "type", "created_at", "updated_at", "email"}
	for _, f := range forbidden {
		_, present := m[f]
		require.False(t, present, "private field must not appear: %s", f)
	}
}
```

- [ ] **Step 5: Build + run the discovery tests:**
```bash
export PATH="$HOME/.local/bin:$PATH"
export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')
go build ./... && go test ./internal/discovery/ -v
```
Expected: all PASS.

- [ ] **Step 6: Run the full Go suite sequentially** (avoid shared-DB flake):
```bash
go test ./... -p 1
```
Expected: green (a known pre-existing flake in `internal/rfq/TestMatchSuppliersVerifiedGate` may appear under
parallelism — `-p 1` avoids it; if it still flakes, re-run that package alone).

- [ ] **Step 7: Commit** — `feat(core): discovery handler + public & authenticated endpoints + rate limit`.

---

## Self-Review checklist

- Migration 025 up/down round-trips; test+live path stays additive (generated columns, no data rewrite).
- Rank weights are Go constants passed as args; the query has no scattered numeric literals beyond the
  clamp/normalize math.
- Public `/public/v1/discovery` returns only whitelisted columns (projection test passes); no `SELECT *` of
  `companies`.
- Empty `q` returns a browse-all list ordered by trust + recency (relevance term 0).
- `verified`, `category`, `location`, `min_rating`, `sort`, paging all filter/order as specified.
- Full suite green under `-p 1`; nothing pushed.
- Generated sqlc field/column Go types matched exactly (no guessed types); `go build ./...` clean.

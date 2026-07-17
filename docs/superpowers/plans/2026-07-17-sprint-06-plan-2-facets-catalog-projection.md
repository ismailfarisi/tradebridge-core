# Sprint 6 · Plan 2 — Facets + Catalog Sub-signal + Projection Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.
> Repo: `/home/deploy/b2b/tradebridge-core`. Builds on Plan 1 (`internal/discovery` exists). Commit per task.

**Goal:** (1) Remove the `search_vector` tsvector artifact that Plan 1's migration rippled into
`SELECT */RETURNING *` response contracts. (2) Add matched-catalog snippets to discovery results. (3) Add
filter-aware facets (`GET /public/v1/discovery/facets`).

**Architecture:** Task 1 pins explicit column lists (dropping `search_vector`) in `suppliers.sql` +
`catalog.sql` and regenerates sqlc. Task 2 adds a `MatchedCatalogForCompanies` query + service enrichment so
each discovery result carries `matched_catalog:[{name,category}]`. Task 3 adds three facet queries
(category / verified / rating, each omitting its own dimension) + a facets service method + endpoints.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5. Postgres FTS.

## Global Constraints

- Merge to `master` **LOCALLY ONLY, never push**.
- DB tests → `TEST_DATABASE_URL` (`tradebridge_core_test`) only. Setup for every shell:
  `export PATH="$HOME/.local/bin:$PATH"; export TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- Public facets/discovery expose ONLY whitelisted public data. Never `SELECT *` of `companies`.
- After editing `.sql`, run `sqlc generate`; never hand-edit `pkg/db/sqlc/*.go`. Confirm generated types and
  make code compile against them (don't guess).
- Facet weights/buckets are named constants where they appear in Go.
- Full suite green under `go test ./... -p 1`. No new migration in this plan (025 already applied).

## Reference (current state after Plan 1)

- `internal/discovery/{rank,service,handler}.go` exist. `Service.Search(ctx, Query) (Page, error)`;
  `Result` struct with public columns + `Score`; `Page{Results,Page,PerPage,Total}`.
- `supplier_profiles` cols: `id, company_id, description, categories, location, verified, created_at,
  updated_at, search_vector`. `catalog_summaries` cols: `id, company_id, name, category, summary,
  created_at, updated_at, search_vector`.
- Discovery query already folds a catalog FTS match into the WHERE (`EXISTS ... cs.search_vector @@ ...`) —
  Task 2 only adds the *snippets*, the match signal is already there.
- Integration harness: `buildAPI(t)`, `doJSON(t, app, method, path, token, body) (int, map[string]any)`,
  `poolOrSkip(t)` (see `test/integration/api_a1_test.go`, `e2e_04_order_lifecycle_test.go:111` for a public
  GET example).

## File Structure

- Modify: `pkg/db/query/suppliers.sql`, `pkg/db/query/catalog.sql` (pin columns), regenerate sqlc.
- Modify: `pkg/db/query/discovery.sql` (add `MatchedCatalogForCompanies` + 3 facet queries), regenerate.
- Modify: `internal/discovery/service.go` (attach `matched_catalog`; add `Facets` method + types).
- Modify: `internal/discovery/handler.go` (add `/discovery/facets` guarded + public routes).
- Modify: `internal/httpapi/router.go` (mount public facets route under the same `rlDiscovery` limiter).
- Create/modify tests: `internal/discovery/discovery_test.go` (facets + matched-catalog),
  `test/integration/discovery_public_test.go` (real HTTP-path projection-leak test).

---

### Task 1: Projection hardening — drop `search_vector` from response contracts

**Files:** `pkg/db/query/suppliers.sql`, `pkg/db/query/catalog.sql`, then `sqlc generate`.

**Interfaces:**
- Produces: `SupplierProfile`, `CatalogSummary`, `SearchSuppliersRow`, `SearchCatalogRow` sqlc structs no
  longer carry a `SearchVector` field. Consumers (`internal/suppliers/handler.go`,
  `internal/catalog/handler.go`) compile unchanged (they return the structs verbatim).

- [ ] **Step 1: Pin explicit columns in `pkg/db/query/suppliers.sql`.** Replace the three star-projections
  (keep query names + all other clauses identical; only the column lists change):

`UpsertSupplierProfile` — change `RETURNING *` to:
```sql
RETURNING id, company_id, description, categories, location, verified, created_at, updated_at;
```
`GetSupplierProfileByCompany` — change `SELECT * FROM supplier_profiles` to:
```sql
SELECT id, company_id, description, categories, location, verified, created_at, updated_at
FROM supplier_profiles
```
`SearchSuppliers` — change `SELECT sp.*, c.name AS company_name, c.verification_status` to:
```sql
SELECT sp.id, sp.company_id, sp.description, sp.categories, sp.location, sp.verified,
       sp.created_at, sp.updated_at,
       c.name AS company_name, c.verification_status
```

- [ ] **Step 2: Pin explicit columns in `pkg/db/query/catalog.sql`.** For `CreateCatalogSummary`
  (`RETURNING *`), `GetCatalogSummary` (`SELECT *`), `ListCatalogByCompany` (`SELECT *`),
  `UpdateCatalogSummary` (`RETURNING *`) use the explicit list:
```sql
id, company_id, name, category, summary, created_at, updated_at
```
  (For the `RETURNING` cases: `RETURNING id, company_id, name, category, summary, created_at, updated_at;`.
  For `SELECT` cases: `SELECT id, company_id, name, category, summary, created_at, updated_at FROM catalog_summaries ...`.)
  For `SearchCatalog` (`SELECT cs.*, c.name AS company_name`) →
```sql
SELECT cs.id, cs.company_id, cs.name, cs.category, cs.summary, cs.created_at, cs.updated_at,
       c.name AS company_name
```

- [ ] **Step 3: Regenerate + build:**
```bash
export PATH="$HOME/.local/bin:$PATH"
sqlc generate && go build ./...
```
Expected: exit 0. Confirm `grep -rn "SearchVector" pkg/db/sqlc/` now only appears in `discovery.sql.go`
(the discovery query still legitimately references `search_vector` in its expressions but does not SELECT it).

- [ ] **Step 4: Add an integration projection-leak test** `test/integration/discovery_public_test.go` — this
  hits the REAL public HTTP endpoint (stronger than a struct-marshal test):
```go
package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The public discovery endpoint must never leak the internal search_vector
// artifact or any private company column.
func TestPublicDiscoveryProjectionIsClean(t *testing.T) {
	api := buildAPI(t)
	pool := poolOrSkip(t)
	_ = pool

	// Register a seller with a searchable profile (registerSeller creates the
	// company + agent credential; then set a profile via the authed route).
	sellerToken, _, _ := registerSeller(t, api, "DiscoveryLeakCo")
	st, _ := doJSON(t, api, "PUT", "/v1/suppliers/me", sellerToken, map[string]any{
		"description": "leakprobe premium widgets supplier",
		"categories":  []string{"leakprobe"},
		"location":    "Berlin",
	})
	require.Equal(t, http.StatusOK, st)

	st, page := doJSON(t, api, "GET", "/public/v1/discovery?q=leakprobe", "", nil)
	require.Equal(t, http.StatusOK, st)
	results, ok := page["results"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, results)

	allowed := map[string]bool{
		"company_id": true, "name": true, "verification_status": true, "verified_domain": true,
		"description": true, "categories": true, "location": true, "verified": true,
		"avg_rating": true, "review_count": true, "score": true, "matched_catalog": true,
	}
	for _, r := range results {
		row := r.(map[string]any)
		for k := range row {
			require.True(t, allowed[k], "unexpected public discovery field leaked: %s", k)
		}
	}
}
```
> NOTE: confirm `registerSeller`'s return order/signature (`test/integration/api_a4_test.go:14`) — it returns
> `(token, companyID, secret)` or similar. Use the token to call `PUT /v1/suppliers/me`. If `registerSeller`
> already creates a supplier profile, adjust the description to include `leakprobe` via the PUT above
> regardless (idempotent upsert).

- [ ] **Step 5: Run tests:**
```bash
go test ./test/integration/ -run TestPublicDiscoveryProjectionIsClean -v
go test ./internal/suppliers/ ./internal/catalog/ -p 1
```
Expected: PASS (the `matched_catalog` key is allowed ahead of Task 2 adding it; until then results simply
won't contain it, which the `allowed` map tolerates).

- [ ] **Step 6: Commit** — `fix(core): pin explicit columns to drop search_vector from response contracts`.

---

### Task 2: Matched-catalog snippets on discovery results

**Files:** `pkg/db/query/discovery.sql`, `internal/discovery/service.go`, `internal/discovery/discovery_test.go`.

**Interfaces:**
- Produces: `sqlc.MatchedCatalogForCompanies(ctx, params) ([]row, error)`; `Result.MatchedCatalog
  []CatalogSnippet` (`{Name, Category string}`), populated only when `q != ""`.

- [ ] **Step 1: Add the query to `pkg/db/query/discovery.sql`:**
```sql
-- name: MatchedCatalogForCompanies :many
-- Catalog items (name/category) matching the query text for a set of companies,
-- used to show WHY a supplier matched. Public columns only.
SELECT company_id, name, category
FROM catalog_summaries
WHERE company_id = ANY(sqlc.arg(company_ids)::text[])
  AND search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
ORDER BY company_id, created_at DESC;
```

- [ ] **Step 2: `sqlc generate` + build.**

- [ ] **Step 3: Enrich `Result` in `internal/discovery/service.go`.** Add:
```go
const maxCatalogSnippets = 3

// CatalogSnippet is a matched catalog item shown as a "why this matched" hint.
type CatalogSnippet struct {
	Name     string `json:"name"`
	Category string `json:"category"`
}
```
  Add `MatchedCatalog []CatalogSnippet `json:"matched_catalog,omitempty"`` to `Result`. After building
  `results` in `Search`, when `q.Q != ""` and `len(results) > 0`, fetch matched catalog rows for the result
  company IDs and attach up to `maxCatalogSnippets` per company:
```go
	if q.Q != "" && len(results) > 0 {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.CompanyID
		}
		rows, err := s.q.MatchedCatalogForCompanies(ctx, sqlc.MatchedCatalogForCompaniesParams{
			CompanyIds: ids,
			Query:      q.Q,
		})
		if err != nil {
			return Page{}, err
		}
		byCompany := make(map[string][]CatalogSnippet, len(rows))
		for _, row := range rows {
			if len(byCompany[row.CompanyID]) >= maxCatalogSnippets {
				continue
			}
			byCompany[row.CompanyID] = append(byCompany[row.CompanyID], CatalogSnippet{Name: row.Name, Category: row.Category})
		}
		for i := range results {
			results[i].MatchedCatalog = byCompany[results[i].CompanyID]
		}
	}
```
> Confirm the generated param struct field names (`CompanyIds` vs `CompanyIDs`, `Query`) in
> `pkg/db/sqlc/discovery.sql.go` and match them exactly.

- [ ] **Step 4: Failing test** in `internal/discovery/discovery_test.go`:
```go
// A supplier whose CATALOG (not profile) matches the query surfaces the matched
// item as a snippet.
func TestDiscoverAttachesMatchedCatalog(t *testing.T) {
	q, _ := queriesOrSkip(t)
	ctx := context.Background()
	tag := "catsnippet"

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "catsnippet-co", Type: "seller"})
	require.NoError(t, err)
	// Profile does NOT contain the tag; only the catalog item does.
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: co.ID, Description: sp("general supplier"), Categories: []string{"misc"}, Location: sp("NYC"),
	})
	require.NoError(t, err)
	_, err = q.CreateCatalogSummary(ctx, sqlc.CreateCatalogSummaryParams{
		CompanyID: co.ID, Name: tag + " widget", Category: "widgets", Summary: sp("a fine " + tag),
	})
	require.NoError(t, err)

	page, err := NewService(q).Search(ctx, Query{Q: tag})
	require.NoError(t, err)

	var found *Result
	for i := range page.Results {
		if page.Results[i].CompanyID == co.ID {
			found = &page.Results[i]
		}
	}
	require.NotNil(t, found, "catalog-only match must appear in discovery")
	require.NotEmpty(t, found.MatchedCatalog, "matched catalog snippet attached")
	require.Contains(t, found.MatchedCatalog[0].Name, tag)
}
```
> Confirm `CreateCatalogSummaryParams` field types (`Summary` is `*string`). Use `sp(...)` for it.

- [ ] **Step 5: Run** `go test ./internal/discovery/ -v` → all PASS.
- [ ] **Step 6: Commit** — `feat(core): attach matched-catalog snippets to discovery results`.

---

### Task 3: Filter-aware facets + endpoints

**Files:** `pkg/db/query/discovery.sql`, `internal/discovery/service.go`, `internal/discovery/handler.go`,
`internal/httpapi/router.go`, `internal/discovery/discovery_test.go`.

**Interfaces:**
- Produces: `Service.Facets(ctx, Query) (Facets, error)`; endpoints `GET /public/v1/discovery/facets`
  (rate-limited) + `GET /v1/discovery/facets` (guarded).
- `Facets{Categories []FacetCount `json:"categories"`; Verified VerifiedFacet `json:"verified"`;
  RatingBuckets []RatingBucket `json:"rating_buckets"`}`,
  `FacetCount{Value string; Count int64}`, `VerifiedFacet{True,False int64}`,
  `RatingBucket{Min float64; Count int64}`.

- [ ] **Step 1: Add three facet queries to `pkg/db/query/discovery.sql`.** Each applies the shared
  text/location filter plus every OTHER dimension, omitting its own so the user can widen it. (The `rep` CTE
  is repeated per query — that's fine.)

```sql
-- name: DiscoverCategoryFacets :many
-- Category counts; omits the category filter (so the user can switch category).
WITH rep AS (
    SELECT reviewee_company_id AS company_id, AVG(rating)::float8 AS avg_rating
    FROM reviews WHERE status = 'visible' GROUP BY reviewee_company_id
),
matched AS (
    SELECT sp.company_id, sp.categories
    FROM supplier_profiles sp
    JOIN companies c ON c.id = sp.company_id
    LEFT JOIN rep ON rep.company_id = sp.company_id
    WHERE (sqlc.arg(query)::text = ''
           OR sp.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
           OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
           OR EXISTS (SELECT 1 FROM catalog_summaries cs WHERE cs.company_id = sp.company_id
                      AND cs.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)))
      AND (sqlc.arg(location)::text = '' OR sp.location ILIKE '%' || sqlc.arg(location)::text || '%')
      AND (NOT sqlc.arg(verified_only)::bool OR sp.verified)
      AND (sqlc.arg(min_rating)::float8 <= 0 OR COALESCE(rep.avg_rating, 0) >= sqlc.arg(min_rating)::float8)
)
SELECT cat AS value, COUNT(*)::bigint AS count
FROM matched, unnest(matched.categories) AS cat
GROUP BY cat
ORDER BY count DESC, value
LIMIT 50;

-- name: DiscoverVerifiedFacets :one
-- Verified vs unverified counts; omits the verified filter.
WITH rep AS (
    SELECT reviewee_company_id AS company_id, AVG(rating)::float8 AS avg_rating
    FROM reviews WHERE status = 'visible' GROUP BY reviewee_company_id
)
SELECT
    COUNT(*) FILTER (WHERE sp.verified)::bigint     AS verified_count,
    COUNT(*) FILTER (WHERE NOT sp.verified)::bigint AS unverified_count
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
LEFT JOIN rep ON rep.company_id = sp.company_id
WHERE (sqlc.arg(query)::text = ''
       OR sp.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
       OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
       OR EXISTS (SELECT 1 FROM catalog_summaries cs WHERE cs.company_id = sp.company_id
                  AND cs.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)))
  AND (sqlc.arg(location)::text = '' OR sp.location ILIKE '%' || sqlc.arg(location)::text || '%')
  AND (sqlc.arg(category)::text = ''
       OR EXISTS (SELECT 1 FROM unnest(sp.categories) cat WHERE cat ILIKE sqlc.arg(category)::text))
  AND (sqlc.arg(min_rating)::float8 <= 0 OR COALESCE(rep.avg_rating, 0) >= sqlc.arg(min_rating)::float8);

-- name: DiscoverRatingFacets :one
-- Count of suppliers at or above each rating threshold; omits the min_rating filter.
WITH rep AS (
    SELECT reviewee_company_id AS company_id, AVG(rating)::float8 AS avg_rating
    FROM reviews WHERE status = 'visible' GROUP BY reviewee_company_id
)
SELECT
    COUNT(*) FILTER (WHERE COALESCE(rep.avg_rating, 0) >= 4)::bigint AS gte4,
    COUNT(*) FILTER (WHERE COALESCE(rep.avg_rating, 0) >= 3)::bigint AS gte3,
    COUNT(*) FILTER (WHERE COALESCE(rep.avg_rating, 0) >= 2)::bigint AS gte2,
    COUNT(*) FILTER (WHERE COALESCE(rep.avg_rating, 0) >= 1)::bigint AS gte1
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
LEFT JOIN rep ON rep.company_id = sp.company_id
WHERE (sqlc.arg(query)::text = ''
       OR sp.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)
       OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
       OR EXISTS (SELECT 1 FROM catalog_summaries cs WHERE cs.company_id = sp.company_id
                  AND cs.search_vector @@ websearch_to_tsquery('english', sqlc.arg(query)::text)))
  AND (sqlc.arg(location)::text = '' OR sp.location ILIKE '%' || sqlc.arg(location)::text || '%')
  AND (sqlc.arg(category)::text = ''
       OR EXISTS (SELECT 1 FROM unnest(sp.categories) cat WHERE cat ILIKE sqlc.arg(category)::text))
  AND (NOT sqlc.arg(verified_only)::bool OR sp.verified);
```

- [ ] **Step 2: `sqlc generate` + build.**

- [ ] **Step 3: Add `Facets` types + method to `internal/discovery/service.go`:**
```go
type FacetCount struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}
type VerifiedFacet struct {
	True  int64 `json:"true"`
	False int64 `json:"false"`
}
type RatingBucket struct {
	Min   float64 `json:"min"`
	Count int64   `json:"count"`
}
type Facets struct {
	Categories    []FacetCount   `json:"categories"`
	Verified      VerifiedFacet  `json:"verified"`
	RatingBuckets []RatingBucket `json:"rating_buckets"`
}

// Facets returns filter-aware facet counts for the current query (each facet
// dimension omits its own filter so the user can widen it).
func (s *Service) Facets(ctx context.Context, q Query) (Facets, error) {
	q.normalize()
	cats, err := s.q.DiscoverCategoryFacets(ctx, sqlc.DiscoverCategoryFacetsParams{
		Query: q.Q, Location: q.Location, VerifiedOnly: q.VerifiedOnly, MinRating: q.MinRating,
	})
	if err != nil {
		return Facets{}, err
	}
	ver, err := s.q.DiscoverVerifiedFacets(ctx, sqlc.DiscoverVerifiedFacetsParams{
		Query: q.Q, Location: q.Location, Category: q.Category, MinRating: q.MinRating,
	})
	if err != nil {
		return Facets{}, err
	}
	rat, err := s.q.DiscoverRatingFacets(ctx, sqlc.DiscoverRatingFacetsParams{
		Query: q.Q, Location: q.Location, Category: q.Category, VerifiedOnly: q.VerifiedOnly,
	})
	if err != nil {
		return Facets{}, err
	}
	out := Facets{
		Categories: make([]FacetCount, 0, len(cats)),
		Verified:   VerifiedFacet{True: ver.VerifiedCount, False: ver.UnverifiedCount},
		RatingBuckets: []RatingBucket{
			{Min: 4, Count: rat.Gte4}, {Min: 3, Count: rat.Gte3},
			{Min: 2, Count: rat.Gte2}, {Min: 1, Count: rat.Gte1},
		},
	}
	for _, c := range cats {
		out.Categories = append(out.Categories, FacetCount{Value: c.Value, Count: c.Count})
	}
	return out, nil
}
```
> Confirm generated field names (`VerifiedCount`, `UnverifiedCount`, `Gte4`… , `c.Value`, `c.Count`) against
> `pkg/db/sqlc/discovery.sql.go` and adjust to match exactly.

- [ ] **Step 4: Add facets routes to `internal/discovery/handler.go`:**
```go
func (h *Handler) facets(c *fiber.Ctx) error {
	f, err := h.svc.Facets(c.Context(), parseQuery(c))
	if err != nil {
		return err
	}
	return c.JSON(f)
}
```
  In `Register` add `g.Get("/facets", h.facets)`. In `RegisterPublic` add a second public route with the same
  middleware:
```go
func (h *Handler) RegisterPublic(app *fiber.App, mw ...fiber.Handler) {
	search := append(append([]fiber.Handler{}, mw...), h.search)
	app.Get("/public/v1/discovery", search...)
	facets := append(append([]fiber.Handler{}, mw...), h.facets)
	app.Get("/public/v1/discovery/facets", facets...)
}
```
  (Router already calls `discoveryHandler.RegisterPublic(f, rlDiscovery)` and
  `discoveryHandler.Register(f, guard...)` — no router change needed beyond what Plan 1 wired; verify both
  new routes appear.)

- [ ] **Step 5: Failing test** in `internal/discovery/discovery_test.go`:
```go
// Facets reflect the filtered set and each dimension omits its own filter.
func TestFacetsReflectFilterSet(t *testing.T) {
	q, _ := queriesOrSkip(t)
	ctx := context.Background()
	tag := "facetprobe"

	mk := func(name, cat string, verified bool) {
		co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: name, Type: "seller"})
		require.NoError(t, err)
		_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
			CompanyID: co.ID, Description: sp(tag + " supplier"), Categories: []string{cat}, Location: sp("Rome"),
		})
		require.NoError(t, err)
		if verified {
			require.NoError(t, q.SyncSupplierVerified(ctx, sqlc.SyncSupplierVerifiedParams{CompanyID: co.ID, Verified: true}))
		}
	}
	mk("facetprobe-a", tag+"-cat1", true)
	mk("facetprobe-b", tag+"-cat1", false)
	mk("facetprobe-c", tag+"-cat2", false)

	f, err := NewService(q).Facets(ctx, Query{Q: tag})
	require.NoError(t, err)

	counts := map[string]int64{}
	for _, c := range f.Categories {
		counts[c.Value] = c.Count
	}
	require.Equal(t, int64(2), counts[tag+"-cat1"])
	require.Equal(t, int64(1), counts[tag+"-cat2"])
	require.GreaterOrEqual(t, f.Verified.True, int64(1))
	require.GreaterOrEqual(t, f.Verified.False, int64(2))
}
```

- [ ] **Step 6: Run** discovery + integration tests, then full suite:
```bash
go test ./internal/discovery/ -v
go test ./test/integration/ -run TestPublicDiscoveryProjectionIsClean -v
go test ./... -p 1
```
Expected: green.

- [ ] **Step 7: Commit** — `feat(core): filter-aware discovery facets (category/verified/rating)`.

---

## Self-Review checklist

- `grep -rn "SearchVector" pkg/db/sqlc/` appears ONLY in `discovery.sql.go`; `/v1/suppliers/me`,
  `/v1/suppliers/search`, and catalog endpoints no longer emit `search_vector` (integration test proves the
  public discovery path is clean).
- Discovery results carry `matched_catalog` only when `q != ""`; capped at 3/company.
- Facets: category counts omit the category filter; verified counts omit the verified filter; rating buckets
  omit min_rating — each still applies the other dimensions + text/location.
- Public facets route is rate-limited (shares `rlDiscovery`) and leaks no private columns.
- Full suite green under `-p 1`; nothing pushed; no new migration.

# Sprint 3 — Plan 2: Public Storefront & Badge Data — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Expose a public, tenant-safe seller storefront read API, and surface each seller's `verification_status` on the buyer's quote list and the supplier search so the frontend can render the verified badge.

**Architecture:** A single allow-list SQL query (`GetPublicSeller`) is the security boundary — it selects only public columns, so no private field (emails, stripe ids, subscription) can leak. The public route mounts with NO tenant guard (it's unauthenticated); it will be rate-limited in Plan 3. Quote/search reads gain `verification_status` from the already-joined `companies` row.

**Tech Stack:** Go / Fiber v2 / pgx v5 / sqlc. DB tests via `TEST_DATABASE_URL` (skip when unset; the execution env sets it).

## Global Constraints

- The public seller endpoint returns ONLY: `company_id, name, verification_status, verified_domain, description, categories, location, catalog[{name,category,summary}], reputation(=null)`. No users, emails, `stripe_customer_id`, subscription, or other private fields — enforced by selecting an explicit column list, never `SELECT *` of `companies`.
- `verification_status` values are exactly `unverified|pending|verified|rejected` (from Plan 1).
- 404 when the company has no `supplier_profiles` row (not a storefront) or does not exist.
- Follow existing handler patterns (`NewHandler(...).Register(app, ...)`); the public route is registered like `waitlist` (no guard).
- Do not change existing quote ordering or the accept flow — only add a field.

---

### Task 1: Public storefront read API

**Files:**
- Modify: `pkg/db/query/suppliers.sql` (add `GetPublicSeller`)
- Modify (generated): `pkg/db/sqlc/*` via `sqlc generate`
- Create: `internal/suppliers/public.go`
- Create: `internal/suppliers/public_test.go`
- Modify: `internal/httpapi/router.go` (register the public route, no guard)

**Interfaces:**
- Consumes: `companies.verification_status/verified_domain` (Plan 1), `supplier_profiles`, `catalog_summaries` (`ListCatalogByCompany`).
- Produces: `GET /public/v1/sellers/:id`; `func (h *PublicHandler) RegisterPublic(app *fiber.App)`.

- [ ] **Step 1: Add the allow-list query**

Append to `pkg/db/query/suppliers.sql`:
```sql
-- name: GetPublicSeller :one
-- Tenant-safe storefront projection: explicit public columns only (no SELECT *
-- of companies, which would leak stripe_customer_id etc.).
SELECT c.id AS company_id,
       c.name,
       c.verification_status,
       c.verified_domain,
       sp.description,
       sp.categories,
       sp.location
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
WHERE sp.company_id = @company_id;
```
Run `sqlc generate`.

- [ ] **Step 2: Write the failing test**

`internal/suppliers/public_test.go`:
```go
package suppliers

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func pubQueriesOrSkip(t *testing.T) *sqlc.Queries {
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

func TestPublicSellerReturnsSafeFieldsOnly(t *testing.T) {
	q := pubQueriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "Public Widgets Co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: co.ID, Description: sp("we sell widgets"), Categories: []string{"widgets"}, Location: sp("NYC"),
	})
	require.NoError(t, err)
	_, err = q.CreateCatalogSummary(ctx, sqlc.CreateCatalogSummaryParams{
		CompanyID: co.ID, Name: "Widget A", Category: "widgets", Summary: sp("the A widget"),
	})
	require.NoError(t, err)

	app := fiber.New()
	NewPublicHandler(q).RegisterPublic(app)

	req := httptest.NewRequest("GET", "/public/v1/sellers/"+co.ID, nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	// Present, safe fields:
	require.Equal(t, "Public Widgets Co", out["name"])
	require.Equal(t, "unverified", out["verification_status"])
	require.Contains(t, out, "catalog")
	require.Nil(t, out["reputation"])
	// Private fields must NOT appear anywhere in the top-level object:
	for _, banned := range []string{"stripe_customer_id", "email", "users", "type", "created_at", "subscription"} {
		_, present := out[banned]
		require.False(t, present, "public seller response must not expose %q", banned)
	}
}

func TestPublicSellerNotFoundWithoutProfile(t *testing.T) {
	q := pubQueriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "No Storefront Co", Type: "seller"})
	require.NoError(t, err)

	app := fiber.New()
	NewPublicHandler(q).RegisterPublic(app)
	req := httptest.NewRequest("GET", "/public/v1/sellers/"+co.ID, nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode)
}

func sp(s string) *string { return &s }
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/suppliers/ -run TestPublicSeller -v`
Expected: FAIL (compile error — `NewPublicHandler` undefined).

- [ ] **Step 4: Implement the public handler**

`internal/suppliers/public.go`:
```go
package suppliers

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// PublicHandler serves the unauthenticated seller storefront. It exposes only a
// tenant-safe projection (see GetPublicSeller) plus a bounded catalog showcase.
type PublicHandler struct {
	q *sqlc.Queries
}

func NewPublicHandler(q *sqlc.Queries) *PublicHandler {
	return &PublicHandler{q: q}
}

// RegisterPublic mounts the public route with NO tenant guard.
func (h *PublicHandler) RegisterPublic(app *fiber.App) {
	app.Get("/public/v1/sellers/:id", h.getSeller)
}

const storefrontCatalogLimit = 24

type catalogItem struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Summary  *string `json:"summary"`
}

func (h *PublicHandler) getSeller(c *fiber.Ctx) error {
	id := c.Params("id")
	s, err := h.q.GetPublicSeller(c.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "seller not found")
	} else if err != nil {
		return err
	}

	rows, err := h.q.ListCatalogByCompany(c.Context(), id)
	if err != nil {
		return err
	}
	catalog := make([]catalogItem, 0, len(rows))
	for i, r := range rows {
		if i >= storefrontCatalogLimit {
			break
		}
		catalog = append(catalog, catalogItem{Name: r.Name, Category: r.Category, Summary: r.Summary})
	}

	return c.JSON(fiber.Map{
		"company_id":          s.CompanyID,
		"name":                s.Name,
		"verification_status": s.VerificationStatus,
		"verified_domain":     s.VerifiedDomain,
		"description":         s.Description,
		"categories":          s.Categories,
		"location":            s.Location,
		"catalog":             catalog,
		"reputation":          nil, // ratings land Sprint 4
	})
}
```
Note: confirm the `ListCatalogByCompany` row field names (`Name`, `Category`, `Summary`) and `GetPublicSellerRow` field names via the generated code; adjust casing if sqlc differs.

- [ ] **Step 5: Register the public route**

In `internal/httpapi/router.go`, near the `waitlist.NewHandler(q).Register(f)` line (the other public route), add:
```go
	// Public seller storefront (unauthenticated; rate-limited in Plan 3).
	suppliers.NewPublicHandler(q).RegisterPublic(f)
```

- [ ] **Step 6: Run tests + build**

Run: `go build ./... && go test ./internal/suppliers/ -v`
Expected: build clean; PASS.

- [ ] **Step 7: Commit**

```bash
git add pkg/db/query/suppliers.sql pkg/db/sqlc internal/suppliers/public.go internal/suppliers/public_test.go internal/httpapi/router.go
git commit -m "feat(trust): public seller storefront read API (tenant-safe allow-list)"
```

---

### Task 2: Verified badge data on quote reads

**Files:**
- Modify: `pkg/db/query/quotes.sql` (`RankQuotesByRFQ` adds `c.verification_status`)
- Modify (generated): `pkg/db/sqlc/*`
- Modify: `internal/quotes/handler.go` (`quoteView` gains `SellerVerificationStatus`; populate in `listForRFQ`)
- Test: `internal/quotes/badge_test.go`

**Interfaces:**
- Produces: quote list JSON includes `seller_verification_status` per quote.

- [ ] **Step 1: Write the failing test**

`internal/quotes/badge_test.go`:
```go
package quotes

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestRankQuotesIncludesVerificationStatus(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	ctx := context.Background()

	buyer, _ := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "badge-buyer", Type: "buyer"})
	seller, _ := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "badge-seller", Type: "seller"})
	// rfqs.created_by is a FK to users(id); email is UNIQUE so key it off the
	// (per-run unique) company ULID to stay idempotent across test-DB reuse.
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@badge.test", Role: "owner"})
	require.NoError(t, err)
	rfqRow, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need widgets", RawText: "widgets"})
	require.NoError(t, err)
	_, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqRow.ID, SellerCompanyID: seller.ID, PriceCents: 1000, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)

	rows, err := q.RankQuotesByRFQ(ctx, rfqRow.ID)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, "unverified", rows[0].VerificationStatus, "quote rows carry the seller's verification status")
}
```
Note: confirm `CreateRFQ` / `UpsertQuote` param names from the generated code (`git grep "CreateRFQParams\|UpsertQuoteParams"`), and adjust field names. If `CreateRFQ` requires more non-null fields, supply them. The assertion field name (`VerificationStatus`) must match the sqlc-generated column alias — align the SQL alias accordingly.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/quotes/ -run TestRankQuotesIncludesVerificationStatus -v`
Expected: FAIL (`VerificationStatus` field does not exist on the row).

- [ ] **Step 3: Update the query**

In `pkg/db/query/quotes.sql`, change `RankQuotesByRFQ` to:
```sql
-- name: RankQuotesByRFQ :many
SELECT q.*, c.name AS seller_name, c.verification_status
FROM quotes q
JOIN companies c ON c.id = q.seller_company_id
WHERE q.rfq_id = $1
ORDER BY q.price_cents ASC, q.delivery_days ASC NULLS LAST, q.completeness DESC, q.created_at ASC;
```
Run `sqlc generate`.

- [ ] **Step 4: Surface it in the handler**

In `internal/quotes/handler.go`, add to `quoteView`:
```go
	SellerVerificationStatus string `json:"seller_verification_status,omitempty"`
```
and in `listForRFQ`'s loop set `SellerVerificationStatus: q.VerificationStatus`.

- [ ] **Step 5: Run tests + build**

Run: `go build ./... && go test ./internal/quotes/ -v`
Expected: build clean; PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/db/query/quotes.sql pkg/db/sqlc internal/quotes/handler.go internal/quotes/badge_test.go
git commit -m "feat(trust): surface seller verification_status on ranked quotes"
```

---

### Task 3: Verified status on supplier search

**Files:**
- Modify: `pkg/db/query/suppliers.sql` (`SearchSuppliers` adds `c.verification_status`)
- Modify (generated): `pkg/db/sqlc/*`
- Test: `internal/suppliers/search_badge_test.go`

**Interfaces:**
- Produces: search rows include `verification_status` (badge data; ordering unchanged — still `verified DESC, name`).

- [ ] **Step 1: Write the failing test**

`internal/suppliers/search_badge_test.go`:
```go
package suppliers

import (
	"context"
	"testing"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/stretchr/testify/require"
)

func TestSearchSuppliersIncludesVerificationStatus(t *testing.T) {
	q := pubQueriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "search-badge-co-unique", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: co.ID, Description: sp("searchable badge widgets"), Categories: []string{"badgewidgets"}, Location: sp("LA"),
	})
	require.NoError(t, err)

	rows, err := q.SearchSuppliers(ctx, sqlc.SearchSuppliersParams{Query: "badgewidgets", Lim: 10, Off: 0})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	found := false
	for _, r := range rows {
		if r.CompanyID == co.ID {
			found = true
			require.Equal(t, "unverified", r.VerificationStatus)
		}
	}
	require.True(t, found)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/suppliers/ -run TestSearchSuppliersIncludesVerificationStatus -v`
Expected: FAIL (`VerificationStatus` field absent).

- [ ] **Step 3: Update the query**

In `pkg/db/query/suppliers.sql`, change the `SearchSuppliers` SELECT to add `c.verification_status`:
```sql
-- name: SearchSuppliers :many
SELECT sp.*, c.name AS company_name, c.verification_status
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
WHERE (
    sqlc.arg(query)::text = ''
    OR c.name ILIKE '%' || sqlc.arg(query)::text || '%'
    OR sp.description ILIKE '%' || sqlc.arg(query)::text || '%'
    OR sp.location ILIKE '%' || sqlc.arg(query)::text || '%'
    OR EXISTS (
        SELECT 1 FROM unnest(sp.categories) cat
        WHERE cat ILIKE '%' || sqlc.arg(query)::text || '%'
    )
)
ORDER BY sp.verified DESC, c.name
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);
```
Run `sqlc generate`.

- [ ] **Step 4: Run tests + build + full suite**

Run: `go build ./... && go test ./...`
Expected: build clean; full suite green.

- [ ] **Step 5: Commit**

```bash
git add pkg/db/query/suppliers.sql pkg/db/sqlc internal/suppliers/search_badge_test.go
git commit -m "feat(trust): include seller verification_status in supplier search rows"
```

---

## Deferred / follow-ups

- Rate-limiting the public storefront endpoint — Plan 3.
- Pretty profile slugs (`/s/acme`) — later.
- Reputation payload (ratings/reviews) — Sprint 4 fills the `reputation` slot.
- Frontend rendering (storefront page, badge component) — Plan 4.

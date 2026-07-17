# Sprint 4 · Plan 3 — Ratings & Reviews Implementation Plan

> **For agentic workers:** TDD, commit per task. Follow `internal/trust` (service+handler split),
> `internal/orders` (party checks), `internal/suppliers/public.go` (unauth storefront).

**Goal:** After an order is `completed`, either party may leave a 1–5 star review of the other.
Reviews are visible immediately; the seller's aggregate rating appears on the public storefront (filling
the existing `reputation` slot) and as a badge on the buyer's quote-compare table. An admin can remove
a review.

**Architecture:** New `internal/reviews` package: `Service` (create with completed-order + party +
direction rules, list, reputation, admin remove) + party `Handler` + a public reviews route. The
storefront handler and the ranked-quotes query gain the seller's aggregate rating. Migration 021 adds
`reviews`.

**Tech Stack:** Go, Fiber, sqlc, pgx/v5, golang-migrate.

## Global Constraints

- Merge to `master` LOCALLY ONLY, never push.
- DB tests → `tradebridge_core_test` only. Derive URL secret-free:
  `TEST_DATABASE_URL=$(grep '^DATABASE_URL=' .env | cut -d= -f2- | sed -E 's#/tradebridge_core(\?|$)#/tradebridge_core_test\1#')`
- Apply migration 021 to the test DB before DB tests. Builds on 019/020.
- `sqlc generate` after query edits; keep `go build ./...` clean.
- sqlc struct: `reviews`→`sqlc.Review`. Money/ids follow existing conventions.
- Direction is derived server-side, NEVER trusted from the client.

## File Structure

- Create: `pkg/db/migrations/021_reviews.up.sql`, `021_reviews.down.sql`
- Create: `pkg/db/query/reviews.sql`
- Create: `internal/reviews/service.go`, `internal/reviews/handler.go`, `internal/reviews/admin.go`
- Create: `internal/reviews/service_test.go` (DB), `internal/reviews/handler_test.go` (light)
- Modify: `pkg/db/query/quotes.sql` (RankQuotesByRFQ + seller rating), `internal/quotes/handler.go` (quoteView fields)
- Modify: `internal/suppliers/public.go` (fill reputation + add public reviews route)
- Modify: `internal/notify/notify.go` (+`TypeReviewReceived`), `internal/notify/handler.go` (validEventType)
- Modify: `internal/httpapi/router.go` (register reviews handler, admin remove, public reviews)

## Migration 021

**`021_reviews.up.sql`:**
```sql
-- Post-completion reputation: each party may review the other once per order.
-- direction is buyer_on_seller | seller_on_buyer; reviewee_company_id is the
-- subject. status flips to 'removed' by an admin (drops out of aggregates/lists).
CREATE TABLE reviews (
    id                  TEXT PRIMARY KEY DEFAULT gen_ulid(),
    order_id            TEXT NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    reviewer_company_id TEXT NOT NULL REFERENCES companies(id),
    reviewee_company_id TEXT NOT NULL REFERENCES companies(id),
    direction           TEXT NOT NULL CHECK (direction IN ('buyer_on_seller','seller_on_buyer')),
    rating              INT  NOT NULL CHECK (rating BETWEEN 1 AND 5),
    body                TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'visible' CHECK (status IN ('visible','removed')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (order_id, reviewer_company_id)
);

CREATE INDEX idx_reviews_reviewee ON reviews (reviewee_company_id);
```

**`021_reviews.down.sql`:**
```sql
DROP TABLE IF EXISTS reviews;
```

## Queries — `pkg/db/query/reviews.sql`

```sql
-- name: CreateReview :one
INSERT INTO reviews (order_id, reviewer_company_id, reviewee_company_id, direction, rating, body)
VALUES (@order_id, @reviewer_company_id, @reviewee_company_id, @direction, @rating, @body)
RETURNING *;

-- name: GetReview :one
SELECT * FROM reviews WHERE id = @id;

-- name: ListReviewsForOrder :many
SELECT * FROM reviews WHERE order_id = @order_id ORDER BY created_at ASC;

-- name: ListVisibleReviewsForCompany :many
SELECT r.*, c.name AS reviewer_name
FROM reviews r JOIN companies c ON c.id = r.reviewer_company_id
WHERE r.reviewee_company_id = @company_id AND r.status = 'visible'
ORDER BY r.created_at DESC;

-- name: GetCompanyReputation :one
SELECT COALESCE(AVG(rating), 0)::float8 AS avg_rating, COUNT(*)::bigint AS review_count
FROM reviews WHERE reviewee_company_id = @company_id AND status = 'visible';

-- name: RemoveReview :one
UPDATE reviews SET status = 'removed' WHERE id = @id RETURNING *;
```

Run `sqlc generate`; confirm `GetCompanyReputation` returns a row struct with `AvgRating float64` and
`ReviewCount int64` (adjust field references in code to the generated names).

## Notify addition

`internal/notify/notify.go`: add `TypeReviewReceived = "review.received"`. Add it to `validEventType`.

---

### Task 1: Reviews service (create + list + reputation + remove)

**Files:** `internal/reviews/service.go`, `internal/reviews/service_test.go`.

**Interfaces (Produces):**
- `type Service struct{...}` + `func NewService(q *sqlc.Queries, notifier *notify.Notifier, logger zerolog.Logger) *Service`
- Errors: `ErrNotFound`, `ConflictError{Msg}`, `ForbiddenError{Msg}`, `ValidationError{Msg}` (mirror `internal/orders`).
- `func (s *Service) Create(ctx, orderID, reviewerCompanyID string, rating int, body string) (sqlc.Review, error)`
- `func (s *Service) ListForOrder(ctx, orderID, companyID string) ([]sqlc.Review, error)` (party-only)
- `func (s *Service) Reputation(ctx, companyID string) (avg float64, count int64, err error)`
- `func (s *Service) Remove(ctx, reviewID string) (sqlc.Review, error)` (admin)

**Create rules (in order):**
1. `rating < 1 || rating > 5` → `ValidationError`.
2. `GetOrder`; `pgx.ErrNoRows` → `ErrNotFound`.
3. Determine reviewer role: `reviewerCompanyID==order.BuyerCompanyID` → buyer; `==SellerCompanyID` → seller;
   else `ErrNotFound` (non-party, no leak).
4. `order.Status != "completed"` → `ConflictError{"order is not completed"}`.
5. Derive: buyer → `reviewee=order.SellerCompanyID, direction="buyer_on_seller"`;
   seller → `reviewee=order.BuyerCompanyID, direction="seller_on_buyer"`.
6. `CreateReview{...}`; a unique violation (pgcode 23505) → `ConflictError{"already reviewed this order"}`
   (map like `internal/trust` maps 23505; use `errors.As` on `*pgconn.PgError` and check `.Code=="23505"`).
7. After success: if notifier != nil, `NotifyCompany(reviewee, Event{Type: TypeReviewReceived,
   Title:"New review", Body:"You received a "+stars+"-star review.", Data:{order_id, rating}, CTAPath:"/orders/"+orderID, CTALabel:"View order"})`.

- [ ] **Step 1: Write failing DB tests** — `service_test.go`, harness like `internal/orders/orders_db_test.go`
  (copy a local `seedCompletedOrder` that walks an order to `completed` via `SetOrderStatus`, or inserts
  an order then sets status directly with `q.SetOrderStatus(... FromStatus...)` chain; simplest: create the
  order then `UPDATE`/use `SetOrderStatus` through the machine). Tests:
  - `TestReviewOnlyAfterCompleted`: order in `created` → buyer Create → `ConflictError`.
  - `TestReviewHappyBuyerOnSeller`: complete order; buyer Create(5,"great"); assert row `direction=="buyer_on_seller"`,
    `reviewee==seller`, `rating==5`; `Reputation(seller)` → avg 5.0, count 1.
  - `TestReviewSellerOnBuyer`: seller Create(4,"") → `direction=="seller_on_buyer"`, reviewee==buyer.
  - `TestReviewDuplicateRejected`: buyer Create twice → 2nd `ConflictError`.
  - `TestReviewNonParty`: stranger Create → `ErrNotFound`.
  - `TestReviewRatingBounds`: rating 0 and 6 → `ValidationError`.
  - `TestRemoveDropsFromReputation`: after buyer review (count 1), `Remove(id)` → `Reputation(seller)` count 0.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** the service.
- [ ] **Step 4: Run, verify pass.**
- [ ] **Step 5: Commit** — `feat(reviews): post-completion review service with reputation + admin remove`.

---

### Task 2: Party HTTP routes + admin remove

**Files:** `internal/reviews/handler.go`, `internal/reviews/admin.go`, light `handler_test.go`.

**Party handler:**
```go
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/orders", guard...)
	g.Post("/:id/reviews", h.create)
	g.Get("/:id/reviews", h.list)
}
```
- `create`: parse `{rating int, body string}`; `h.svc.Create(ctx, id, middleware.CompanyID(c), rating, body)`;
  map `ValidationError`→400, `ConflictError`→409, `ForbiddenError`→403, `ErrNotFound`→404; success 200 `{review}`.
- `list`: `h.svc.ListForOrder(ctx, id, companyID)`; 404 on non-party; `{reviews}`.

**Admin handler** (`admin.go`): `RegisterAdmin(app, adminGuard...)` mounts
`POST /admin/v1/reviews/:id/remove` → `h.svc.Remove`; `pgx.ErrNoRows`→404; `{review}`.

- [ ] **Step 1: Write a light handler test** (fake guard injecting a company id via the locals key from
  `pkg/middleware/tenant.go`) asserting: create on a completed order → 200; create on a non-completed →
  409; list → 200. If fake-guard wiring is awkward, keep service tests authoritative and make this a
  compile/smoke test.
- [ ] **Step 2–4: fail → implement → pass.**
- [ ] **Step 5: Commit** — `feat(reviews): party review routes + admin remove route`.

---

### Task 3: Seller rating on storefront + quote-compare + public reviews

**Files:** `internal/suppliers/public.go`, `pkg/db/query/quotes.sql`, `internal/quotes/handler.go`.

**3a — Storefront reputation slot.** In `internal/suppliers/public.go`:
- Add a public reviews route to `RegisterPublic`: `app.Get("/public/v1/sellers/:id/reviews", h.getReviews)`.
- In `getSeller`, replace `"reputation": nil` with a computed value:
```go
	rep, err := h.q.GetCompanyReputation(c.Context(), id)
	if err != nil {
		return err
	}
	// ... in the JSON map:
	"reputation": fiber.Map{"avg_rating": rep.AvgRating, "review_count": rep.ReviewCount},
```
- `getReviews`: `h.q.ListVisibleReviewsForCompany(ctx, id)` → JSON `{reviews:[{id, rating, body, reviewer_name, created_at}]}` (map only safe fields; do NOT dump internal ids beyond what's needed).

**3b — Quote-compare badge.** In `pkg/db/query/quotes.sql`, replace the `RankQuotesByRFQ` SELECT with:
```sql
-- name: RankQuotesByRFQ :many
SELECT q.*, c.name AS seller_name, c.verification_status,
       COALESCE(rep.avg_rating, 0)::float8 AS seller_avg_rating,
       COALESCE(rep.review_count, 0)::bigint AS seller_review_count
FROM quotes q
JOIN companies c ON c.id = q.seller_company_id
LEFT JOIN LATERAL (
    SELECT AVG(rating)::float8 AS avg_rating, COUNT(*)::bigint AS review_count
    FROM reviews WHERE reviewee_company_id = q.seller_company_id AND status = 'visible'
) rep ON true
WHERE q.rfq_id = $1
ORDER BY q.price_cents ASC, q.delivery_days ASC NULLS LAST, q.completeness DESC, q.created_at ASC;
```
`sqlc generate`. Then in `internal/quotes/handler.go`:
- Add to `quoteView`: `SellerAvgRating float64 \`json:"seller_avg_rating"\`` and
  `SellerReviewCount int64 \`json:"seller_review_count"\``.
- In the `listForRFQ` mapping, set `SellerAvgRating: q.SellerAvgRating, SellerReviewCount: q.SellerReviewCount`.

- [ ] **Step 1: Write/extend tests.** In `internal/quotes` (badge_test.go pattern) assert a ranked quote
  row carries `SellerAvgRating`/`SellerReviewCount` (seed a completed order + a buyer_on_seller review for
  the seller, then rank quotes for a fresh RFQ where that seller quoted; assert avg reflects the review).
  In `internal/suppliers/public_test.go` assert the storefront JSON `reputation` is now an object with
  `review_count` (seed a review) — and the public `/reviews` route returns the seeded review.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** 3a + 3b.
- [ ] **Step 4: Run, verify pass** — `go test ./internal/quotes/ ./internal/suppliers/ -count=1 -v`.
- [ ] **Step 5: Commit** — `feat(reviews): seller rating on storefront + quote-compare + public reviews list`.

---

### Task 4: Router wiring

**Files:** `internal/httpapi/router.go`.

- [ ] **Step 1:** Build the reviews service + handler and register:
```go
	reviewsSvc := reviews.NewService(q, notifier, cfg.Logger)
	reviewsHandler := reviews.NewHandler(reviewsSvc)
	reviewsHandler.Register(f, guard...)
```
  Add the public reviews route: it's mounted inside `suppliers.NewPublicHandler(q).RegisterPublic(f)`
  (already called near the top) — since `getReviews` is added to that handler, no extra registration is
  needed IF you added the route inside `RegisterPublic`. Confirm it is.
- [ ] **Step 2:** Inside the admin block (`if cfg.AdminJWTSecret != ""`), register:
```go
	reviewsHandler.RegisterAdmin(f, adminGuard)
```
- [ ] **Step 3:** `go build ./...` clean.
- [ ] **Step 4: Commit** — `feat(reviews): router wiring (party + public + admin)`.

---

## Self-Review checklist

- `go build ./...` clean; full suite `go test ./... -p 1 -count=1` (migrations ≤021 applied) — 0 failures.
- Migration 021 round-trips (down/up).
- A review can only be created on a `completed` order by a party; direction is server-derived; one per party per order.
- `Reputation` and the storefront/quote aggregates EXCLUDE `removed` reviews.
- Public storefront `reputation` is now an object `{avg_rating, review_count}`; `/public/v1/sellers/:id/reviews` returns visible reviews only.
- Quote-compare rows carry `seller_avg_rating` + `seller_review_count`.
- No client-supplied `direction`/`reviewee` is trusted.

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
	rep, ok := out["reputation"].(map[string]any)
	require.True(t, ok, "reputation must be an object")
	require.Equal(t, 0.0, rep["avg_rating"])
	require.Equal(t, 0.0, rep["review_count"]) // JSON numbers decode as float64
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

// seedReviewedSeller creates a buyer + seller company, a supplier profile
// for the seller, an order between them, and one visible review of the
// seller, returning the seller company id.
func seedReviewedSeller(t *testing.T, q *sqlc.Queries, tag string) (sellerID string) {
	t.Helper()
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "pub-buyer-" + tag, Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "pub-seller-" + tag, Type: "seller"})
	require.NoError(t, err)
	// Use a unique, non-"widgets" category/description: this fixture asserts
	// reputation by company id and does not rely on category matching, so keep
	// it OUT of the shared "%widgets%" MatchSuppliers collision pool (that query
	// has a LIMIT with no ORDER BY, so accumulating matches flake other packages'
	// match tests against the persistent test DB).
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: seller.ID, Description: sp("reviews fixture seller"), Categories: []string{"revfix-" + tag}, Location: sp("NYC"),
	})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@pub.test", Role: "owner"})
	require.NoError(t, err)
	rfqRow, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need widgets", RawText: "widgets"})
	require.NoError(t, err)
	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqRow.ID, SellerCompanyID: seller.ID, PriceCents: 5000, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)
	order, err := q.CreateOrder(ctx, sqlc.CreateOrderParams{
		RfqID: rfqRow.ID, QuoteID: quote.ID, BuyerCompanyID: buyer.ID, SellerCompanyID: seller.ID,
		TotalCents: quote.PriceCents, Currency: quote.Currency,
	})
	require.NoError(t, err)
	_, err = q.CreateReview(ctx, sqlc.CreateReviewParams{
		OrderID: order.ID, ReviewerCompanyID: buyer.ID, RevieweeCompanyID: seller.ID,
		Direction: "buyer_on_seller", Rating: 5, Body: "excellent",
	})
	require.NoError(t, err)

	return seller.ID
}

func TestPublicSellerReputationReflectsVisibleReviews(t *testing.T) {
	q := pubQueriesOrSkip(t)
	sellerID := seedReviewedSeller(t, q, "reputation")

	app := fiber.New()
	NewPublicHandler(q).RegisterPublic(app)

	req := httptest.NewRequest("GET", "/public/v1/sellers/"+sellerID, nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	rep, ok := out["reputation"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 5.0, rep["avg_rating"])
	require.Equal(t, 1.0, rep["review_count"])
}

func TestPublicReviewsRoute(t *testing.T) {
	q := pubQueriesOrSkip(t)
	sellerID := seedReviewedSeller(t, q, "reviewslist")

	app := fiber.New()
	NewPublicHandler(q).RegisterPublic(app)

	req := httptest.NewRequest("GET", "/public/v1/sellers/"+sellerID+"/reviews", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	reviews, ok := out["reviews"].([]any)
	require.True(t, ok)
	require.Len(t, reviews, 1)
	first := reviews[0].(map[string]any)
	require.Equal(t, 5.0, first["rating"])
	require.Equal(t, "excellent", first["body"])
	require.Contains(t, first, "reviewer_name")
	require.Contains(t, first, "created_at")
}

func sp(s string) *string { return &s }

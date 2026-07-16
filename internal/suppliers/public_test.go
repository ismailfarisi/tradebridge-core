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

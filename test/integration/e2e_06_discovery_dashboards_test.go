package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #6 (Sprint 6): a buyer finds a supplier via public discovery; the buyer
// and seller dashboards + seller analytics render real aggregations after a
// full quote→accept→order flow.
func TestE2E06_DiscoveryAndDashboards(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// Seller with a discoverable profile (registerSeller sets description
	// "cartons and pallets", categories ["packaging"]). Mark it verified so it
	// ranks and shows the badge.
	sellerToken, sellerCompany, secret := registerSeller(t, api, "E2E6Pack")
	require.NoError(t, q.SyncSupplierVerified(ctx, sqlc.SyncSupplierVerifiedParams{CompanyID: sellerCompany, Verified: true}))
	// Give the profile a distinctive search term.
	st, _ := doJSON(t, api, "PUT", "/v1/suppliers/me", sellerToken, map[string]any{
		"description": "e2e6probe cartons and pallets supplier", "categories": []string{"packaging"}, "location": "Dubai",
	})
	require.Equal(t, http.StatusOK, st)

	// Public discovery (no auth) finds the seller by need.
	st, disc := doJSON(t, api, "GET", "/public/v1/discovery?q=e2e6probe", "", nil)
	require.Equal(t, http.StatusOK, st)
	results := disc["results"].([]any)
	require.NotEmpty(t, results, "discovery finds the seller by need")
	found := false
	for _, r := range results {
		if r.(map[string]any)["company_id"] == sellerCompany {
			found = true
		}
	}
	require.True(t, found, "the pilot seller appears in discovery results")

	// Facets are non-empty for the query.
	st, facets := doJSON(t, api, "GET", "/public/v1/discovery/facets?q=e2e6probe", "", nil)
	require.Equal(t, http.StatusOK, st)
	require.NotEmpty(t, facets["categories"].([]any), "facets include categories")

	// Buyer posts a matching RFQ, seller agent quotes, buyer accepts → order.
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E6 Buyer", "company_type": "buyer",
		"email": uniqueEmail("e2e6-buyer-"), "password": "password123",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	verifyUser(t, pool, buyer["user_id"].(string))

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": "e2e6-submit", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 14, "notes": "in stock"},
	})
	require.Equal(t, http.StatusCreated, st)

	st, listed := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quoteID := listed["quotes"].([]any)[0].(map[string]any)["id"].(string)

	st, accepted := doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusCreated, st)
	_ = accepted

	// Buyer dashboard reflects the real flow.
	st, bd := doJSON(t, api, "GET", "/v1/dashboard/buyer", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.GreaterOrEqual(t, bd["quotes_received"].(float64), float64(1))
	require.GreaterOrEqual(t, bd["orders_in_progress"].(float64), float64(1))

	// Seller dashboard + analytics reflect the won deal.
	st, sd := doJSON(t, api, "GET", "/v1/dashboard/seller", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.GreaterOrEqual(t, sd["active_orders"].(float64), float64(1))

	st, an := doJSON(t, api, "GET", "/v1/analytics/seller", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.GreaterOrEqual(t, an["quotes_won"].(float64), float64(1))
	require.GreaterOrEqual(t, an["revenue_won_cents"].(float64), float64(100000))
	require.InDelta(t, 1.0, an["win_rate"].(float64), 0.001)
}

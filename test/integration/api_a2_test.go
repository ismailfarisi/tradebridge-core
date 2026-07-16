package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestA2_RFQCreateMatchAndBroadcast exercises the full RFQ pipeline: a buyer
// posts a plain-text RFQ, Core extracts requirements (heuristic path — no
// ANTHROPIC_API_KEY in tests), matches a real seller, writes rfq_matches, and
// records a durable rfq.matched event for the matched seller's agent.
func TestA2_RFQCreateMatchAndBroadcast(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	// Seller company publishes a supplier profile in the "packaging" category.
	st, seller := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "PackPro", "company_type": "seller",
		"email": uniqueEmail("seller-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	sellerToken := seller["token"].(string)
	sellerCompanyID := seller["company_id"].(string)

	st, _ = doJSON(t, app, "PUT", "/v1/suppliers/me", sellerToken, map[string]any{
		"description": "cartons, boxes and pallets supplier",
		"categories":  []string{"packaging"},
		"location":    "Dubai",
	})
	require.Equal(t, http.StatusOK, st)

	// Buyer company posts an RFQ.
	st, buyer := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "BuyerCo", "company_type": "buyer",
		"email": uniqueEmail("buyer-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	verifyUser(t, pool, buyer["user_id"].(string))

	st, created := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons needed",
		"text":  "We need 500 cartons for packaging our products, deliver to Dubai by next month.",
	})
	require.Equal(t, http.StatusCreated, st)

	rfq := created["rfq"].(map[string]any)
	rfqID := rfq["id"].(string)
	require.Equal(t, "open", rfq["status"], "a matched RFQ moves draft → open")
	require.Equal(t, "packaging", rfq["category"], "heuristic should classify as packaging")

	matchCount := int(created["match_count"].(float64))
	require.GreaterOrEqual(t, matchCount, 1, "should match at least one seller")

	matches := created["matches"].([]any)
	found := false
	for _, m := range matches {
		if m.(map[string]any)["seller_company_id"] == sellerCompanyID {
			found = true
		}
	}
	require.True(t, found, "the packaging seller must be among the matches")

	// A durable rfq.matched event was written for the matched seller.
	var events int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events
		 WHERE company_id = $1 AND type = 'rfq.matched' AND payload->>'rfq_id' = $2`,
		sellerCompanyID, rfqID).Scan(&events)
	require.NoError(t, err)
	require.Equal(t, 1, events, "exactly one rfq.matched event per matched seller")

	// Buyer can read their RFQ with its matches.
	st, got := doJSON(t, app, "GET", "/v1/rfqs/"+rfqID, buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, matchCount, int(got["match_count"].(float64)))

	// Cross-tenant read is a 404 — existence must not leak.
	st, _ = doJSON(t, app, "GET", "/v1/rfqs/"+rfqID, sellerToken, nil)
	require.Equal(t, http.StatusNotFound, st, "seller must not read the buyer's RFQ")

	// State machine: open → closed succeeds; closed → closed is rejected.
	st, closed := doJSON(t, app, "POST", "/v1/rfqs/"+rfqID+"/close", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "closed", closed["status"])
	st, _ = doJSON(t, app, "POST", "/v1/rfqs/"+rfqID+"/close", buyerToken, nil)
	require.Equal(t, http.StatusConflict, st, "closed RFQ cannot be closed again")
}

// TestA2_RFQValidation rejects empty input.
func TestA2_RFQValidation(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)

	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "EmptyCo", "email": uniqueEmail("empty-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := reg["token"].(string)
	verifyUser(t, pool, reg["user_id"].(string))

	st, _ = doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{"title": "no body"})
	require.Equal(t, http.StatusBadRequest, st)
}

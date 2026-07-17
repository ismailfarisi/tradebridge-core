package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// stepsDone maps the activation steps array to key→done.
func stepsDone(t *testing.T, resp map[string]any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, s := range resp["steps"].([]any) {
		m := s.(map[string]any)
		out[m["key"].(string)] = m["done"].(bool)
	}
	return out
}

// E2E #7 (Sprint 7): a brand-new seller walks the whole activation path —
// profile → catalog → verified → agent connected → first quote — reaching 100%
// with the milestone events recorded, no manual help.
func TestE2E07_SellerActivationToFirstQuote(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// registerSeller sets the supplier profile (description + ["packaging"]).
	sellerToken, sellerCompany, secret := registerSeller(t, api, "E2E7Pack")

	// Initially: profile done (registerSeller set it), the rest todo, not 100%.
	st, act := doJSON(t, api, "GET", "/v1/activation", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "seller", act["role"])
	require.Less(t, act["percent_complete"].(float64), float64(100))
	done := stepsDone(t, act)
	require.True(t, done["profile"], "registerSeller set the profile")
	require.False(t, done["catalog"])
	require.False(t, done["first_quote"])

	// Add a catalog item.
	st, _ = doJSON(t, api, "POST", "/v1/catalog", sellerToken, map[string]any{
		"name": "Cartons 30cm", "category": "packaging", "summary": "sturdy shipping box",
	})
	require.Equal(t, http.StatusCreated, st)

	// Verify the company (seed via the query layer — bypasses the DNS handler,
	// so the `verified` STEP goes done but no `verified` EVENT is emitted).
	domain := "e2e7pack.example"
	require.NoError(t, q.SetCompanyVerified(ctx, sqlc.SetCompanyVerifiedParams{ID: sellerCompany, VerifiedDomain: &domain}))

	// Agent heartbeat (Bearer = agent secret) → agent_connected.
	st, _ = doJSON(t, gw, "POST", "/agent/v1/heartbeat", secret, nil)
	require.Equal(t, http.StatusOK, st)

	// A buyer posts a matching RFQ; the seller's agent submits a quote → first_quote.
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E7 Buyer", "company_type": "buyer",
		"email": uniqueEmail("e2e7-buyer-"), "password": "password123",
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
		"type": "quote.submit", "idempotency_key": "e2e7-submit", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 14, "notes": "in stock"},
	})
	require.Equal(t, http.StatusCreated, st)

	// Now the seller is fully activated: 100%, all five steps done.
	st, act2 := doJSON(t, api, "GET", "/v1/activation", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, float64(100), act2["percent_complete"].(float64))
	done2 := stepsDone(t, act2)
	for _, k := range []string{"profile", "catalog", "verified", "agent_connected", "first_quote"} {
		require.True(t, done2[k], "step %s should be done", k)
	}

	// The handler-emitted milestone events were recorded (verified is seeded via
	// the query layer, so its event is intentionally not asserted).
	for _, ev := range []string{"signup", "profile_set", "catalog_added", "agent_connected", "first_quote"} {
		n, err := q.CountActivationEventsFor(ctx, sqlc.CountActivationEventsForParams{CompanyID: sellerCompany, Event: ev})
		require.NoError(t, err)
		require.Equal(t, int64(1), n, "exactly one %s event", ev)
	}
}

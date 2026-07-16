package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEntitlementGates_RFQCatalogSeats(t *testing.T) {
	const email, pass = "gate-admin@test.local", "admin-supersecret"
	pool := poolOrSkip(t) // same DB buildAPIWithAdmin uses; needed for verifyUser
	app := buildAPIWithAdmin(t, email, pass)

	st, login := doJSON(t, app, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": pass})
	require.Equal(t, http.StatusOK, st)
	adminTok := login["token"].(string)

	// Lower Free to 1/1/1.
	st, _ = doJSON(t, app, "PATCH", "/admin/v1/plans/free", adminTok, map[string]any{
		"agent_autonomy": false, "catalog_max_items": 1, "rfq_monthly_limit": 1, "seats": 1,
	})
	require.Equal(t, http.StatusOK, st)
	t.Cleanup(func() {
		doJSON(t, app, "PATCH", "/admin/v1/plans/free", adminTok, map[string]any{
			"agent_autonomy": false, "catalog_max_items": 25, "rfq_monthly_limit": 10, "seats": 2,
		})
	})

	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "GateCo", "company_type": "both",
		"email": uniqueEmail("gate-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := reg["token"].(string)
	verifyUser(t, pool, reg["user_id"].(string)) // RFQ create sits behind RequireVerified

	// Catalog: 1st ok, 2nd paywalled.
	st, _ = doJSON(t, app, "POST", "/v1/catalog", token, map[string]any{"name": "A", "category": "c"})
	require.Equal(t, http.StatusCreated, st)
	st, pw := doJSON(t, app, "POST", "/v1/catalog", token, map[string]any{"name": "B", "category": "c"})
	require.Equal(t, http.StatusPaymentRequired, st)
	require.Equal(t, "upgrade_required", pw["code"])
	require.Equal(t, "catalog_max_items", pw["feature"])

	// RFQ: 1st ok, 2nd paywalled (limit 1).
	st, _ = doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{"title": "need widgets", "text": "100 blue widgets"})
	require.Equal(t, http.StatusCreated, st)
	st, pw = doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{"title": "need more", "text": "more widgets"})
	require.Equal(t, http.StatusPaymentRequired, st)
	require.Equal(t, "rfq_monthly_limit", pw["feature"])

	// Seats: owner already fills the single seat, so an invite is paywalled.
	st, pw = doJSON(t, app, "POST", "/v1/users/invite", token, map[string]any{"email": uniqueEmail("inv-"), "role": "member"})
	require.Equal(t, http.StatusPaymentRequired, st)
	require.Equal(t, "seats", pw["feature"])
}

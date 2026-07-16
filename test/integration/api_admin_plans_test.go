package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminPlans_ListAndPatch(t *testing.T) {
	const email, pass = "plan-admin@test.local", "admin-supersecret"
	app := buildAPIWithAdmin(t, email, pass)

	st, login := doJSON(t, app, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": pass})
	require.Equal(t, http.StatusOK, st)
	adminTok := login["token"].(string)

	st, list := doJSON(t, app, "GET", "/admin/v1/plans", adminTok, nil)
	require.Equal(t, http.StatusOK, st)
	require.Len(t, list["plans"].([]any), 3)

	// Lower Free's RFQ limit to 1 and confirm it round-trips.
	st, patched := doJSON(t, app, "PATCH", "/admin/v1/plans/free", adminTok, map[string]any{
		"agent_autonomy": false, "catalog_max_items": 1, "rfq_monthly_limit": 1, "seats": 1,
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, float64(1), patched["rfq_monthly_limit"])

	// A company now sees the edited limit (cache was invalidated).
	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AdminSeeCo", "company_type": "both",
		"email": uniqueEmail("asee-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	st, sum := doJSON(t, app, "GET", "/v1/billing/summary", reg["token"].(string), nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, float64(1), sum["entitlements"].(map[string]any)["rfq_monthly_limit"])

	// Restore defaults so other tests are unaffected.
	doJSON(t, app, "PATCH", "/admin/v1/plans/free", adminTok, map[string]any{
		"agent_autonomy": false, "catalog_max_items": 25, "rfq_monthly_limit": 10, "seats": 2,
	})
}

package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBillingSummary_FreeDefaults(t *testing.T) {
	app := buildAPI(t)

	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "SummaryCo", "company_type": "both",
		"email": uniqueEmail("sum-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := reg["token"].(string)

	st, out := doJSON(t, app, "GET", "/v1/billing/summary", token, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "free", out["plan"])

	ent := out["entitlements"].(map[string]any)
	require.Equal(t, false, ent["agent_autonomy"])
	require.Equal(t, float64(25), ent["catalog_max_items"])

	usage := out["usage"].(map[string]any)
	require.Equal(t, float64(0), usage["rfq_used"])
	require.Equal(t, float64(10), usage["rfq_limit"])
	require.Equal(t, float64(1), usage["seats_used"]) // the owner
}

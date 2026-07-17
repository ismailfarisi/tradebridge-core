package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// The public discovery endpoint must never leak the internal search_vector
// artifact or any private company column.
func TestPublicDiscoveryProjectionIsClean(t *testing.T) {
	api := buildAPI(t)
	pool := poolOrSkip(t)
	_ = pool

	// Register a seller with a searchable profile (registerSeller creates the
	// company + agent credential; then set a profile via the authed route).
	sellerToken, _, _ := registerSeller(t, api, "DiscoveryLeakCo")
	st, _ := doJSON(t, api, "PUT", "/v1/suppliers/me", sellerToken, map[string]any{
		"description": "leakprobe premium widgets supplier",
		"categories":  []string{"leakprobe"},
		"location":    "Berlin",
	})
	require.Equal(t, http.StatusOK, st)

	st, page := doJSON(t, api, "GET", "/public/v1/discovery?q=leakprobe", "", nil)
	require.Equal(t, http.StatusOK, st)
	results, ok := page["results"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, results)

	allowed := map[string]bool{
		"company_id": true, "name": true, "verification_status": true, "verified_domain": true,
		"description": true, "categories": true, "location": true, "verified": true,
		"avg_rating": true, "review_count": true, "score": true, "matched_catalog": true,
	}
	for _, r := range results {
		row := r.(map[string]any)
		for k := range row {
			require.True(t, allowed[k], "unexpected public discovery field leaked: %s", k)
		}
	}
}

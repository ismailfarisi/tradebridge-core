package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMe_ReportsEmailVerified(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)

	_, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "MeCo", "company_type": "buyer",
		"email": uniqueEmail("me-"), "password": "password123",
	})
	token := reg["token"].(string)
	userID := reg["user_id"].(string)

	// New user is unverified.
	status, me := doJSON(t, app, "GET", "/v1/me", token, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, false, me["email_verified"])

	// After verifying, /v1/me flips to true.
	verifyUser(t, pool, userID)
	status, me = doJSON(t, app, "GET", "/v1/me", token, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, me["email_verified"])
}

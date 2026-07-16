package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVerify_SoftGateOnRFQCreate(t *testing.T) {
	app := buildAPI(t)

	status, body := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "Gate Co", "company_type": "buyer",
		"email": "gate@x.com", "password": "password123",
	})
	require.Equal(t, http.StatusCreated, status)
	token := body["token"].(string)

	// New user is unverified → RFQ create is blocked with the verify_email code.
	status, gated := doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{
		"title": "Need 500 units", "text": "500 units of widget",
	})
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "verify_email", gated["code"])

	// Fetch the verification token from the DB (the email is logged, not returned).
	pool := poolOrSkip(t)
	var vtoken string
	err := pool.QueryRow(t.Context(),
		`SELECT ev.token FROM email_verifications ev
		 JOIN users u ON u.id = ev.user_id WHERE u.email = $1`, "gate@x.com").Scan(&vtoken)
	require.NoError(t, err)

	// Verify, then RFQ create succeeds.
	status, _ = doJSON(t, app, "POST", "/v1/auth/verify-email", "", map[string]any{"token": vtoken})
	require.Equal(t, http.StatusOK, status)

	status, _ = doJSON(t, app, "POST", "/v1/rfqs", token, map[string]any{
		"title": "Need 500 units", "text": "500 units of widget",
	})
	require.Equal(t, http.StatusCreated, status)
}

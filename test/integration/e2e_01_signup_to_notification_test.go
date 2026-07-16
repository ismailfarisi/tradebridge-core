package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #1: a new buyer signs up, is soft-gated, verifies email, posts an RFQ that
// matches a seller, and the seller receives an in-app notification. This is the
// spine of the Sprint 1 journey and the first member of the growing E2E suite.
func TestE2E01_SignupVerifyRFQNotification(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	_, _, sellerUserID := seedSellerWithCatalog(t, app, "packaging boxes")

	// Buyer signs up (unverified).
	buyerEmail := uniqueEmail("e2e-buyer-")
	status, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E Buyer", "company_type": "buyer",
		"email": buyerEmail, "password": "password123",
	})
	require.Equal(t, http.StatusCreated, status)
	buyerToken := reg["token"].(string)

	// Soft gate blocks RFQ create.
	status, gated := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Need packaging boxes", "text": "500 packaging boxes",
	})
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "verify_email", gated["code"])

	// Verify.
	var vtoken string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT token FROM email_verifications ev JOIN users u ON u.id=ev.user_id WHERE u.email=$1`,
		buyerEmail).Scan(&vtoken))
	status, _ = doJSON(t, app, "POST", "/v1/auth/verify-email", "", map[string]any{"token": vtoken})
	require.Equal(t, http.StatusOK, status)

	// RFQ create now succeeds and matches the seller.
	status, _ = doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Need packaging boxes", "text": "500 packaging boxes",
	})
	require.Equal(t, http.StatusCreated, status)

	// Seller is notified.
	require.Eventually(t, func() bool {
		c, _ := q.CountUnreadNotifications(ctx, sellerUserID)
		return c >= 1
	}, 3*time.Second, 25*time.Millisecond)
}

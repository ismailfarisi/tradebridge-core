package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// TestNotify_QuoteSubmittedNotifiesBuyer verifies that when a seller's agent
// submits a quote via the Agent Gateway, the buyer company is notified
// in-app with a quote.submitted notification.
func TestNotify_QuoteSubmittedNotifiesBuyer(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	_, _, secret := registerSeller(t, api, "QuotePack")
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "QuoteBuyer", "company_type": "buyer",
		"email": uniqueEmail("qbuyer-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	buyerUserID := buyer["user_id"].(string)
	verifyUser(t, pool, buyerUserID)

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": "q1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1200, "currency": "USD"},
	})
	require.Equal(t, http.StatusCreated, st)

	require.Eventually(t, func() bool {
		c, _ := q.CountUnreadNotifications(ctx, buyerUserID)
		return c >= 1
	}, 3*time.Second, 25*time.Millisecond)

	rows, err := q.ListNotificationsByUser(ctx, sqlc.ListNotificationsByUserParams{UserID: buyerUserID, Limit: 1})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, "quote.submitted", rows[0].Type)
}

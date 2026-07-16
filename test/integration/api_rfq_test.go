package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// TestRFQ_SetsExpiresAtOnMatch verifies a matched RFQ gets an expires_at
// roughly RFQ_TTL_DAYS (default 14 days) out from created_at. It drives the
// same heuristic chat → create-and-match path as TestChat_HeuristicAsksThenCreates
// (api_chat_test.go), then reloads the row via GetRFQ to check the new column.
func TestRFQ_SetsExpiresAtOnMatch(t *testing.T) {
	api := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)

	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "ExpiryBuyer", "company_type": "buyer",
		"email": uniqueEmail("exp-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := buyer["token"].(string)
	verifyUser(t, pool, buyer["user_id"].(string))

	// Full detail in one shot → assistant creates + matches immediately.
	st, r := doJSON(t, api, "POST", "/v1/rfq-chat", token, map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "500 cartons for packaging, deliver to Dubai next month"},
		},
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, r["done"])
	rfq := r["rfq"].(map[string]any)
	rfqID := rfq["id"].(string)
	require.NotEmpty(t, rfqID)

	got, err := q.GetRFQ(context.Background(), rfqID)
	require.NoError(t, err)
	require.True(t, got.ExpiresAt.Valid, "expires_at must be set on match")
	delta := got.ExpiresAt.Time.Sub(got.CreatedAt)
	require.InDelta(t, (14 * 24 * time.Hour).Hours(), delta.Hours(), 1.0)
}

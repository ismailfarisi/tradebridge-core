package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// TestChat_MessagesRoundTrip proves the new column persists a transcript and
// GetRFQ returns it (proving migration 009 + SetRFQMessages + regen work).
func TestChat_MessagesRoundTrip(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "MsgBuyer", Type: "buyer"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: co.ID, Email: uniqueEmail("mrt-"), Role: "owner",
	})
	require.NoError(t, err)
	rfq, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: co.ID, CreatedBy: u.ID, Title: "Cartons", RawText: "need cartons",
	})
	require.NoError(t, err)

	transcript, err := json.Marshal([]map[string]string{
		{"role": "user", "content": "I need cartons"},
		{"role": "assistant", "content": "How many?"},
	})
	require.NoError(t, err)
	require.NoError(t, q.SetRFQMessages(ctx, sqlc.SetRFQMessagesParams{Messages: transcript, ID: rfq.ID}))

	got, err := q.GetRFQ(ctx, rfq.ID)
	require.NoError(t, err)
	require.JSONEq(t, string(transcript), string(got.Messages))
}

// TestChat_HeuristicAsksThenCreates walks the no-key heuristic path end to end.
func TestChat_HeuristicAsksThenCreates(t *testing.T) {
	api := buildAPI(t)
	pool := poolOrSkip(t)

	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "ChatBuyer", "company_type": "buyer",
		"email": uniqueEmail("cb-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := buyer["token"].(string)
	verifyUser(t, pool, buyer["user_id"].(string))

	// Vague opener → assistant asks (done=false, no rfq).
	st, r1 := doJSON(t, api, "POST", "/v1/rfq-chat", token, map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hi, I want to buy something"}},
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, false, r1["done"])
	require.NotEmpty(t, r1["assistant"])
	require.Nil(t, r1["rfq"])

	// Full detail → assistant creates (done=true, rfq present, transcript persisted).
	st, r2 := doJSON(t, api, "POST", "/v1/rfq-chat", token, map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "hi, I want to buy something"},
			{"role": "assistant", "content": r1["assistant"].(string)},
			{"role": "user", "content": "500 cartons for packaging, deliver to Dubai next month"},
		},
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, r2["done"])
	rfq := r2["rfq"].(map[string]any)
	rfqID := rfq["id"].(string)
	require.NotEmpty(t, rfqID)
	msgs := rfq["messages"].([]any)
	require.GreaterOrEqual(t, len(msgs), 3, "transcript persisted incl. assistant confirmation")

	// GET returns the persisted transcript.
	st, got := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID, token, nil)
	require.Equal(t, http.StatusOK, st)
	require.NotEmpty(t, got["rfq"].(map[string]any)["messages"])
}

// TestChat_RequiresAuth proves the endpoint is behind the tenant guard.
func TestChat_RequiresAuth(t *testing.T) {
	api := buildAPI(t)
	st, _ := doJSON(t, api, "POST", "/v1/rfq-chat", "", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "x"}},
	})
	require.Equal(t, http.StatusUnauthorized, st)
}

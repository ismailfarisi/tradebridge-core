package integration

import (
	"context"
	"encoding/json"
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

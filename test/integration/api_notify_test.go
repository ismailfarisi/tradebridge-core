package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestNotify_CreateNotificationRoundTrip(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "Notify Co", Type: "buyer"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: co.ID, Email: "n1@example.com", Role: "owner"})
	require.NoError(t, err)

	n, err := q.CreateNotification(ctx, sqlc.CreateNotificationParams{
		UserID: u.ID, CompanyID: co.ID, Type: "rfq.matched",
		Title: "New RFQ matched", Body: "A buyer needs what you sell", Data: []byte(`{"rfq_id":"R1"}`),
	})
	require.NoError(t, err)
	require.Len(t, n.ID, 26)
	require.False(t, n.ReadAt.Valid)

	unread, err := q.CountUnreadNotifications(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), unread)
}

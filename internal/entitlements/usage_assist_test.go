package entitlements

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func testQueriesOrSkip(t *testing.T) *sqlc.Queries {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool)
}

// TestCheckAssistQuota_FreeCompanyHitsCapAt20 verifies a Free-plan company
// (no active subscription) can use the assist endpoints 20 times per month,
// then gets blocked with a *PaywallError naming the assist feature.
func TestCheckAssistQuota_FreeCompanyHitsCapAt20(t *testing.T) {
	q := testQueriesOrSkip(t)
	ctx := context.Background()
	svc := NewService(q, zerolog.Nop())

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "assist-free-co", Type: "buyer"})
	require.NoError(t, err)

	for i := 0; i < 20; i++ {
		require.NoError(t, svc.CheckAssistQuota(ctx, co.ID), "under cap at increment %d", i)
		svc.IncrementAssistUsage(ctx, co.ID)
	}

	err = svc.CheckAssistQuota(ctx, co.ID)
	require.Error(t, err)
	var pw *PaywallError
	require.True(t, errors.As(err, &pw))
	require.Equal(t, "assist_monthly_limit", pw.Feature)
}

// TestCheckAssistQuota_ProCompanyUnlimited verifies a company on a 'pro'
// subscription (limit -1) is never blocked, even after many increments.
func TestCheckAssistQuota_ProCompanyUnlimited(t *testing.T) {
	q := testQueriesOrSkip(t)
	ctx := context.Background()
	svc := NewService(q, zerolog.Nop())

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "assist-pro-co", Type: "buyer"})
	require.NoError(t, err)
	_, err = q.UpsertSubscription(ctx, sqlc.UpsertSubscriptionParams{
		CompanyID: co.ID, Plan: "pro", Interval: "month", Status: "active",
	})
	require.NoError(t, err)

	for i := 0; i < 25; i++ {
		require.NoError(t, svc.CheckAssistQuota(ctx, co.ID))
		svc.IncrementAssistUsage(ctx, co.ID)
	}
	require.NoError(t, svc.CheckAssistQuota(ctx, co.ID))
}

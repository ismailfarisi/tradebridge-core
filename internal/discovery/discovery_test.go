package discovery

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func queriesOrSkip(t *testing.T) (*sqlc.Queries, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool), pool
}

func sp(s string) *string { return &s }

// A verified, well-rated, on-topic supplier must outrank an unverified,
// unrated one for the same query term.
func TestDiscoverRanksVerifiedRatedHigher(t *testing.T) {
	q, _ := queriesOrSkip(t)
	ctx := context.Background()
	tag := "rankprobe" // unique token so this test's rows are isolated

	// Verified + will receive a 5-star review.
	top, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rankprobe-top-co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: top.ID, Description: sp("premium " + tag + " supplier"), Categories: []string{tag}, Location: sp("NYC"),
	})
	require.NoError(t, err)
	require.NoError(t, q.SyncSupplierVerified(ctx, sqlc.SyncSupplierVerifiedParams{CompanyID: top.ID, Verified: true}))

	// Unverified, unrated, same term.
	low, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rankprobe-low-co", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: low.ID, Description: sp("basic " + tag + " supplier"), Categories: []string{tag}, Location: sp("LA"),
	})
	require.NoError(t, err)

	svc := NewService(q)
	page, err := svc.Search(ctx, Query{Q: tag})
	require.NoError(t, err)

	var topIdx, lowIdx = -1, -1
	for i, r := range page.Results {
		if r.CompanyID == top.ID {
			topIdx = i
		}
		if r.CompanyID == low.ID {
			lowIdx = i
		}
	}
	require.GreaterOrEqual(t, topIdx, 0, "verified supplier present")
	require.GreaterOrEqual(t, lowIdx, 0, "unverified supplier present")
	require.Less(t, topIdx, lowIdx, "verified+rated ranks above unverified")
}

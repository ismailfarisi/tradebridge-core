package rfq

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func rfqQueriesOrSkip(t *testing.T) (*sqlc.Queries, *pgxpool.Pool) {
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

func TestMatchSuppliersVerifiedGate(t *testing.T) {
	q, pool := rfqQueriesOrSkip(t)
	ctx := context.Background()
	// Persistent test DB: release any stale claim from a prior invocation
	// before SetCompanyVerified below re-claims this hardcoded domain.
	_, _ = pool.Exec(ctx, `UPDATE companies SET verification_status='unverified', verified_domain=NULL, verified_at=NULL WHERE verified_domain=$1`, "gate-seller.example")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE companies SET verification_status='unverified', verified_domain=NULL, verified_at=NULL WHERE verified_domain=$1`, "gate-seller.example")
	})

	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "gate-seller", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: seller.ID, Description: strPtr("widgets and gadgets"), Categories: []string{"widgets"}, Location: strPtr("NYC"),
	})
	require.NoError(t, err)

	// Unverified seller: appears when gate is OFF, hidden when ON.
	off, err := q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{ExcludeCompany: "buyer-x", Needle: "widgets", VerifiedOnly: false, Lim: 10})
	require.NoError(t, err)
	require.True(t, containsCompany(off, seller.ID), "gate off → unverified included")

	on, err := q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{ExcludeCompany: "buyer-x", Needle: "widgets", VerifiedOnly: true, Lim: 10})
	require.NoError(t, err)
	require.False(t, containsCompany(on, seller.ID), "gate on → unverified excluded")

	// Verify the seller, then it appears with the gate ON.
	require.NoError(t, q.SetCompanyVerified(ctx, sqlc.SetCompanyVerifiedParams{ID: seller.ID, VerifiedDomain: strPtr("gate-seller.example")}))
	on2, err := q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{ExcludeCompany: "buyer-x", Needle: "widgets", VerifiedOnly: true, Lim: 10})
	require.NoError(t, err)
	require.True(t, containsCompany(on2, seller.ID), "gate on + verified → included")
}

func strPtr(s string) *string { return &s }

func containsCompany(rows []sqlc.MatchSuppliersRow, id string) bool {
	for _, r := range rows {
		if r.CompanyID == id {
			return true
		}
	}
	return false
}

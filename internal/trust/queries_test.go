package trust

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func testQueriesOrSkip(t *testing.T) (*sqlc.Queries, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool), pool
}

// makeCompany inserts a bare company and returns its id.
func makeCompany(t *testing.T, q *sqlc.Queries, name string) string {
	t.Helper()
	co, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: name, Type: "seller"})
	require.NoError(t, err)
	return co.ID
}

func TestDomainVerificationRoundTrip(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()
	// This test's DB is persistent across separate `go test` invocations (not
	// per-run ephemeral), and SetCompanyVerified below permanently claims the
	// domain via the partial unique index. Release any stale claim left by a
	// prior run before (and after) this run so re-invocations stay green.
	releaseDomain(t, pool, "acme-rt.example")
	t.Cleanup(func() { releaseDomain(t, pool, "acme-rt.example") })
	cid := makeCompany(t, q, "verif-rt-co")

	dv, err := q.UpsertDomainVerification(ctx, sqlc.UpsertDomainVerificationParams{
		CompanyID: cid, Domain: "acme-rt.example", Token: "tok1",
	})
	require.NoError(t, err)
	require.Equal(t, "pending", dv.Status)

	// Re-claim regenerates token and resets.
	dv2, err := q.UpsertDomainVerification(ctx, sqlc.UpsertDomainVerificationParams{
		CompanyID: cid, Domain: "acme-rt.example", Token: "tok2",
	})
	require.NoError(t, err)
	require.Equal(t, "tok2", dv2.Token)
	require.Equal(t, dv.ID, dv2.ID, "same (company,domain) row")

	require.NoError(t, q.MarkDomainVerificationVerified(ctx, dv2.ID))
	require.NoError(t, q.SetCompanyVerified(ctx, sqlc.SetCompanyVerifiedParams{ID: cid, VerifiedDomain: strptr("acme-rt.example")}))

	got, err := q.GetCompanyVerification(ctx, cid)
	require.NoError(t, err)
	require.Equal(t, "verified", got.VerificationStatus)
	require.Equal(t, "acme-rt.example", *got.VerifiedDomain)
}

func strptr(s string) *string { return &s }

// releaseDomain frees a previously-claimed verified domain so tests that hard-
// code a domain string stay idempotent across repeated `go test` invocations
// against the same (persistent) test database.
func releaseDomain(t *testing.T, pool *pgxpool.Pool, domain string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE companies SET verification_status='unverified', verified_domain=NULL, verified_at=NULL WHERE verified_domain=$1`,
		domain)
	require.NoError(t, err)
}

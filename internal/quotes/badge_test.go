package quotes

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestRankQuotesIncludesVerificationStatus(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	ctx := context.Background()

	buyer, _ := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "badge-buyer", Type: "buyer"})
	seller, _ := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "badge-seller", Type: "seller"})
	// rfqs.created_by is a FK to users(id); email is UNIQUE so key it off the
	// (per-run unique) company ULID to stay idempotent across test-DB reuse.
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@badge.test", Role: "owner"})
	require.NoError(t, err)
	rfqRow, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need widgets", RawText: "widgets"})
	require.NoError(t, err)
	_, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqRow.ID, SellerCompanyID: seller.ID, PriceCents: 1000, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)

	rows, err := q.RankQuotesByRFQ(ctx, rfqRow.ID)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, "unverified", rows[0].VerificationStatus, "quote rows carry the seller's verification status")
}

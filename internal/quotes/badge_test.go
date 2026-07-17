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

func TestRankQuotesIncludesSellerRating(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rating-buyer", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rating-seller", Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@rating.test", Role: "owner"})
	require.NoError(t, err)

	// A prior completed order between this buyer and seller, reviewed
	// buyer_on_seller with rating 4 — establishes the seller's reputation.
	priorRFQ, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "prior widgets", RawText: "widgets"})
	require.NoError(t, err)
	priorQuote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: priorRFQ.ID, SellerCompanyID: seller.ID, PriceCents: 1000, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)
	priorOrder, err := q.CreateOrder(ctx, sqlc.CreateOrderParams{
		RfqID: priorRFQ.ID, QuoteID: priorQuote.ID, BuyerCompanyID: buyer.ID, SellerCompanyID: seller.ID,
		TotalCents: priorQuote.PriceCents, Currency: priorQuote.Currency,
	})
	require.NoError(t, err)
	_, err = q.CreateReview(ctx, sqlc.CreateReviewParams{
		OrderID: priorOrder.ID, ReviewerCompanyID: buyer.ID, RevieweeCompanyID: seller.ID,
		Direction: "buyer_on_seller", Rating: 4, Body: "solid",
	})
	require.NoError(t, err)

	// A fresh RFQ the seller now quotes on — its ranked row should carry the
	// seller's rating computed from the prior order's review.
	rfqRow, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need widgets", RawText: "widgets"})
	require.NoError(t, err)
	_, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqRow.ID, SellerCompanyID: seller.ID, PriceCents: 1000, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)

	rows, err := q.RankQuotesByRFQ(ctx, rfqRow.ID)
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	require.Equal(t, 4.0, rows[0].SellerAvgRating)
	require.Equal(t, int64(1), rows[0].SellerReviewCount)
}

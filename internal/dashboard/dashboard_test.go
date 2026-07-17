package dashboard

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func queriesOrSkip(t *testing.T) *sqlc.Queries {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool)
}

func TestBuyerDashboardAggregates(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "bd-buyer-co", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "bd-seller-co", Type: "seller"})
	require.NoError(t, err)
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "bd-" + buyer.ID + "@example.com", Role: "owner",
	})
	require.NoError(t, err)

	// One open RFQ with a quote (unseen => new), one quoted RFQ.
	rfq1, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: user.ID, Title: "Open RFQ", RawText: "need boxes",
	})
	require.NoError(t, err)
	_, err = q.SetRFQStatus(ctx, sqlc.SetRFQStatusParams{ID: rfq1.ID, Status: "open"})
	require.NoError(t, err)
	_, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfq1.ID, SellerCompanyID: seller.ID, PriceCents: 1000, Currency: "USD",
	})
	require.NoError(t, err)

	svc := NewService(q)
	d, err := svc.BuyerDashboard(ctx, buyer.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, d.ActiveRFQs, int64(1))
	require.GreaterOrEqual(t, d.QuotesReceived, int64(1))
	require.GreaterOrEqual(t, d.NewQuotes, int64(1), "unseen quote counts as new")
	require.NotEmpty(t, d.RecentRFQs)

	// After viewing the RFQ, its quotes are no longer "new".
	require.NoError(t, q.UpsertRFQView(ctx, sqlc.UpsertRFQViewParams{CompanyID: buyer.ID, RfqID: rfq1.ID}))
	d2, err := svc.BuyerDashboard(ctx, buyer.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), d2.NewQuotes, "viewing clears the new-quote badge")
}

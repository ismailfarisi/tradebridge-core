package quotes

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/invoicing"
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

// seedQuote creates a buyer company, seller company, RFQ, and a submitted
// quote on it, returning the quote plus the buyer company id.
func seedQuote(t *testing.T, q *sqlc.Queries, tag string) (quote sqlc.Quote, buyerID string) {
	t.Helper()
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "acc-buyer-" + tag, Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "acc-seller-" + tag, Type: "seller"})
	require.NoError(t, err)

	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@accept.test", Role: "owner",
	})
	require.NoError(t, err)

	rfqRow, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need widgets", RawText: "widgets",
	})
	require.NoError(t, err)

	quote, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqRow.ID, SellerCompanyID: seller.ID, PriceCents: 7500, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)

	return quote, buyer.ID
}

func TestAcceptCreatesInvoiceAtomically(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	quote, buyerID := seedQuote(t, q, "invoice")

	pub := eventbus.New(q, nil, zerolog.Nop())
	invSvc := invoicing.NewService(zerolog.Nop())
	svc := NewService(q, pool, pub, nil, zerolog.Nop(), nil, invSvc)

	order, err := svc.Accept(ctx, buyerID, quote.ID)
	require.NoError(t, err)

	inv, err := q.GetInvoiceByOrder(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, quote.PriceCents, inv.TotalCents)

	lines, err := q.ListInvoiceLineItems(ctx, inv.ID)
	require.NoError(t, err)
	require.Len(t, lines, 1)
}

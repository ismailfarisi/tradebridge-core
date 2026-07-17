package invoicing

import (
	"context"
	"os"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
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

// seedOrder creates a buyer company, seller company, RFQ, quote, and an order
// linking them, returning the order plus the RFQ that produced it.
func seedOrder(t *testing.T, q *sqlc.Queries, tag string) (order sqlc.Order, r sqlc.Rfq) {
	t.Helper()
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "inv-buyer-" + tag, Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "inv-seller-" + tag, Type: "seller"})
	require.NoError(t, err)

	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@invoicing.test", Role: "owner",
	})
	require.NoError(t, err)

	rfqRow, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need widgets", RawText: "widgets",
	})
	require.NoError(t, err)

	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqRow.ID, SellerCompanyID: seller.ID, PriceCents: 5000, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)

	order, err = q.CreateOrder(ctx, sqlc.CreateOrderParams{
		RfqID:           rfqRow.ID,
		QuoteID:         quote.ID,
		BuyerCompanyID:  buyer.ID,
		SellerCompanyID: seller.ID,
		TotalCents:      quote.PriceCents,
		Currency:        quote.Currency,
	})
	require.NoError(t, err)

	return order, rfqRow
}

var invoiceNumberRE = regexp.MustCompile(`^INV-\d{4}-\d{6}$`)

func TestCreateForOrder(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	ctx := context.Background()

	order, r := seedOrder(t, q, "create")

	svc := NewService(zerolog.Nop())
	inv, err := svc.CreateForOrder(ctx, q, order, r)
	require.NoError(t, err)
	require.Equal(t, order.TotalCents, inv.TotalCents)
	require.Regexp(t, invoiceNumberRE, inv.InvoiceNumber)

	lines, err := q.ListInvoiceLineItems(ctx, inv.ID)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	require.Equal(t, order.TotalCents, lines[0].AmountCents)
}

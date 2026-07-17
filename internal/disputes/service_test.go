package disputes

import (
	"context"
	"os"
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
// linking them (status "created"), returning the order plus each company id.
// Deliberately does NOT create any supplier_profiles rows — the RFQ match
// pool is a shared '%widgets%' fixture across test files and dispute tests
// don't need it.
func seedOrder(t *testing.T, q *sqlc.Queries, tag string) (order sqlc.Order, buyerID, sellerID string) {
	t.Helper()
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "dsp-buyer-" + tag, Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "dsp-seller-" + tag, Type: "seller"})
	require.NoError(t, err)

	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@disputes.test", Role: "owner",
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

	return order, buyer.ID, seller.ID
}

func TestRaiseHappyPath(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedOrder(t, q, "happy")

	d, err := svc.Raise(ctx, order.ID, buyerID, "not_delivered", "never showed up")
	require.NoError(t, err)
	require.Equal(t, "open", d.Status)
	require.Equal(t, "not_delivered", d.Reason)
	require.Equal(t, order.ID, d.OrderID)

	got, err := q.GetOrder(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, "disputed", got.Status)

	events, err := q.ListOrderEvents(ctx, order.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "disputed", events[0].ToStatus)
}

func TestRaiseNonParty(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, _, _ := seedOrder(t, q, "nonparty")
	stranger, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "dsp-stranger-nonparty", Type: "buyer"})
	require.NoError(t, err)

	_, err = svc.Raise(ctx, order.ID, stranger.ID, "not_delivered", "")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRaiseInvalidReason(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedOrder(t, q, "badreason")

	_, err := svc.Raise(ctx, order.ID, buyerID, "nonsense", "")
	require.Error(t, err)
	var validation ValidationError
	require.ErrorAs(t, err, &validation)
}

func TestRaiseNotDisputableState(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedOrder(t, q, "notdisputable")

	steps := []string{"confirmed", "fulfilled", "completed"}
	from := order.Status
	for _, to := range steps {
		updated, err := q.SetOrderStatus(ctx, sqlc.SetOrderStatusParams{ID: order.ID, Status: to, FromStatus: from})
		require.NoError(t, err)
		order = updated
		from = to
	}

	_, err := svc.Raise(ctx, order.ID, buyerID, "not_delivered", "")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestRaiseDuplicateOpen(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedOrder(t, q, "dup")

	_, err := svc.Raise(ctx, order.ID, buyerID, "not_delivered", "first")
	require.NoError(t, err)

	// Second raise on the same (now-disputed) order — the seller tries too.
	_, err = svc.Raise(ctx, order.ID, sellerID, "payment", "second")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestListForOrderPartyOnly(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedOrder(t, q, "listorder")

	_, err := svc.Raise(ctx, order.ID, buyerID, "not_delivered", "")
	require.NoError(t, err)

	list, err := svc.ListForOrder(ctx, order.ID, buyerID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	list, err = svc.ListForOrder(ctx, order.ID, sellerID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	stranger, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "dsp-stranger-listorder", Type: "buyer"})
	require.NoError(t, err)
	_, err = svc.ListForOrder(ctx, order.ID, stranger.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

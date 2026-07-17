package reviews

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
func seedOrder(t *testing.T, q *sqlc.Queries, tag string) (order sqlc.Order, buyerID, sellerID string) {
	t.Helper()
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rev-buyer-" + tag, Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rev-seller-" + tag, Type: "seller"})
	require.NoError(t, err)

	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@reviews.test", Role: "owner",
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

// seedCompletedOrder is seedOrder followed by walking the order through the
// lifecycle machine (created -> confirmed -> fulfilled -> completed) via
// SetOrderStatus, so review Create's "order must be completed" rule can be
// exercised against a real row.
func seedCompletedOrder(t *testing.T, q *sqlc.Queries, tag string) (order sqlc.Order, buyerID, sellerID string) {
	t.Helper()
	ctx := context.Background()

	order, buyerID, sellerID = seedOrder(t, q, tag)

	steps := []string{"confirmed", "fulfilled", "completed"}
	from := order.Status
	for _, to := range steps {
		updated, err := q.SetOrderStatus(ctx, sqlc.SetOrderStatusParams{ID: order.ID, Status: to, FromStatus: from})
		require.NoError(t, err)
		order = updated
		from = to
	}
	return order, buyerID, sellerID
}

func TestReviewOnlyAfterCompleted(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedOrder(t, q, "notcompleted")

	_, err := svc.Create(ctx, order.ID, buyerID, 5, "great")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestReviewHappyBuyerOnSeller(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedCompletedOrder(t, q, "happybuyer")

	rev, err := svc.Create(ctx, order.ID, buyerID, 5, "great")
	require.NoError(t, err)
	require.Equal(t, "buyer_on_seller", rev.Direction)
	require.Equal(t, sellerID, rev.RevieweeCompanyID)
	require.Equal(t, int32(5), rev.Rating)

	avg, count, err := svc.Reputation(ctx, sellerID)
	require.NoError(t, err)
	require.Equal(t, 5.0, avg)
	require.Equal(t, int64(1), count)
}

func TestReviewSellerOnBuyer(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedCompletedOrder(t, q, "sellerbuyer")

	rev, err := svc.Create(ctx, order.ID, sellerID, 4, "")
	require.NoError(t, err)
	require.Equal(t, "seller_on_buyer", rev.Direction)
	require.Equal(t, buyerID, rev.RevieweeCompanyID)
}

func TestReviewDuplicateRejected(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedCompletedOrder(t, q, "dup")

	_, err := svc.Create(ctx, order.ID, buyerID, 5, "great")
	require.NoError(t, err)

	_, err = svc.Create(ctx, order.ID, buyerID, 3, "again")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestReviewNonParty(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	ctx := context.Background()

	order, _, _ := seedCompletedOrder(t, q, "nonparty")
	stranger, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rev-stranger-nonparty", Type: "buyer"})
	require.NoError(t, err)

	_, err = svc.Create(ctx, order.ID, stranger.ID, 5, "")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestReviewRatingBounds(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedCompletedOrder(t, q, "bounds")

	_, err := svc.Create(ctx, order.ID, buyerID, 0, "")
	require.Error(t, err)
	var validation ValidationError
	require.ErrorAs(t, err, &validation)

	_, err = svc.Create(ctx, order.ID, buyerID, 6, "")
	require.Error(t, err)
	require.ErrorAs(t, err, &validation)
}

func TestRemoveDropsFromReputation(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedCompletedOrder(t, q, "remove")

	rev, err := svc.Create(ctx, order.ID, buyerID, 5, "great")
	require.NoError(t, err)

	_, count, err := svc.Reputation(ctx, sellerID)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	_, err = svc.Remove(ctx, rev.ID)
	require.NoError(t, err)

	_, count, err = svc.Reputation(ctx, sellerID)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
}

func TestReviewListForOrderPartyOnly(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedCompletedOrder(t, q, "listorder")

	_, err := svc.Create(ctx, order.ID, buyerID, 5, "great")
	require.NoError(t, err)

	list, err := svc.ListForOrder(ctx, order.ID, buyerID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	list, err = svc.ListForOrder(ctx, order.ID, sellerID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	stranger, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "rev-stranger-listorder", Type: "buyer"})
	require.NoError(t, err)
	_, err = svc.ListForOrder(ctx, order.ID, stranger.ID)
	require.ErrorIs(t, err, ErrNotFound)
}

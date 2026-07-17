package orders

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
// linking them, returning the order plus each company id.
func seedOrder(t *testing.T, q *sqlc.Queries, tag string) (order sqlc.Order, buyerID, sellerID string) {
	t.Helper()
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "ord-buyer-" + tag, Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "ord-seller-" + tag, Type: "seller"})
	require.NoError(t, err)

	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@orders.test", Role: "owner",
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

func TestTransitionHappyPath(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedOrder(t, q, "happy")

	// Seller confirms created -> confirmed.
	got, err := svc.Transition(ctx, order.ID, sellerID, "confirmed", "confirming")
	require.NoError(t, err)
	require.Equal(t, "confirmed", got.Status)

	events, err := q.ListOrderEvents(ctx, order.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "created", events[0].FromStatus)
	require.Equal(t, "confirmed", events[0].ToStatus)
	require.Equal(t, "seller", events[0].Actor)

	// Buyer cannot complete before fulfilled.
	_, err = svc.Transition(ctx, order.ID, buyerID, "completed", "")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)

	// Seller fulfills.
	got, err = svc.Transition(ctx, order.ID, sellerID, "fulfilled", "shipped")
	require.NoError(t, err)
	require.Equal(t, "fulfilled", got.Status)

	// Buyer completes.
	got, err = svc.Transition(ctx, order.ID, buyerID, "completed", "received")
	require.NoError(t, err)
	require.Equal(t, "completed", got.Status)

	events, err = q.ListOrderEvents(ctx, order.ID)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, "buyer", events[2].Actor)
}

func TestTransitionWrongRole(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedOrder(t, q, "wrongrole")

	_, err := svc.Transition(ctx, order.ID, buyerID, "confirmed", "")
	require.Error(t, err)
	var forbidden ForbiddenError
	require.ErrorAs(t, err, &forbidden)
}

func TestTransitionIllegal(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedOrder(t, q, "illegal")

	_, err := svc.Transition(ctx, order.ID, buyerID, "completed", "")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestTransitionRejectsDisputedTarget(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, _ := seedOrder(t, q, "disputedtarget")

	// disputed is a legal machine edge from created, but NOT a party-driven
	// target — it must go through the dispute endpoint (Plan 4), so the generic
	// transition endpoint rejects it as a conflict for a party.
	_, err := svc.Transition(ctx, order.ID, buyerID, "disputed", "")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)
}

func TestTransitionNonParty(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, _, _ := seedOrder(t, q, "nonparty")
	stranger, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "ord-stranger-nonparty", Type: "buyer"})
	require.NoError(t, err)

	_, err = svc.Transition(ctx, order.ID, stranger.ID, "confirmed", "")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestListOrdersByCompany(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedOrder(t, q, "list")

	buyerOrders, err := svc.List(ctx, buyerID)
	require.NoError(t, err)
	require.True(t, containsOrder(buyerOrders, order.ID))

	sellerOrders, err := svc.List(ctx, sellerID)
	require.NoError(t, err)
	require.True(t, containsOrder(sellerOrders, order.ID))
}

func TestCancelBeforeFulfilledOnly(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	ctx := context.Background()

	order, buyerID, sellerID := seedOrder(t, q, "cancel")

	_, err := svc.Transition(ctx, order.ID, sellerID, "confirmed", "")
	require.NoError(t, err)
	_, err = svc.Transition(ctx, order.ID, sellerID, "fulfilled", "")
	require.NoError(t, err)

	// fulfilled -> cancelled is not a legal edge.
	_, err = svc.Transition(ctx, order.ID, buyerID, "cancelled", "")
	require.Error(t, err)
	var conflict ConflictError
	require.ErrorAs(t, err, &conflict)
}

func containsOrder(orders []sqlc.Order, id string) bool {
	for _, o := range orders {
		if o.ID == id {
			return true
		}
	}
	return false
}

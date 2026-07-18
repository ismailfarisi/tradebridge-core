package analytics

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func queriesOrSkip(t *testing.T) (*sqlc.Queries, *pgxpool.Pool) {
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

// TestBusinessAggregatesAndMRRHonesty seeds one order, one activation event,
// and one subscription, then verifies Business() reports GMV/funnel data and
// — critically — refuses to compute MRR while the subscription's plan has no
// price configured, only doing so once a price is set.
func TestBusinessAggregatesAndMRRHonesty(t *testing.T) {
	q, pool := queriesOrSkip(t)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "an-buyer-co", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "an-seller-co", Type: "seller"})
	require.NoError(t, err)
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "an-" + buyer.ID + "@example.com", Role: "owner",
	})
	require.NoError(t, err)

	// Seed a non-cancelled order feeding GMV.
	rfq, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: user.ID, Title: "Analytics RFQ", RawText: "need widgets",
	})
	require.NoError(t, err)
	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfq.ID, SellerCompanyID: seller.ID, PriceCents: 7500, Currency: "USD",
	})
	require.NoError(t, err)
	const orderTotalCents = int64(7500)
	_, err = q.CreateOrder(ctx, sqlc.CreateOrderParams{
		RfqID: rfq.ID, QuoteID: quote.ID, BuyerCompanyID: buyer.ID, SellerCompanyID: seller.ID,
		TotalCents: orderTotalCents, Currency: "USD",
	})
	require.NoError(t, err)

	// Seed an activation event so the funnel is non-empty.
	const funnelEvent = "an_test_milestone"
	require.NoError(t, q.InsertActivationEvent(ctx, sqlc.InsertActivationEventParams{
		CompanyID: buyer.ID, Event: funnelEvent, Props: []byte(`{}`),
	}))

	// Seed an active subscription on the "free" plan (price initially unset by
	// migration 030's default NULL — other tests/migrations never set it).
	_, err = q.UpsertSubscription(ctx, sqlc.UpsertSubscriptionParams{
		CompanyID: buyer.ID, Plan: "free", Interval: "month", Status: "active",
	})
	require.NoError(t, err)

	svc := NewService(q)

	// Ensure our own subscription's plan (free) has no price set before
	// asserting MRR nullness. Business() aggregates across ALL companies'
	// subscriptions, and this DB is shared across the test suite, so this
	// alone is sufficient to force prices_set=false regardless of what other
	// packages' tests have left behind on 'pro'/'business'.
	_, err = pool.Exec(ctx, `UPDATE plans SET price_cents_month = NULL, price_cents_year = NULL WHERE code = 'free'`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE plans SET price_cents_month = NULL, price_cents_year = NULL WHERE code IN ('free','pro','business')`)
	})

	out, err := svc.Business(ctx)
	require.NoError(t, err)

	require.GreaterOrEqual(t, out.GMVCents, orderTotalCents)
	require.GreaterOrEqual(t, out.Orders, int64(1))

	found := false
	for _, f := range out.Funnel {
		if f.Event == funnelEvent {
			found = true
			require.GreaterOrEqual(t, f.Companies, int64(1))
		}
	}
	require.True(t, found, "seeded activation event must appear in the funnel")

	require.False(t, out.PricesSet, "no plan has a price yet => prices_set is false")
	require.Nil(t, out.MRRCents, "MRR must be nil, never fabricated, while any active plan lacks a price")

	// Now price EVERY plan code so that no active subscription anywhere in
	// this shared DB (including ones left by other packages' tests) can be
	// missing a price, then confirm MRR computes.
	_, err = pool.Exec(ctx, `UPDATE plans SET price_cents_month = 1000 WHERE code IN ('free','pro','business')`)
	require.NoError(t, err)

	out2, err := svc.Business(ctx)
	require.NoError(t, err)
	require.True(t, out2.PricesSet, "all active plans now have a price")
	require.NotNil(t, out2.MRRCents, "MRR should compute once every active plan has a price")
	require.GreaterOrEqual(t, *out2.MRRCents, int64(1000))
}

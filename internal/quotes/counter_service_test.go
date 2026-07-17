package quotes

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/invoicing"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// seedCounterFixture creates a buyer, seller, RFQ, and a submitted quote,
// returning enough to exercise Service.Counter.
func seedCounterFixture(t *testing.T, q *sqlc.Queries, tag string) (quote sqlc.Quote, buyerID, sellerID string) {
	t.Helper()
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "ctr-buyer-" + tag, Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "ctr-seller-" + tag, Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@counter-svc.test", Role: "owner",
	})
	require.NoError(t, err)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need cartons", RawText: "cartons",
	})
	require.NoError(t, err)
	quote, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: r.ID, SellerCompanyID: seller.ID, PriceCents: 5000, Currency: "USD", Completeness: 1,
	})
	require.NoError(t, err)
	return quote, buyer.ID, seller.ID
}

// TestCounter_NoAutonomy_RecordsMessageOnly verifies that when the seller has
// no plan (defaults to Free, AgentAutonomy=false), a buyer's counter records
// a thread message but emits NO message.received agent event.
func TestCounter_NoAutonomy_RecordsMessageOnly(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	quote, buyerID, sellerID := seedCounterFixture(t, q, "noauto")

	pub := eventbus.New(q, nil, zerolog.Nop())
	entSvc := entitlements.NewService(q, zerolog.Nop())
	svc := NewService(q, pool, pub, nil, zerolog.Nop(), nil, invoicing.NewService(zerolog.Nop()), entSvc, nil)

	price := int64(4500)
	days := int32(5)
	msg, err := svc.Counter(ctx, buyerID, quote.ID, &price, &days, "can you do better?")
	require.NoError(t, err)
	require.Equal(t, "buyer", msg.Author)
	require.Contains(t, msg.Body, "can you do better?")
	require.Contains(t, msg.Body, "price: $45.00")
	require.Contains(t, msg.Body, "delivery: 5 days")

	msgs, err := q.ListQuoteMessages(ctx, quote.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	var evCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='message.received'`,
		sellerID).Scan(&evCount))
	require.Equal(t, 0, evCount, "no agent_events row when the seller lacks agent autonomy")
}

// TestCounter_WithAutonomy_EmitsMessageReceived verifies that when the seller
// is on a plan with AgentAutonomy (seeded via a 'pro' subscription), a
// buyer's counter both records the thread message and emits a durable
// message.received agent event carrying the proposed terms.
func TestCounter_WithAutonomy_EmitsMessageReceived(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	quote, buyerID, sellerID := seedCounterFixture(t, q, "auto")

	_, err := q.UpsertSubscription(ctx, sqlc.UpsertSubscriptionParams{
		CompanyID: sellerID, Plan: "pro", Interval: "month", Status: "active",
	})
	require.NoError(t, err)

	pub := eventbus.New(q, nil, zerolog.Nop())
	entSvc := entitlements.NewService(q, zerolog.Nop())
	require.True(t, entSvc.For(ctx, sellerID).AgentAutonomy, "sanity: seller now has agent autonomy")
	svc := NewService(q, pool, pub, nil, zerolog.Nop(), nil, invoicing.NewService(zerolog.Nop()), entSvc, nil)

	price := int64(4500)
	_, err = svc.Counter(ctx, buyerID, quote.ID, &price, nil, "revised offer")
	require.NoError(t, err)

	var evCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='message.received' AND payload->>'quote_id'=$2`,
		sellerID, quote.ID).Scan(&evCount))
	require.Equal(t, 1, evCount, "seller with agent autonomy receives a durable message.received event")

	var payload []byte
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT payload FROM agent_events WHERE company_id=$1 AND type='message.received' AND payload->>'quote_id'=$2`,
		sellerID, quote.ID).Scan(&payload))
	require.Contains(t, string(payload), `"price_cents": 4500`)
	require.Contains(t, string(payload), quote.RfqID)
}

// TestCounter_NonBuyerReturnsNotFound verifies a company that isn't the
// buyer on the RFQ (e.g. the seller itself, or a third party) cannot
// counter, and gets ErrNotFound rather than a leaked 403/other signal.
func TestCounter_NonBuyerReturnsNotFound(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	quote, _, sellerID := seedCounterFixture(t, q, "nonbuyer")

	pub := eventbus.New(q, nil, zerolog.Nop())
	entSvc := entitlements.NewService(q, zerolog.Nop())
	svc := NewService(q, pool, pub, nil, zerolog.Nop(), nil, invoicing.NewService(zerolog.Nop()), entSvc, nil)

	price := int64(4500)
	_, err := svc.Counter(ctx, sellerID, quote.ID, &price, nil, "sneaky")
	require.ErrorIs(t, err, ErrNotFound)

	other, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "ctr-third-party", Type: "buyer"})
	require.NoError(t, err)
	_, err = svc.Counter(ctx, other.ID, quote.ID, &price, nil, "also sneaky")
	require.ErrorIs(t, err, ErrNotFound)
}

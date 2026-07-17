package dashboard

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/entitlements"
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

func newSvc(t *testing.T, q *sqlc.Queries) *Service {
	t.Helper()
	return NewService(q, entitlements.NewService(q, zerolog.Nop()))
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

	svc := newSvc(t, q)
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

func TestSellerDashboardCountsAndAgent(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()

	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sd-seller-co", Type: "seller"})
	require.NoError(t, err)
	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sd-buyer-co", Type: "buyer"})
	require.NoError(t, err)
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "sd-" + buyer.ID + "@ex.com", Role: "owner"})
	require.NoError(t, err)

	// An open RFQ matched to the seller, not yet quoted => incoming + review_queue.
	rfq, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: user.ID, Title: "SD RFQ", RawText: "need pallets"})
	require.NoError(t, err)
	_, err = q.SetRFQStatus(ctx, sqlc.SetRFQStatusParams{ID: rfq.ID, Status: "open"})
	require.NoError(t, err)
	_, err = q.CreateRFQMatch(ctx, sqlc.CreateRFQMatchParams{RfqID: rfq.ID, SellerCompanyID: seller.ID})
	require.NoError(t, err)

	d, err := newSvc(t, q).SellerDashboard(ctx, seller.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, d.IncomingRFQs, int64(1))
	require.GreaterOrEqual(t, d.ReviewQueue, int64(1), "unquoted matched RFQ is in the review queue")
	require.False(t, d.Agent.Online, "no heartbeat => offline")
	require.NotEmpty(t, d.PlanUsage.Plan)
}

func TestSellerAnalyticsWinRateAndRevenue(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()

	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sa-seller-co", Type: "seller"})
	require.NoError(t, err)
	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sa-buyer-co", Type: "buyer"})
	require.NoError(t, err)
	user, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "sa-" + buyer.ID + "@ex.com", Role: "owner"})
	require.NoError(t, err)
	rfq, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: user.ID, Title: "SA", RawText: "need X"})
	require.NoError(t, err)
	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{RfqID: rfq.ID, SellerCompanyID: seller.ID, PriceCents: 5000, Currency: "USD"})
	require.NoError(t, err)
	// Mark the quote accepted + create the order (a won deal).
	_, err = q.AcceptQuote(ctx, quote.ID) // AcceptQuote(ctx, id string), not a params struct
	require.NoError(t, err)
	_, err = q.CreateOrder(ctx, sqlc.CreateOrderParams{
		RfqID: rfq.ID, QuoteID: quote.ID, BuyerCompanyID: buyer.ID, SellerCompanyID: seller.ID,
		TotalCents: 5000, Currency: "USD",
	})
	require.NoError(t, err)

	a, err := newSvc(t, q).SellerAnalytics(ctx, seller.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), a.QuotesWon)
	require.InDelta(t, 1.0, a.WinRate, 0.001, "1 accepted / 1 decided = 100%")
	require.GreaterOrEqual(t, a.RevenueWonCents, int64(5000))
	require.NotEmpty(t, a.TopBuyers)
	require.Equal(t, buyer.ID, a.TopBuyers[0].CompanyID)
}

// A seller whose agent credential is active and heartbeated within the freshness
// window reports Online=true with a populated LastSeen. This guards the online
// branch that depends on the interface{}->time.Time assertion in SellerDashboard.
func TestSellerDashboardAgentOnline(t *testing.T) {
	q := queriesOrSkip(t)
	ctx := context.Background()

	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "sd-online-co", Type: "seller"})
	require.NoError(t, err)
	cred, err := q.CreateAgentCredential(ctx, sqlc.CreateAgentCredentialParams{
		CompanyID: seller.ID, Label: "gw", SecretHash: "x", ExpiresAt: pgtype.Timestamptz{},
	})
	require.NoError(t, err)
	// Fresh heartbeat (NOW()) -> within the 5-minute window.
	require.NoError(t, q.TouchAgentCredentialLastSeen(ctx, cred.ID))

	d, err := newSvc(t, q).SellerDashboard(ctx, seller.ID)
	require.NoError(t, err)
	require.True(t, d.Agent.Online, "fresh heartbeat on an active credential => online")
	require.NotNil(t, d.Agent.LastSeen, "online agent has a last_seen timestamp")
}

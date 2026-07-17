package thread

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

func testQueriesOrSkip(t *testing.T) *sqlc.Queries {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool)
}

// seedThreadQuote creates a buyer company, seller company, RFQ, and a
// submitted quote on it, returning the quote plus the buyer company id.
func seedThreadQuote(t *testing.T, q *sqlc.Queries, tag string) (quote sqlc.Quote, buyerID string) {
	t.Helper()
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "thread-buyer-" + tag, Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "thread-seller-" + tag, Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@thread.test", Role: "owner",
	})
	require.NoError(t, err)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need thread widgets", RawText: "widgets",
	})
	require.NoError(t, err)

	quote, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: r.ID, SellerCompanyID: seller.ID, PriceCents: 5000, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)
	return quote, buyer.ID
}

// TestAuthorizeThreadParty covers the 404/party split directly, without
// driving an actual SSE connection (a live stream blocks on its keepalive
// ticker, which would hang a test — see stream.go's doc comment on why this
// is extracted).
func TestAuthorizeThreadParty(t *testing.T) {
	q := testQueriesOrSkip(t)

	t.Run("seller is a party", func(t *testing.T) {
		quote, _ := seedThreadQuote(t, q, "seller-ok")
		require.NoError(t, authorizeThreadParty(context.Background(), q, quote.ID, quote.SellerCompanyID))
	})

	t.Run("buyer is a party", func(t *testing.T) {
		quote, buyerID := seedThreadQuote(t, q, "buyer-ok")
		require.NoError(t, authorizeThreadParty(context.Background(), q, quote.ID, buyerID))
	})

	t.Run("stranger gets 404, not 403 (no existence leak)", func(t *testing.T) {
		quote, _ := seedThreadQuote(t, q, "stranger")
		stranger, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: "thread-stranger", Type: "buyer"})
		require.NoError(t, err)

		err = authorizeThreadParty(context.Background(), q, quote.ID, stranger.ID)
		require.Error(t, err)
		fe, ok := err.(*fiber.Error)
		require.True(t, ok, "expected a *fiber.Error, got %T", err)
		require.Equal(t, fiber.StatusNotFound, fe.Code)
	})

	t.Run("unknown quote gets 404", func(t *testing.T) {
		err := authorizeThreadParty(context.Background(), q, "does-not-exist", "someone")
		require.Error(t, err)
		fe, ok := err.(*fiber.Error)
		require.True(t, ok, "expected a *fiber.Error, got %T", err)
		require.Equal(t, fiber.StatusNotFound, fe.Code)
	})
}

// TestStreamHandler_NonPartyGets404 drives the real HTTP handler for the
// non-party case: authorizeThreadParty fails before SetBodyStreamWriter is
// ever reached, so the response completes immediately — safe for app.Test.
func TestStreamHandler_NonPartyGets404(t *testing.T) {
	q := testQueriesOrSkip(t)
	quote, _ := seedThreadQuote(t, q, "http-404")
	stranger, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: "thread-http-stranger", Type: "buyer"})
	require.NoError(t, err)

	app := fiber.New()
	NewHandler(q, nil, zerolog.Nop()).Register(app, func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalCompanyID, stranger.ID)
		return c.Next()
	})

	req := httptest.NewRequest("GET", "/v1/quotes/"+quote.ID+"/thread/stream", http.NoBody)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

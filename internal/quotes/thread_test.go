package quotes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/internal/invoicing"
	"github.com/tradebridge/core/internal/thread"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// stubThreadGuard injects a company id into locals the way TenantGuard would.
func stubThreadGuard(companyID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalCompanyID, companyID)
		return c.Next()
	}
}

// TestThreadEndpoint_ReturnsQuoteMessagesAndVersions verifies GET
// /v1/quotes/:id/thread returns the head quote plus its full message and
// version history after a seller submit + buyer counter, for a party.
// Publishing itself is nil-safe (Redis nil here) and covered separately by
// the thread package's own tests plus the E2E environment.
func TestThreadEndpoint_ReturnsQuoteMessagesAndVersions(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "thread-rest-buyer", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "thread-rest-seller", Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@threadrest.test", Role: "owner",
	})
	require.NoError(t, err)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need thread rest widgets", RawText: "widgets",
	})
	require.NoError(t, err)

	// Version 1: the seller's initial submit.
	n1 := Normalized{PriceCents: 5000, Currency: "USD", Notes: "initial", Completeness: 1}
	head, err := Record(ctx, q, pool, r.ID, seller.ID, "", n1)
	require.NoError(t, err)

	entSvc := entitlements.NewService(q, zerolog.Nop())
	svc := NewService(q, pool, nil, nil, zerolog.Nop(), nil, invoicing.NewService(zerolog.Nop()), entSvc, thread.NewPublisher(nil, zerolog.Nop()))

	// The buyer counters — this records a thread message (no new version; a
	// version only appears when the seller's agent replies via quote.counter).
	price := int64(4700)
	_, err = svc.Counter(ctx, buyer.ID, head.ID, &price, nil, "let's meet in the middle")
	require.NoError(t, err)

	app := fiber.New()
	NewHandler(svc, q).Register(app, func(c *fiber.Ctx) error { return c.Next() }, stubThreadGuard(buyer.ID))

	req := httptest.NewRequest("GET", "/v1/quotes/"+head.ID+"/thread", http.NoBody)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var out struct {
		Quote    map[string]any   `json:"quote"`
		Messages []map[string]any `json:"messages"`
		Versions []map[string]any `json:"versions"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, head.ID, out.Quote["id"])
	require.Len(t, out.Messages, 1, "the buyer's counter message")
	require.Len(t, out.Versions, 1, "only the seller's initial submit created a version")
	require.Equal(t, float64(1), out.Versions[0]["version"])
}

// TestThreadEndpoint_NonPartyGets404 confirms the combined thread load
// doesn't leak existence to a stranger, mirroring the other quote routes.
func TestThreadEndpoint_NonPartyGets404(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	quote, _ := seedQuote(t, q, "thread-rest-404")

	entSvc := entitlements.NewService(q, zerolog.Nop())
	svc := NewService(q, pool, nil, nil, zerolog.Nop(), nil, invoicing.NewService(zerolog.Nop()), entSvc, nil)

	stranger, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: "thread-rest-stranger", Type: "buyer"})
	require.NoError(t, err)

	app := fiber.New()
	NewHandler(svc, q).Register(app, func(c *fiber.Ctx) error { return c.Next() }, stubThreadGuard(stranger.ID))

	req := httptest.NewRequest("GET", "/v1/quotes/"+quote.ID+"/thread", http.NoBody)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode)
}

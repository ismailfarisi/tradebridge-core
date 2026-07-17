package invoicing

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// stubIdentity injects company id into ctx locals the way the JWT guard would.
func stubIdentity(companyID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("company_id", companyID)
		return c.Next()
	}
}

func TestHandlerPDFForParty(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	order, r := seedOrder(t, q, "h-party")

	svc := NewService(zerolog.Nop())
	ctx := context.Background()
	_, err := svc.CreateForOrder(ctx, q, order, r)
	require.NoError(t, err)

	app := fiber.New()
	NewHandler(svc, q).Register(app, stubIdentity(order.BuyerCompanyID))

	req := httptest.NewRequest("GET", "/v1/orders/"+order.ID+"/invoice.pdf", http.NoBody)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "application/pdf", resp.Header.Get("Content-Type"))
	buf := make([]byte, 5)
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err)
	require.Equal(t, "%PDF-", string(buf))
}

func TestHandlerPDFNonPartyNotFound(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	order, r := seedOrder(t, q, "h-nonparty")

	svc := NewService(zerolog.Nop())
	ctx := context.Background()
	_, err := svc.CreateForOrder(ctx, q, order, r)
	require.NoError(t, err)

	stranger, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "inv-stranger-h-nonparty", Type: "buyer"})
	require.NoError(t, err)

	app := fiber.New()
	NewHandler(svc, q).Register(app, stubIdentity(stranger.ID))

	req := httptest.NewRequest("GET", "/v1/orders/"+order.ID+"/invoice.pdf", http.NoBody)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode)
}

package orders

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// stubIdentity injects company/role into ctx locals the way the JWT guard would.
func stubIdentity(companyID, role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("company_id", companyID)
		c.Locals("user_id", "u_"+companyID)
		c.Locals("role", role)
		return c.Next()
	}
}

func newTestApp(t *testing.T, companyID, role string) *fiber.App {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	app := fiber.New()
	NewHandler(svc, q).Register(app, stubIdentity(companyID, role))
	return app
}

func doJSON(t *testing.T, app *fiber.App, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, http.NoBody)
	} else {
		var buf bytes.Buffer
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
		req = httptest.NewRequest(method, path, &buf)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestHandlerTransitionHappyPath(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	order, _, sellerID := seedOrder(t, q, "h-happy")
	app := newTestApp(t, sellerID, "owner")

	code, out := doJSON(t, app, "POST", "/v1/orders/"+order.ID+"/transitions", map[string]string{"to": "confirmed"})
	require.Equal(t, 200, code)
	got := out["order"].(map[string]any)
	require.Equal(t, "confirmed", got["status"])
}

func TestHandlerTransitionWrongRole(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	order, buyerID, _ := seedOrder(t, q, "h-wrongrole")
	app := newTestApp(t, buyerID, "owner")

	code, _ := doJSON(t, app, "POST", "/v1/orders/"+order.ID+"/transitions", map[string]string{"to": "confirmed"})
	require.Equal(t, 403, code)
}

func TestHandlerTransitionIllegal(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	order, buyerID, _ := seedOrder(t, q, "h-illegal")
	app := newTestApp(t, buyerID, "owner")

	code, _ := doJSON(t, app, "POST", "/v1/orders/"+order.ID+"/transitions", map[string]string{"to": "completed"})
	require.Equal(t, 409, code)
}

func TestHandlerList(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	order, buyerID, _ := seedOrder(t, q, "h-list")
	app := newTestApp(t, buyerID, "owner")

	req := httptest.NewRequest("GET", "/v1/orders/", http.NoBody)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	items, ok := out["orders"].([]any)
	require.True(t, ok)
	found := false
	for _, it := range items {
		if it.(map[string]any)["id"] == order.ID {
			found = true
		}
	}
	require.True(t, found)
}

func TestHandlerGetWithTimeline(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	order, buyerID, _ := seedOrder(t, q, "h-get")
	app := newTestApp(t, buyerID, "owner")

	req := httptest.NewRequest("GET", "/v1/orders/"+order.ID, http.NoBody)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Contains(t, out, "order")
	require.Contains(t, out, "events")
}

func TestHandlerGetNotFound(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	stranger, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: "ord-stranger-h-get", Type: "buyer"})
	require.NoError(t, err)
	app := newTestApp(t, stranger.ID, "owner")

	req := httptest.NewRequest("GET", "/v1/orders/does-not-exist", http.NoBody)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode)
}

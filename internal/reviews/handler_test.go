package reviews

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
)

// stubIdentity injects the locals TenantGuard() would derive from real JWT
// claims, so it fully replaces the JWT+TenantGuard chain in these tests.
func stubIdentity(companyID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("company_id", companyID)
		c.Locals("user_id", "u_"+companyID)
		c.Locals("role", "owner")
		return c.Next()
	}
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

func TestHandlerCreateOnCompletedOrder(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	order, buyerID, _ := seedCompletedOrder(t, q, "handler-happy")

	app := fiber.New()
	NewHandler(svc).Register(app, stubIdentity(buyerID))

	code, out := doJSON(t, app, "POST", "/v1/orders/"+order.ID+"/reviews", map[string]any{
		"rating": 5, "body": "great",
	})
	require.Equal(t, 200, code, "%v", out)
	require.Contains(t, out, "review")
}

func TestHandlerCreateOnNonCompletedOrderConflicts(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	order, buyerID, _ := seedOrder(t, q, "handler-notcompleted")

	app := fiber.New()
	NewHandler(svc).Register(app, stubIdentity(buyerID))

	code, _ := doJSON(t, app, "POST", "/v1/orders/"+order.ID+"/reviews", map[string]any{
		"rating": 5, "body": "great",
	})
	require.Equal(t, 409, code)
}

func TestHandlerList(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	order, buyerID, _ := seedCompletedOrder(t, q, "handler-list")

	app := fiber.New()
	NewHandler(svc).Register(app, stubIdentity(buyerID))

	_, err := svc.Create(context.Background(), order.ID, buyerID, 5, "great")
	require.NoError(t, err)

	code, out := doJSON(t, app, "GET", "/v1/orders/"+order.ID+"/reviews", nil)
	require.Equal(t, 200, code)
	require.Contains(t, out, "reviews")
}

func TestAdminRemove(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	svc := NewService(q, nil, zerolog.Nop())
	order, buyerID, _ := seedCompletedOrder(t, q, "handler-admin-remove")

	rev, err := svc.Create(context.Background(), order.ID, buyerID, 5, "great")
	require.NoError(t, err)

	app := fiber.New()
	pass := func(c *fiber.Ctx) error { return c.Next() }
	NewAdminHandler(svc).Register(app, pass)

	code, out := doJSON(t, app, "POST", "/admin/v1/reviews/"+rev.ID+"/remove", nil)
	require.Equal(t, 200, code, "%v", out)
}

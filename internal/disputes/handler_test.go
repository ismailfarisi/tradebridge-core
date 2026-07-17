package disputes

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
	"github.com/tradebridge/core/pkg/middleware"
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

// stubAdminIdentity injects the locals PlatformAdminGuard would derive from a
// real admin JWT.
func stubAdminIdentity(adminID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalAdminID, adminID)
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

func TestHandlerRaiseHappyPath(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	order, buyerID, _ := seedOrder(t, q, "handler-happy")

	app := fiber.New()
	NewHandler(svc).Register(app, stubIdentity(buyerID))

	code, out := doJSON(t, app, "POST", "/v1/orders/"+order.ID+"/disputes", map[string]any{
		"reason": "not_delivered", "detail": "never showed",
	})
	require.Equal(t, 200, code, "%v", out)
	require.Contains(t, out, "dispute")
}

func TestHandlerRaiseOnCompletedOrderConflicts(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	order, buyerID, _ := seedOrder(t, q, "handler-completed")

	steps := []string{"confirmed", "fulfilled", "completed"}
	from := order.Status
	for _, to := range steps {
		updated, err := q.SetOrderStatus(context.Background(), sqlc.SetOrderStatusParams{ID: order.ID, Status: to, FromStatus: from})
		require.NoError(t, err)
		order = updated
		from = to
	}

	app := fiber.New()
	NewHandler(svc).Register(app, stubIdentity(buyerID))

	code, _ := doJSON(t, app, "POST", "/v1/orders/"+order.ID+"/disputes", map[string]any{
		"reason": "not_delivered",
	})
	require.Equal(t, 409, code)
}

func TestHandlerListDisputes(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	order, buyerID, _ := seedOrder(t, q, "handler-list")

	_, err := svc.Raise(context.Background(), order.ID, buyerID, "other", "")
	require.NoError(t, err)

	app := fiber.New()
	NewHandler(svc).Register(app, stubIdentity(buyerID))

	code, out := doJSON(t, app, "GET", "/v1/orders/"+order.ID+"/disputes", nil)
	require.Equal(t, 200, code)
	require.Contains(t, out, "disputes")
}

func TestAdminListAndResolve(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, nil, zerolog.Nop())
	order, buyerID, _ := seedOrder(t, q, "handler-admin")

	d, err := svc.Raise(context.Background(), order.ID, buyerID, "other", "")
	require.NoError(t, err)

	app := fiber.New()
	NewAdminHandler(svc).RegisterAdmin(app, stubAdminIdentity("admin-h"))

	code, out := doJSON(t, app, "GET", "/admin/v1/disputes?status=open", nil)
	require.Equal(t, 200, code, "%v", out)
	require.Contains(t, out, "disputes")

	code, out = doJSON(t, app, "POST", "/admin/v1/disputes/"+d.ID+"/resolve", map[string]any{
		"outcome": "resolved", "order_to": "completed", "resolution": "shipped",
	})
	require.Equal(t, 200, code, "%v", out)
	require.Contains(t, out, "dispute")
}

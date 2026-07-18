package dsr

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

	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// stubIdentity injects the locals TenantGuard() would derive from real JWT
// claims, so it fully replaces the JWT+TenantGuard chain in these tests.
func stubIdentity(companyID, userID, role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("company_id", companyID)
		c.Locals("user_id", userID)
		c.Locals("role", role)
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

func seedOwnerCompany(t *testing.T, q *sqlc.Queries, namePrefix, plainPassword string) (sqlc.Company, sqlc.User) {
	t.Helper()
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: namePrefix, Type: "seller"})
	require.NoError(t, err)
	hash, err := identity.HashPassword(plainPassword)
	require.NoError(t, err)
	user, err := q.CreateUserWithPassword(ctx, sqlc.CreateUserWithPasswordParams{
		CompanyID:    co.ID,
		Email:        namePrefix + "-" + co.ID + "@example.com",
		Role:         "owner",
		PasswordHash: &hash,
	})
	require.NoError(t, err)
	return co, user
}

func TestHandlerDeleteHappyPathWritesAuditRow(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	co, user := seedOwnerCompany(t, q, "dsr-handler-ok", "correct-horse-battery-staple")

	svc := NewService(q, pool)
	app := fiber.New()
	NewHandler(svc, q, zerolog.Nop()).Register(app, stubIdentity(co.ID, user.ID, "owner"))

	code, out := doJSON(t, app, "POST", "/v1/account/delete", map[string]any{"password": "correct-horse-battery-staple"})
	require.Equal(t, 200, code, "%v", out)
	require.Equal(t, true, out["deleted"])

	gate, err := q.GetCompanyGate(context.Background(), co.ID)
	require.NoError(t, err)
	require.True(t, gate.DeletedAt.Valid)

	action := "account.delete"
	rows, err := q.ListAuditLogs(context.Background(), sqlc.ListAuditLogsParams{CompanyID: &co.ID, Action: &action, Lim: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "user:"+user.ID, rows[0].Actor)
}

func TestHandlerDeleteWrongPasswordForbidden(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	co, user := seedOwnerCompany(t, q, "dsr-handler-badpw", "correct-horse-battery-staple")

	svc := NewService(q, pool)
	app := fiber.New()
	NewHandler(svc, q, zerolog.Nop()).Register(app, stubIdentity(co.ID, user.ID, "owner"))

	code, out := doJSON(t, app, "POST", "/v1/account/delete", map[string]any{"password": "wrong-password"})
	require.Equal(t, 403, code, "%v", out)

	gate, err := q.GetCompanyGate(context.Background(), co.ID)
	require.NoError(t, err)
	require.False(t, gate.DeletedAt.Valid)
}

func TestHandlerDeleteNonOwnerForbidden(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	co, user := seedOwnerCompany(t, q, "dsr-handler-nonowner", "correct-horse-battery-staple")

	svc := NewService(q, pool)
	app := fiber.New()
	NewHandler(svc, q, zerolog.Nop()).Register(app, stubIdentity(co.ID, user.ID, "member"))

	code, out := doJSON(t, app, "POST", "/v1/account/delete", map[string]any{"password": "correct-horse-battery-staple"})
	require.Equal(t, 403, code, "%v", out)

	gate, err := q.GetCompanyGate(context.Background(), co.ID)
	require.NoError(t, err)
	require.False(t, gate.DeletedAt.Valid)
}

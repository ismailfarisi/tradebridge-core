package impersonation

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/audit"
	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

const testJWTSecret = "test-secret"

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

// makeCompany inserts a bare company and returns its id.
func makeCompany(t *testing.T, q *sqlc.Queries, name string) string {
	t.Helper()
	co, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: name, Type: "seller"})
	require.NoError(t, err)
	return co.ID
}

// stubAdminIdentity injects the locals PlatformAdminGuard would derive from a
// real admin JWT, including the admin email the auditor records as actor.
func stubAdminIdentity(adminID, email string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalAdminID, adminID)
		c.Locals(middleware.LocalAdminEmail, email)
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

func newTestHandler(q *sqlc.Queries) *Handler {
	tokens := identity.NewTokenService(testJWTSecret, 24*time.Hour)
	auditor := audit.New(q, zerolog.Nop())
	return NewHandler(q, tokens, auditor)
}

func TestImpersonateHappyPath(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "impersonate-co")
	_, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
		CompanyID: cid, Email: "owner-" + cid + "@impersonate.test", Role: "owner",
	})
	require.NoError(t, err)

	app := fiber.New()
	newTestHandler(q).Register(app, stubAdminIdentity("admin-1", "ops@x.com"))

	code, out := doJSON(t, app, "POST", "/admin/v1/impersonate", map[string]string{"company_id": cid})
	require.Equal(t, 200, code, "%v", out)

	tok, _ := out["token"].(string)
	require.NotEmpty(t, tok)
	require.NotEmpty(t, out["user_id"])
	require.Equal(t, cid, out["company_id"])
	require.Equal(t, float64(1800), out["expires_in"])

	claims := &middleware.Claims{}
	_, err = jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (interface{}, error) {
		return []byte(testJWTSecret), nil
	})
	require.NoError(t, err)
	require.True(t, claims.ReadOnly)
	require.Equal(t, "ops@x.com", claims.ActingAdmin)
	require.Equal(t, cid, claims.CompanyID)

	logs, err := q.ListAuditLogs(context.Background(), sqlc.ListAuditLogsParams{CompanyID: &cid, Lim: 10})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "support.impersonate", logs[0].Action)
	require.Equal(t, "admin:ops@x.com", logs[0].Actor)
}

func TestImpersonateOnCompanyWithNoUsersReturns404(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "empty-co")

	app := fiber.New()
	newTestHandler(q).Register(app, stubAdminIdentity("admin-1", "ops@x.com"))

	code, _ := doJSON(t, app, "POST", "/admin/v1/impersonate", map[string]string{"company_id": cid})
	require.Equal(t, 404, code)
}

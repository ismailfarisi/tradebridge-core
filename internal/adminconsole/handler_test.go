package adminconsole

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/audit"
	"github.com/tradebridge/core/internal/entitlements"
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
	ent := entitlements.NewService(q, zerolog.Nop())
	auditor := audit.New(q, zerolog.Nop())
	return NewHandler(q, ent, auditor)
}

func TestSearchReturnsSeededCompany(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "search-co")

	app := fiber.New()
	newTestHandler(q).Register(app, stubAdminIdentity("admin-1", "ops@x.com"))

	code, out := doJSON(t, app, "GET", "/admin/v1/companies?q=search-co&limit=500", nil)
	require.Equal(t, 200, code, "%v", out)
	rows, ok := out["companies"].([]any)
	require.True(t, ok)
	found := false
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok && m["id"] == cid {
			found = true
		}
	}
	require.True(t, found, "seeded company present in search results")
}

func TestDetailReturnsStatusPlanAndCounts(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "detail-co")

	app := fiber.New()
	newTestHandler(q).Register(app, stubAdminIdentity("admin-1", "ops@x.com"))

	code, out := doJSON(t, app, "GET", "/admin/v1/companies/"+cid, nil)
	require.Equal(t, 200, code, "%v", out)
	require.Equal(t, "active", out["status"])
	require.Equal(t, "free", out["plan_code"])
	counts, ok := out["counts"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(0), counts["rfqs"])
	require.Equal(t, float64(0), counts["quotes"])
	require.Equal(t, float64(0), counts["orders"])
}

func TestSuspendFlipsStatusAndWritesAuditRow(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "suspend-co")

	app := fiber.New()
	newTestHandler(q).Register(app, stubAdminIdentity("admin-1", "ops@x.com"))

	code, out := doJSON(t, app, "POST", "/admin/v1/companies/"+cid+"/suspend", map[string]string{"reason": "fraud"})
	require.Equal(t, 200, code, "%v", out)
	require.Equal(t, "suspended", out["status"])

	status, err := q.GetCompanyStatus(context.Background(), cid)
	require.NoError(t, err)
	require.Equal(t, "suspended", status)

	logs, err := q.ListAuditLogs(context.Background(), sqlc.ListAuditLogsParams{CompanyID: &cid, Lim: 10})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "company.suspend", logs[0].Action)
	require.Equal(t, "admin:ops@x.com", logs[0].Actor)
	require.NotNil(t, logs[0].TargetID)
	require.Equal(t, cid, *logs[0].TargetID)
}

func TestUnsuspendFlipsStatusBack(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "unsuspend-co")
	require.NoError(t, q.SetCompanyStatus(context.Background(), sqlc.SetCompanyStatusParams{ID: cid, Status: "suspended"}))

	app := fiber.New()
	newTestHandler(q).Register(app, stubAdminIdentity("admin-1", "ops@x.com"))

	code, out := doJSON(t, app, "POST", "/admin/v1/companies/"+cid+"/unsuspend", nil)
	require.Equal(t, 200, code, "%v", out)
	require.Equal(t, "active", out["status"])

	status, err := q.GetCompanyStatus(context.Background(), cid)
	require.NoError(t, err)
	require.Equal(t, "active", status)

	logs, err := q.ListAuditLogs(context.Background(), sqlc.ListAuditLogsParams{CompanyID: &cid, Lim: 10})
	require.NoError(t, err)
	require.Len(t, logs, 1)
	require.Equal(t, "company.unsuspend", logs[0].Action)
}

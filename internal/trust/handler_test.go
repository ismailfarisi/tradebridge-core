package trust

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

func newTestApp(t *testing.T, res TXTResolver) (*fiber.App, *sqlc.Queries, string) {
	q, pool := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "handler-co")
	svc := NewService(q, pool, res, zerolog.Nop())
	app := fiber.New()
	// verified passthrough (email-verify is exercised elsewhere).
	verified := func(c *fiber.Ctx) error { return c.Next() }
	// stubIdentity injects the locals TenantGuard() would derive from real JWT
	// claims, so it fully replaces the JWT+TenantGuard guard chain here —
	// chaining a real TenantGuard() after it would 401 (no "claims" local set).
	NewHandler(svc, q).Register(app, verified, stubIdentity(cid, "owner"))
	return app, q, cid
}

func doJSON(t *testing.T, app *fiber.App, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var bodyReader = http.NoBody
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, bodyReader)
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

func TestHandlerClaimThenCheck(t *testing.T) {
	res := &fakeResolver{records: map[string][]string{}}
	app, q, cid := newTestApp(t, res)
	_, pool := testQueriesOrSkip(t)
	// Persistent test DB: release any stale claim from a prior invocation
	// before this test re-claims (and verifies) this hardcoded domain.
	releaseDomain(t, pool, "handler.example")
	t.Cleanup(func() { releaseDomain(t, pool, "handler.example") })

	code, out := doJSON(t, app, "POST", "/v1/verification/domain", map[string]string{"domain": "handler.example"})
	require.Equal(t, 200, code)
	recName := out["record_name"].(string)
	recVal := out["record_value"].(string)

	// Not published yet → pending.
	code, out = doJSON(t, app, "POST", "/v1/verification/domain/check", map[string]string{"domain": "handler.example"})
	require.Equal(t, 200, code)
	require.Equal(t, false, out["verified"])

	// Publish + check → verified.
	res.records[recName] = []string{recVal}
	code, out = doJSON(t, app, "POST", "/v1/verification/domain/check", map[string]string{"domain": "handler.example"})
	require.Equal(t, 200, code)
	require.Equal(t, true, out["verified"])

	v, _ := q.GetCompanyVerification(context.Background(), cid)
	require.Equal(t, "verified", v.VerificationStatus)
}

func TestHandlerCheckNoClaim(t *testing.T) {
	app, _, _ := newTestApp(t, &fakeResolver{})
	code, _ := doJSON(t, app, "POST", "/v1/verification/domain/check", map[string]string{"domain": "unclaimed.example"})
	require.Equal(t, 404, code)
}

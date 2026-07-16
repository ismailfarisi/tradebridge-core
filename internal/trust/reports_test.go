package trust

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestCreateReportAndAdminResolve(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	reporter := makeCompany(t, q, "reporter-co")
	reported := makeCompany(t, q, "reported-co")

	app := fiber.New()
	NewReportsHandler(q).Register(app, stubIdentity(reporter, "owner"), func(c *fiber.Ctx) error { return c.Next() })
	NewReportsHandler(q).RegisterAdmin(app, func(c *fiber.Ctx) error { return c.Next() })

	code, out := doJSON(t, app, "POST", "/v1/reports", map[string]string{"reported_company_id": reported, "reason": "spam", "detail": "spammy"})
	require.Equal(t, 200, code)
	id := out["id"].(string)
	require.NotEmpty(t, id)

	code, list := doJSON(t, app, "GET", "/admin/v1/reports?status=open&limit=100", nil)
	require.Equal(t, 200, code)
	require.Contains(t, list, "reports")

	code, res := doJSON(t, app, "POST", "/admin/v1/reports/"+id, map[string]string{"status": "dismissed"})
	require.Equal(t, 200, code)
	require.Equal(t, "dismissed", res["status"])
}

func TestCannotReportSelf(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	me := makeCompany(t, q, "self-report-co")
	app := fiber.New()
	NewReportsHandler(q).Register(app, stubIdentity(me, "owner"), func(c *fiber.Ctx) error { return c.Next() })
	code, _ := doJSON(t, app, "POST", "/v1/reports", map[string]string{"reported_company_id": me, "reason": "spam"})
	require.Equal(t, 400, code)
}

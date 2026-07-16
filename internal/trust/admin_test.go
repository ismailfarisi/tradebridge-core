package trust

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestAdminSetStatusVerifies(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "admin-verif-co")
	svc := NewService(q, pool, &fakeResolver{}, zerolog.Nop())
	app := fiber.New()
	pass := func(c *fiber.Ctx) error { return c.Next() }
	NewAdminHandler(svc, q).Register(app, pass)

	code, out := doJSON(t, app, "POST", "/admin/v1/companies/"+cid+"/verification", map[string]string{"status": "verified", "reason": "manual"})
	require.Equal(t, 200, code)
	require.Equal(t, "verified", out["verification_status"])
}

func TestAdminListFiltersByStatus(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, &fakeResolver{}, zerolog.Nop())
	app := fiber.New()
	pass := func(c *fiber.Ctx) error { return c.Next() }
	NewAdminHandler(svc, q).Register(app, pass)

	code, out := doJSON(t, app, "GET", "/admin/v1/verifications?status=verified&limit=5", nil)
	require.Equal(t, 200, code)
	require.Contains(t, out, "companies")
}

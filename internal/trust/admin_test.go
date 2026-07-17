package trust

import (
	"context"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/audit"
)

func TestAdminSetStatusVerifies(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "admin-verif-co")
	svc := NewService(q, pool, &fakeResolver{}, zerolog.Nop())
	app := fiber.New()
	pass := func(c *fiber.Ctx) error { return c.Next() }
	NewAdminHandler(svc, q, audit.New(q, zerolog.Nop())).Register(app, pass)

	code, out := doJSON(t, app, "POST", "/admin/v1/companies/"+cid+"/verification", map[string]string{"status": "verified", "reason": "manual"})
	require.Equal(t, 200, code)
	require.Equal(t, "verified", out["verification_status"])
}

func TestAdminListFiltersByStatus(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, &fakeResolver{}, zerolog.Nop())
	app := fiber.New()
	pass := func(c *fiber.Ctx) error { return c.Next() }
	NewAdminHandler(svc, q, audit.New(q, zerolog.Nop())).Register(app, pass)

	// A company we drive to 'verified' must appear under status=verified and be
	// absent under status=rejected — proving the filter actually filters.
	cid := makeCompany(t, q, "admin-filter-co")
	require.NoError(t, svc.SetStatus(context.Background(), cid, "verified", "seed"))

	code, out := doJSON(t, app, "GET", "/admin/v1/verifications?status=verified&limit=500", nil)
	require.Equal(t, 200, code)
	require.True(t, listContainsCompany(out, cid), "verified company present under status=verified")

	code, out = doJSON(t, app, "GET", "/admin/v1/verifications?status=rejected&limit=500", nil)
	require.Equal(t, 200, code)
	require.False(t, listContainsCompany(out, cid), "verified company absent under status=rejected")
}

// listContainsCompany reports whether the admin list response contains a company id.
func listContainsCompany(out map[string]any, id string) bool {
	rows, ok := out["companies"].([]any)
	if !ok {
		return false
	}
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			if m["id"] == id {
				return true
			}
		}
	}
	return false
}

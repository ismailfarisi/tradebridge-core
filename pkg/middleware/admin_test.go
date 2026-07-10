package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/middleware"
)

func adminToken(t *testing.T, secret, adminID, email, scope string) string {
	t.Helper()
	claims := &middleware.AdminClaims{
		AdminID: adminID, Email: email, Scope: scope,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return s
}

func adminApp(secret string) *fiber.App {
	f := fiber.New()
	f.Get("/admin/v1/me", middleware.PlatformAdminGuard(secret), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"admin_id": middleware.AdminID(c), "email": middleware.AdminEmail(c)})
	})
	return f
}

func doGet(t *testing.T, app *fiber.App, bearer string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	return resp.StatusCode
}

func TestPlatformAdminGuard(t *testing.T) {
	const secret = "admin-secret"
	app := adminApp(secret)

	// Valid admin token → 200.
	require.Equal(t, http.StatusOK, doGet(t, app, adminToken(t, secret, "a1", "a@x.com", middleware.ScopePlatformAdmin)))
	// No token → 401.
	require.Equal(t, http.StatusUnauthorized, doGet(t, app, ""))
	// Wrong secret → 401.
	require.Equal(t, http.StatusUnauthorized, doGet(t, app, adminToken(t, "other-secret", "a1", "a@x.com", middleware.ScopePlatformAdmin)))
	// Missing scope (e.g. a token without platform_admin scope) → 401.
	require.Equal(t, http.StatusUnauthorized, doGet(t, app, adminToken(t, secret, "a1", "a@x.com", "")))

	// A company token (different claims/secret) → 401.
	companyTok := adminToken(t, "company-secret", "", "", "") // signed with a different key; won't verify
	require.Equal(t, http.StatusUnauthorized, doGet(t, app, companyTok))
}

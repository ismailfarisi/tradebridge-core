package middleware

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret"

func testApp() *fiber.App {
	app := fiber.New()
	v1 := app.Group("/v1", JWT(testSecret), TenantGuard())
	v1.Get("/me", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"company_id": CompanyID(c), "user_id": UserID(c)})
	})
	return app
}

func signToken(t *testing.T, companyID, userID string) string {
	t.Helper()
	claims := &Claims{
		CompanyID: companyID,
		UserID:    userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(testSecret))
	require.NoError(t, err)
	return signed
}

func TestTenantGuardRejectsMissingToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/me", nil)
	resp, err := testApp().Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
}

func TestTenantGuardInjectsCompany(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+signToken(t, "cmp_123", "usr_456"))
	resp, err := testApp().Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "cmp_123")
	require.Contains(t, string(body), "usr_456")
}

func TestRequireActiveCompany(t *testing.T) {
	newApp := func(status string, statusErr error) *fiber.App {
		app := fiber.New()
		app.Use(func(c *fiber.Ctx) error { c.Locals(LocalCompanyID, "co_1"); return c.Next() })
		app.Use(RequireActiveCompany(func(context.Context, string) (string, error) {
			return status, statusErr
		}))
		app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })
		return app
	}

	t.Run("active passes", func(t *testing.T) {
		res, _ := newApp("active", nil).Test(httptest.NewRequest("GET", "/x", nil))
		if res.StatusCode != 200 {
			t.Fatalf("want 200, got %d", res.StatusCode)
		}
	})
	t.Run("suspended blocked 403", func(t *testing.T) {
		res, _ := newApp("suspended", nil).Test(httptest.NewRequest("GET", "/x", nil))
		if res.StatusCode != 403 {
			t.Fatalf("want 403, got %d", res.StatusCode)
		}
	})
}

func TestEnforceReadOnly(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &Claims{CompanyID: "co_1", ReadOnly: true})
		return c.Next()
	})
	app.Use(EnforceReadOnly())
	app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Post("/x", func(c *fiber.Ctx) error { return c.SendString("wrote") })

	get, _ := app.Test(httptest.NewRequest("GET", "/x", nil))
	if get.StatusCode != 200 {
		t.Fatalf("GET want 200, got %d", get.StatusCode)
	}
	post, _ := app.Test(httptest.NewRequest("POST", "/x", nil))
	if post.StatusCode != 403 {
		t.Fatalf("POST want 403, got %d", post.StatusCode)
	}
}

func TestEnforceReadOnlyPassesWhenNotReadOnly(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &Claims{CompanyID: "co_1"})
		return c.Next()
	})
	app.Use(EnforceReadOnly())
	app.Post("/x", func(c *fiber.Ctx) error { return c.SendString("wrote") })

	post, _ := app.Test(httptest.NewRequest("POST", "/x", nil))
	if post.StatusCode != 200 {
		t.Fatalf("POST want 200, got %d", post.StatusCode)
	}
}

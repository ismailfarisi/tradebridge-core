package middleware

import (
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

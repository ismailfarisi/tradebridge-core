package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// ScopePlatformAdmin is the required scope claim for the platform-admin realm.
const ScopePlatformAdmin = "platform_admin"

// Locals keys for the admin realm (kept separate from the tenant locals).
const (
	LocalAdminID    = "admin_id"
	LocalAdminEmail = "admin_email"
)

// AdminClaims is the JWT payload for the platform superadmin. It is signed with
// ADMIN_JWT_SECRET (distinct from the company JWT_SECRET) and carries a scope,
// so a company token can never satisfy PlatformAdminGuard and vice-versa.
type AdminClaims struct {
	AdminID string `json:"admin_id"`
	Email   string `json:"email"`
	Scope   string `json:"scope"`
	jwt.RegisteredClaims
}

// PlatformAdminGuard validates a Bearer admin token signed with secret and
// requires scope == platform_admin. It stashes admin_id/admin_email in locals.
func PlatformAdminGuard(secret string) fiber.Handler {
	key := []byte(secret)
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		raw := strings.TrimPrefix(auth, "Bearer ")

		claims := &AdminClaims{}
		token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return key, nil
		})
		if err != nil || !token.Valid || claims.Scope != ScopePlatformAdmin {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid admin token")
		}
		c.Locals(LocalAdminID, claims.AdminID)
		c.Locals(LocalAdminEmail, claims.Email)
		return c.Next()
	}
}

// AdminID returns the platform admin id for the current request.
func AdminID(c *fiber.Ctx) string {
	if v, ok := c.Locals(LocalAdminID).(string); ok {
		return v
	}
	return ""
}

// AdminEmail returns the platform admin email for the current request.
func AdminEmail(c *fiber.Ctx) string {
	if v, ok := c.Locals(LocalAdminEmail).(string); ok {
		return v
	}
	return ""
}

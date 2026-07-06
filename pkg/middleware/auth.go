// Package middleware holds cross-cutting Fiber middleware: JWT auth, the
// tenant guard, and request logging.
package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// Claims is the JWT payload TradeBridge issues. Every authenticated request
// carries a company_id (tenant) and a user_id.
type Claims struct {
	CompanyID string `json:"company_id"`
	UserID    string `json:"user_id"`
	jwt.RegisteredClaims
}

// JWT returns middleware that validates a Bearer token and stashes the parsed
// claims in c.Locals("claims"). It does not itself enforce tenancy — that is
// the tenant guard's job — so the two compose.
func JWT(secret string) fiber.Handler {
	key := []byte(secret)
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		raw := strings.TrimPrefix(auth, "Bearer ")

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return key, nil
		})
		if err != nil || !token.Valid {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid token")
		}

		c.Locals("claims", claims)
		return c.Next()
	}
}

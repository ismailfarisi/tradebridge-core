package middleware

import "github.com/gofiber/fiber/v2"

// Locals keys for tenant context. Handlers read these instead of re-parsing
// the JWT, so tenancy is enforced in exactly one place.
const (
	LocalCompanyID = "company_id"
	LocalUserID    = "user_id"
)

// TenantGuard must run after JWT. It lifts company_id/user_id out of the
// validated claims into request-scoped locals. A request that reaches here
// without a company_id is rejected — no tenant, no access.
func TenantGuard() fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*Claims)
		if !ok || claims.CompanyID == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "no tenant in token")
		}
		c.Locals(LocalCompanyID, claims.CompanyID)
		c.Locals(LocalUserID, claims.UserID)
		return c.Next()
	}
}

// CompanyID returns the tenant id for the current request. Safe only on routes
// behind TenantGuard.
func CompanyID(c *fiber.Ctx) string {
	if v, ok := c.Locals(LocalCompanyID).(string); ok {
		return v
	}
	return ""
}

// UserID returns the acting user id for the current request.
func UserID(c *fiber.Ctx) string {
	if v, ok := c.Locals(LocalUserID).(string); ok {
		return v
	}
	return ""
}

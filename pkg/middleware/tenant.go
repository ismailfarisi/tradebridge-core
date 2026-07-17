package middleware

import (
	"context"

	"github.com/gofiber/fiber/v2"
)

// Locals keys for tenant context. Handlers read these instead of re-parsing
// the JWT, so tenancy is enforced in exactly one place.
const (
	LocalCompanyID = "company_id"
	LocalUserID    = "user_id"
	LocalRole      = "role"
)

// TenantGuard must run after JWT. It lifts company_id/user_id/role out of the
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
		c.Locals(LocalRole, claims.Role)
		return c.Next()
	}
}

// RequireRole must run after TenantGuard. It allows the request only if the
// user's role is one of the permitted roles.
func RequireRole(allowed ...string) fiber.Handler {
	set := make(map[string]struct{}, len(allowed))
	for _, r := range allowed {
		set[r] = struct{}{}
	}
	return func(c *fiber.Ctx) error {
		if _, ok := set[Role(c)]; !ok {
			return fiber.NewError(fiber.StatusForbidden, "insufficient role")
		}
		return c.Next()
	}
}

// Role returns the acting user's role for the current request.
func Role(c *fiber.Ctx) string {
	if v, ok := c.Locals(LocalRole).(string); ok {
		return v
	}
	return ""
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

// RequireActiveCompany must run after TenantGuard. It rejects requests from a
// suspended company so an admin suspend takes effect immediately, not at token
// expiry. `status` is injected (typically q.GetCompanyStatus) so this package
// stays free of a sqlc dependency. One indexed PK lookup per authed request.
func RequireActiveCompany(status func(ctx context.Context, companyID string) (string, error)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		st, err := status(c.Context(), CompanyID(c))
		if err != nil {
			return err
		}
		if st == "suspended" {
			return fiber.NewError(fiber.StatusForbidden, "company suspended")
		}
		return c.Next()
	}
}

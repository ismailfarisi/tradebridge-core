package middleware

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// RequireVerified must run after TenantGuard. It blocks the request when the
// acting user has not verified their email (soft gate). Existing users are
// grandfathered (email_verified_at backfilled), so this only stops brand-new
// unverified signups from the gated action. The `code` field lets the frontend
// show the "verify your email" banner instead of a raw error string.
func RequireVerified(q *sqlc.Queries) fiber.Handler {
	return func(c *fiber.Ctx) error {
		u, err := q.GetUserByID(c.Context(), UserID(c))
		if errors.Is(err, pgx.ErrNoRows) {
			return fiber.NewError(fiber.StatusUnauthorized, "unknown user")
		} else if err != nil {
			return err
		}
		if !u.EmailVerifiedAt.Valid {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"code": "verify_email", "message": "verify your email to continue",
			})
		}
		return c.Next()
	}
}

package waitlist

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Handler struct{ q *sqlc.Queries }

func NewHandler(q *sqlc.Queries) *Handler { return &Handler{q: q} }

// Register mounts the PUBLIC waitlist route (no tenant guard — pre-signup).
// rl is a fail-open per-IP rate limiter (nil Redis => pass-through), mirroring
// every other unauthenticated public write (e.g. /v1/auth/register).
func (h *Handler) Register(app *fiber.App, rl fiber.Handler) {
	app.Post("/v1/waitlist", rl, h.join)
}

type joinReq struct {
	Email  string `json:"email"`
	Source string `json:"source"`
}

func (h *Handler) join(c *fiber.Ctx) error {
	var req joinReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(email, "@") || !strings.Contains(email, ".") || len(email) < 5 {
		return fiber.NewError(fiber.StatusBadRequest, "valid email required")
	}
	var source *string
	if s := strings.TrimSpace(req.Source); s != "" {
		source = &s
	}
	if _, err := h.q.CreateWaitlistSignup(c.Context(), sqlc.CreateWaitlistSignupParams{
		Email: email, Source: source,
	}); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"joined": true})
}

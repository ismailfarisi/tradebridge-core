package admin

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the admin auth routes.
type Handler struct {
	svc    *Service
	tokens *TokenService
}

// NewHandler builds the admin handler.
func NewHandler(svc *Service, tokens *TokenService) *Handler {
	return &Handler{svc: svc, tokens: tokens}
}

// Register mounts /admin/v1/login (public) and /admin/v1/me (guarded).
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Post("/admin/v1/login", h.login)
	app.Get("/admin/v1/me", append(append([]fiber.Handler{}, guard...), h.me)...)
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(c *fiber.Ctx) error {
	var req loginReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	admin, err := h.svc.Authenticate(c.Context(), req.Email, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	} else if err != nil {
		return err
	}
	tok, err := h.tokens.Issue(admin.ID, admin.Email)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"token": tok})
}

func (h *Handler) me(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"admin_id": middleware.AdminID(c), "email": middleware.AdminEmail(c)})
}

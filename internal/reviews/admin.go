package reviews

import (
	"github.com/gofiber/fiber/v2"
)

// AdminHandler is the platform-admin moderation surface for reviews.
type AdminHandler struct {
	svc *Service
}

// NewAdminHandler builds the reviews admin handler.
func NewAdminHandler(svc *Service) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// Register mounts the admin remove route.
func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1", adminGuard)
	g.Post("/reviews/:id/remove", h.remove)
}

// remove marks a review as removed, dropping it out of reputation
// aggregates and public listings.
func (h *AdminHandler) remove(c *fiber.Ctx) error {
	review, err := h.svc.Remove(c.Context(), c.Params("id"))
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"review": review})
}

package settings

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the company settings routes (owner/admin only).
type Handler struct {
	svc *Service
}

// NewHandler builds the settings handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts settings routes under the tenant guard, restricted to
// owner/admin (settings hold provider credentials).
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/settings", guard...)
	g.Get("/extraction", middleware.RequireRole("owner", "admin"), h.get)
	g.Put("/extraction", middleware.RequireRole("owner", "admin"), h.update)
}

func (h *Handler) get(c *fiber.Ctx) error {
	view, err := h.svc.Get(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(view)
}

func (h *Handler) update(c *fiber.Ctx) error {
	var in ExtractionInput
	if err := c.BodyParser(&in); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if in.Provider != "" && in.Provider != "anthropic" && in.Provider != "openai_compatible" {
		return fiber.NewError(fiber.StatusBadRequest, "provider must be 'anthropic' or 'openai_compatible'")
	}
	view, err := h.svc.UpdateExtraction(c.Context(), middleware.CompanyID(c), in)
	if err != nil {
		return err
	}
	return c.JSON(view)
}

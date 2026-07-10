package settings

import (
	"github.com/gofiber/fiber/v2"
)

// Handler serves the global platform settings routes (admin realm only).
type Handler struct {
	svc *Service
}

// NewHandler builds the settings handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts settings routes under the platform admin guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/admin/v1/settings", guard...)
	g.Get("/", h.get)
	g.Put("/", h.update)
}

func (h *Handler) get(c *fiber.Ctx) error {
	view, err := h.svc.Get(c.Context())
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
	view, err := h.svc.Update(c.Context(), in)
	if err != nil {
		return err
	}
	return c.JSON(view)
}

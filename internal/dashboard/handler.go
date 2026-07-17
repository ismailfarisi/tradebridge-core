package dashboard

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the dashboard read endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register mounts the tenant-guarded dashboard routes.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/dashboard", guard...)
	g.Get("/buyer", h.buyer)
}

func (h *Handler) buyer(c *fiber.Ctx) error {
	d, err := h.svc.BuyerDashboard(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(d)
}

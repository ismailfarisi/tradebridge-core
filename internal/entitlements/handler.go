package entitlements

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the buyer app's billing/entitlements read model.
type Handler struct {
	svc *Service
}

// NewHandler builds the billing summary handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts GET /v1/billing/summary under the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/billing", guard...)
	g.Get("/summary", h.summary)
}

// summary returns the caller company's plan, entitlements, and usage.
func (h *Handler) summary(c *fiber.Ctx) error {
	companyID := middleware.CompanyID(c)
	return c.JSON(fiber.Map{
		"plan":         h.svc.PlanCode(c.Context(), companyID),
		"entitlements": h.svc.For(c.Context(), companyID),
		"usage":        h.svc.Usage(c.Context(), companyID),
	})
}

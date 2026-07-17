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

// Register mounts the tenant-guarded dashboard + analytics routes.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/dashboard", guard...)
	g.Get("/buyer", h.buyer)
	g.Get("/seller", h.seller)

	a := app.Group("/v1/analytics", guard...)
	a.Get("/seller", h.sellerAnalytics)
}

func (h *Handler) buyer(c *fiber.Ctx) error {
	d, err := h.svc.BuyerDashboard(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(d)
}

func (h *Handler) seller(c *fiber.Ctx) error {
	d, err := h.svc.SellerDashboard(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(d)
}

func (h *Handler) sellerAnalytics(c *fiber.Ctx) error {
	a, err := h.svc.SellerAnalytics(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(a)
}

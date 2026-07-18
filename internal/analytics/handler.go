package analytics

import "github.com/gofiber/fiber/v2"

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(app *fiber.App, adminGuard fiber.Handler) {
	app.Get("/admin/v1/analytics/business", adminGuard, h.business)
}

func (h *Handler) business(c *fiber.Ctx) error {
	s, err := h.svc.Business(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(s)
}

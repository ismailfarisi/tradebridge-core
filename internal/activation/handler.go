package activation

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/activation", guard...)
	g.Get("/", h.status)
	g.Get("", h.status)
}

func (h *Handler) status(c *fiber.Ctx) error {
	st, err := h.svc.Status(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(st)
}

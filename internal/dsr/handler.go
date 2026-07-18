package dsr

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Get("/v1/account/export", append(append([]fiber.Handler{}, guard...), h.export)...)
}

func (h *Handler) export(c *fiber.Ctx) error {
	role := middleware.Role(c)
	if role != "owner" && role != "admin" {
		return fiber.NewError(fiber.StatusForbidden, "owner or admin required")
	}
	bundle, err := h.svc.Export(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	c.Set("Content-Disposition", `attachment; filename="tradebridge-export.json"`)
	return c.JSON(fiber.Map{"bundle": bundle})
}

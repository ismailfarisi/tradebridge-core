package disputes

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves party dispute routes, mounted under the tenant guard.
type Handler struct {
	svc *Service
}

// NewHandler builds the disputes handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts party dispute routes under the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/orders", guard...)
	g.Post("/:id/disputes", h.raise)
	g.Get("/:id/disputes", h.list)
}

type raiseDisputeReq struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// raise escalates an order the caller's company is a party to.
func (h *Handler) raise(c *fiber.Ctx) error {
	var req raiseDisputeReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	dispute, err := h.svc.Raise(c.Context(), c.Params("id"), middleware.CompanyID(c), req.Reason, req.Detail)
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"dispute": dispute})
}

// list returns the disputes raised on an order, scoped to a caller who is a
// party to it.
func (h *Handler) list(c *fiber.Ctx) error {
	disputes, err := h.svc.ListForOrder(c.Context(), c.Params("id"), middleware.CompanyID(c))
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"disputes": disputes})
}

// mapErr maps service-layer sentinel/typed errors to their HTTP status.
func mapErr(err error) error {
	var validation ValidationError
	if errors.As(err, &validation) {
		return fiber.NewError(fiber.StatusBadRequest, validation.Error())
	}
	var conflict ConflictError
	if errors.As(err, &conflict) {
		return fiber.NewError(fiber.StatusConflict, conflict.Error())
	}
	if errors.Is(err, ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return err
}

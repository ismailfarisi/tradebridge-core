package reviews

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves party review routes, mounted under the tenant guard.
type Handler struct {
	svc *Service
}

// NewHandler builds the reviews handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts party review routes under the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/orders", guard...)
	g.Post("/:id/reviews", h.create)
	g.Get("/:id/reviews", h.list)
}

type createReviewReq struct {
	Rating int    `json:"rating"`
	Body   string `json:"body"`
}

// create leaves a review of the counterparty on a completed order the
// caller's company is a party to.
func (h *Handler) create(c *fiber.Ctx) error {
	var req createReviewReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	review, err := h.svc.Create(c.Context(), c.Params("id"), middleware.CompanyID(c), req.Rating, req.Body)
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"review": review})
}

// list returns the reviews left on an order, scoped to a caller who is a
// party to it.
func (h *Handler) list(c *fiber.Ctx) error {
	reviews, err := h.svc.ListForOrder(c.Context(), c.Params("id"), middleware.CompanyID(c))
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"reviews": reviews})
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
	var forbidden ForbiddenError
	if errors.As(err, &forbidden) {
		return fiber.NewError(fiber.StatusForbidden, forbidden.Error())
	}
	if errors.Is(err, ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return err
}

// Package orders serves purchase orders created when a buyer accepts a quote.
// Both parties to an order (buyer and seller) may view it.
package orders

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves order routes.
type Handler struct {
	svc *Service
	q   *sqlc.Queries
}

// NewHandler builds the orders handler.
func NewHandler(svc *Service, q *sqlc.Queries) *Handler {
	return &Handler{svc: svc, q: q}
}

// Register mounts order routes under the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/orders", guard...)
	g.Get("/", h.list)
	g.Get("/:id", h.get)
	g.Post("/:id/transitions", h.transition)
}

// list returns every order the caller's company is a party to.
func (h *Handler) list(c *fiber.Ctx) error {
	orders, err := h.svc.List(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"orders": orders})
}

// get returns a single order plus its full event timeline, scoped to a
// caller who is a party to it.
func (h *Handler) get(c *fiber.Ctx) error {
	order, events, err := h.svc.GetWithTimeline(c.Context(), c.Params("id"), middleware.CompanyID(c))
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"order": order, "events": events})
}

type transitionReq struct {
	To   string `json:"to"`
	Note string `json:"note"`
}

// transition drives an order to a new lifecycle state on behalf of the
// caller's company.
func (h *Handler) transition(c *fiber.Ctx) error {
	var req transitionReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	order, err := h.svc.Transition(c.Context(), c.Params("id"), middleware.CompanyID(c), req.To, req.Note)
	if err != nil {
		return mapErr(err)
	}
	return c.JSON(fiber.Map{"order": order})
}

// mapErr maps service-layer sentinel/typed errors to their HTTP status.
// Fiber renders the message as plain text, matching the web `call<T>` helper.
func mapErr(err error) error {
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

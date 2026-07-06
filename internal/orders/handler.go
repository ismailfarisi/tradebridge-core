// Package orders serves purchase orders created when a buyer accepts a quote.
// Both parties to an order (buyer and seller) may view it.
package orders

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves order routes.
type Handler struct {
	q *sqlc.Queries
}

// NewHandler builds the orders handler.
func NewHandler(q *sqlc.Queries) *Handler {
	return &Handler{q: q}
}

// Register mounts order routes under the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/orders", guard...)
	g.Get("/:id", h.get)
}

func (h *Handler) get(c *fiber.Ctx) error {
	order, err := h.q.GetOrder(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "order not found")
	} else if err != nil {
		return err
	}
	me := middleware.CompanyID(c)
	if order.BuyerCompanyID != me && order.SellerCompanyID != me {
		// Only the two parties to the order can see it.
		return fiber.NewError(fiber.StatusNotFound, "order not found")
	}
	return c.JSON(order)
}

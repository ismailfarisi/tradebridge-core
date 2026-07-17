package invoicing

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the downloadable PDF invoice for an order.
type Handler struct {
	svc *Service
	q   *sqlc.Queries
}

// NewHandler builds the invoicing handler.
func NewHandler(svc *Service, q *sqlc.Queries) *Handler {
	return &Handler{svc: svc, q: q}
}

// Register mounts the invoice PDF route under the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Group("/v1/orders", guard...).Get("/:id/invoice.pdf", h.pdf)
}

// pdf streams the PDF rendering of an order's invoice to a party (buyer or
// seller) of that order. It returns 404 — never a distinguishing error — for
// both a nonexistent order and an order the caller isn't a party to, so a
// caller can't probe for the existence of orders across tenants.
func (h *Handler) pdf(c *fiber.Ctx) error {
	ctx := c.Context()
	id := c.Params("id")

	order, err := h.q.GetOrder(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "order not found")
	} else if err != nil {
		return err
	}

	me := middleware.CompanyID(c)
	if me != order.BuyerCompanyID && me != order.SellerCompanyID {
		return fiber.NewError(fiber.StatusNotFound, "order not found")
	}

	inv, lines, err := h.svc.GetForOrder(ctx, h.q, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "invoice not found")
	} else if err != nil {
		return err
	}

	buyerCo, err := h.q.GetCompanyByID(ctx, order.BuyerCompanyID)
	if err != nil {
		return err
	}
	sellerCo, err := h.q.GetCompanyByID(ctx, order.SellerCompanyID)
	if err != nil {
		return err
	}

	pdfBytes, err := Render(inv, lines, Party{Name: sellerCo.Name}, Party{Name: buyerCo.Name})
	if err != nil {
		return err
	}

	c.Set("Content-Type", "application/pdf")
	c.Set("Content-Disposition", "inline; filename=\""+inv.InvoiceNumber+".pdf\"")
	return c.Send(pdfBytes)
}

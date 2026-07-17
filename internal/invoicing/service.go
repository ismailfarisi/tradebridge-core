// Package invoicing generates and serves invoices created at quote
// acceptance: one invoice with one line item per order, plus a downloadable
// PDF rendering.
package invoicing

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Service creates and reads invoices. CreateForOrder takes a caller-supplied
// *sqlc.Queries so it can participate in the caller's transaction (quote
// acceptance creates the invoice atomically with the order).
type Service struct {
	logger zerolog.Logger
}

// NewService builds the invoicing service.
func NewService(logger zerolog.Logger) *Service {
	return &Service{logger: logger}
}

// CreateForOrder inserts an invoice and its single line item for order,
// derived from the RFQ that produced it. It uses the passed q so callers can
// run it inside their own transaction.
func (s *Service) CreateForOrder(ctx context.Context, q *sqlc.Queries, order sqlc.Order, r sqlc.Rfq) (sqlc.Invoice, error) {
	description, quantity, unitPriceCents, amountCents := deriveLineItem(order, r)

	inv, err := q.CreateInvoice(ctx, sqlc.CreateInvoiceParams{
		OrderID:         order.ID,
		BuyerCompanyID:  order.BuyerCompanyID,
		SellerCompanyID: order.SellerCompanyID,
		Currency:        order.Currency,
		SubtotalCents:   amountCents,
		TotalCents:      order.TotalCents,
	})
	if err != nil {
		return sqlc.Invoice{}, err
	}

	if _, err := q.CreateInvoiceLineItem(ctx, sqlc.CreateInvoiceLineItemParams{
		InvoiceID:      inv.ID,
		Description:    description,
		Quantity:       quantity,
		UnitPriceCents: unitPriceCents,
		AmountCents:    amountCents,
	}); err != nil {
		return sqlc.Invoice{}, err
	}

	return inv, nil
}

// GetForOrder returns the invoice for orderID plus its line items.
func (s *Service) GetForOrder(ctx context.Context, q *sqlc.Queries, orderID string) (sqlc.Invoice, []sqlc.InvoiceLineItem, error) {
	inv, err := q.GetInvoiceByOrder(ctx, orderID)
	if err != nil {
		return sqlc.Invoice{}, nil, err
	}
	lines, err := q.ListInvoiceLineItems(ctx, inv.ID)
	if err != nil {
		return sqlc.Invoice{}, nil, err
	}
	return inv, lines, nil
}

// requirements is the subset of an RFQ's free-form JSON requirements blob
// this package understands: a best-effort positive integer quantity.
type requirements struct {
	Quantity json.Number `json:"quantity"`
}

// deriveLineItem derives the sole invoice line item for order from the RFQ
// that produced it. description falls back to "Order <id>" when the RFQ has
// no title. quantity is a best-effort positive integer parsed from the RFQ's
// requirements JSON (key "quantity"), defaulting to 1 when absent, unparsable,
// or non-positive. amountCents is always order.TotalCents (authoritative);
// unitPriceCents is integer-divided from it for display and may not multiply
// back exactly.
func deriveLineItem(order sqlc.Order, r sqlc.Rfq) (description string, quantity, unitPriceCents, amountCents int64) {
	description = r.Title
	if description == "" {
		description = "Order " + order.ID
	}

	quantity = 1
	if len(r.Requirements) > 0 {
		var req requirements
		if err := json.Unmarshal(r.Requirements, &req); err == nil && req.Quantity != "" {
			if n, err := req.Quantity.Int64(); err == nil && n > 0 {
				quantity = n
			}
		}
	}

	amountCents = order.TotalCents
	unitPriceCents = amountCents / quantity

	return description, quantity, unitPriceCents, amountCents
}

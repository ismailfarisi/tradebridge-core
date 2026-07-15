package gateway

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// attachment serves RFQ attachment bytes to an authenticated agent whose
// company is matched to the attachment's RFQ. Anything else is a 404 so
// attachment IDs don't leak existence.
func (h *Handler) attachment(c *fiber.Ctx) error {
	agent, err := h.authenticate(c)
	if err != nil {
		return err
	}
	row, err := h.q.GetAttachment(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "attachment not found")
	} else if err != nil {
		return err
	}
	allowed := false
	if row.RfqID != nil {
		matched, err := h.q.SellerMatchedToRFQ(c.Context(), sqlc.SellerMatchedToRFQParams{
			RfqID: *row.RfqID, SellerCompanyID: agent.CompanyID,
		})
		allowed = err == nil && matched
	}
	if !allowed {
		return fiber.NewError(fiber.StatusNotFound, "attachment not found")
	}
	data, err := h.store.Get(row.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "attachment data missing")
	}
	c.Set(fiber.HeaderContentType, row.Mime)
	c.Set(fiber.HeaderContentDisposition, attachments.ContentDisposition(row.Filename))
	return c.Send(data)
}

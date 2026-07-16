package trust

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// BlocksHandler serves company block/unblock/list.
type BlocksHandler struct {
	q *sqlc.Queries
}

func NewBlocksHandler(q *sqlc.Queries) *BlocksHandler {
	return &BlocksHandler{q: q}
}

func (h *BlocksHandler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/blocks", guard...)
	g.Get("/", h.list)
	g.Post("/", h.create)
	g.Delete("/:blockedID", h.remove)
}

type blockReq struct {
	BlockedCompanyID string `json:"blocked_company_id"`
}

func (h *BlocksHandler) create(c *fiber.Ctx) error {
	var req blockReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	me := middleware.CompanyID(c)
	if req.BlockedCompanyID == "" || req.BlockedCompanyID == me {
		return fiber.NewError(fiber.StatusBadRequest, "invalid blocked company")
	}
	// ON CONFLICT DO NOTHING ... RETURNING * returns pgx.ErrNoRows on a
	// duplicate; treat that as success, not an error.
	_, err := h.q.CreateBlock(c.Context(), sqlc.CreateBlockParams{CompanyID: me, BlockedCompanyID: req.BlockedCompanyID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return c.JSON(fiber.Map{"blocked_company_id": req.BlockedCompanyID, "status": "blocked"})
}

func (h *BlocksHandler) remove(c *fiber.Ctx) error {
	if err := h.q.DeleteBlock(c.Context(), sqlc.DeleteBlockParams{
		CompanyID: middleware.CompanyID(c), BlockedCompanyID: c.Params("blockedID"),
	}); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *BlocksHandler) list(c *fiber.Ctx) error {
	rows, err := h.q.ListBlocksByCompany(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"blocks": rows, "count": len(rows)})
}

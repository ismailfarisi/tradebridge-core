package disputes

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/audit"
	"github.com/tradebridge/core/pkg/middleware"
)

// AdminHandler is the platform-admin dispute queue + resolution surface.
type AdminHandler struct {
	svc     *Service
	auditor *audit.Auditor
}

// NewAdminHandler builds the disputes admin handler.
func NewAdminHandler(svc *Service, auditor *audit.Auditor) *AdminHandler {
	return &AdminHandler{svc: svc, auditor: auditor}
}

// RegisterAdmin mounts the admin dispute queue + resolve routes.
func (h *AdminHandler) RegisterAdmin(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1", adminGuard)
	g.Get("/disputes", h.list)
	g.Post("/disputes/:id/resolve", h.resolve)
}

// list returns the dispute queue, optionally filtered by ?status=.
func (h *AdminHandler) list(c *fiber.Ctx) error {
	disputes, err := h.svc.ListForAdmin(c.Context(), c.Query("status"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"disputes": disputes})
}

type resolveDisputeReq struct {
	Outcome    string `json:"outcome"`
	OrderTo    string `json:"order_to"`
	Resolution string `json:"resolution"`
}

// resolve applies the calling admin's resolution to a dispute.
func (h *AdminHandler) resolve(c *fiber.Ctx) error {
	var req resolveDisputeReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	dispute, err := h.svc.Resolve(c.Context(), c.Params("id"), middleware.AdminID(c), req.Outcome, req.OrderTo, req.Resolution)
	if err != nil {
		return mapErr(err)
	}
	h.auditor.Admin(c.Context(), middleware.AdminEmail(c), "dispute.resolve", dispute.RaisedByCompanyID,
		map[string]any{"dispute_id": dispute.ID, "outcome": req.Outcome})
	return c.JSON(fiber.Map{"dispute": dispute})
}

package entitlements

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// AdminHandler serves the platform-admin plan-limit editor.
type AdminHandler struct {
	q   *sqlc.Queries
	svc *Service
}

// NewAdminHandler builds the admin plan handler.
func NewAdminHandler(q *sqlc.Queries, svc *Service) *AdminHandler {
	return &AdminHandler{q: q, svc: svc}
}

// Register mounts the plan routes behind the caller-supplied admin guard.
func (h *AdminHandler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/admin/v1/plans", guard...)
	g.Get("/", h.list)
	g.Patch("/:code", h.patch)
}

type planView struct {
	Code            string `json:"code"`
	Name            string `json:"name"`
	AgentAutonomy   bool   `json:"agent_autonomy"`
	CatalogMaxItems int32  `json:"catalog_max_items"`
	RFQMonthlyLimit int32  `json:"rfq_monthly_limit"`
	Seats           int32  `json:"seats"`
}

func toPlanView(p sqlc.Plan) planView {
	return planView{
		Code: p.Code, Name: p.Name, AgentAutonomy: p.AgentAutonomy,
		CatalogMaxItems: p.CatalogMaxItems, RFQMonthlyLimit: p.RfqMonthlyLimit, Seats: p.Seats,
	}
}

func (h *AdminHandler) list(c *fiber.Ctx) error {
	plans, err := h.q.ListPlans(c.Context())
	if err != nil {
		return err
	}
	out := make([]planView, len(plans))
	for i, p := range plans {
		out[i] = toPlanView(p)
	}
	return c.JSON(fiber.Map{"plans": out})
}

type patchPlanReq struct {
	AgentAutonomy   bool  `json:"agent_autonomy"`
	CatalogMaxItems int32 `json:"catalog_max_items"`
	RFQMonthlyLimit int32 `json:"rfq_monthly_limit"`
	Seats           int32 `json:"seats"`
}

func (h *AdminHandler) patch(c *fiber.Ctx) error {
	code := c.Params("code")
	if code != "free" && code != "pro" && code != "business" {
		return fiber.NewError(fiber.StatusBadRequest, "code must be free|pro|business")
	}
	var req patchPlanReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if err := validLimit(req.CatalogMaxItems); err != nil {
		return err
	}
	if err := validLimit(req.RFQMonthlyLimit); err != nil {
		return err
	}
	if err := validLimit(req.Seats); err != nil {
		return err
	}

	p, err := h.q.UpdatePlanLimits(c.Context(), sqlc.UpdatePlanLimitsParams{
		Code: code, AgentAutonomy: req.AgentAutonomy,
		CatalogMaxItems: req.CatalogMaxItems, RfqMonthlyLimit: req.RFQMonthlyLimit, Seats: req.Seats,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "plan not found")
	} else if err != nil {
		return err
	}
	h.svc.InvalidateCache() // edits take effect immediately, no restart
	return c.JSON(toPlanView(p))
}

// validLimit rejects values below -1 (only -1 or a non-negative cap are valid).
func validLimit(v int32) error {
	if v < -1 {
		return fiber.NewError(fiber.StatusBadRequest, "limit must be -1 (unlimited) or >= 0")
	}
	return nil
}

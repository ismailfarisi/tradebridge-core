package trust

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// AdminHandler is the platform-admin override/revoke surface for verification.
type AdminHandler struct {
	svc *Service
	q   *sqlc.Queries
}

func NewAdminHandler(svc *Service, q *sqlc.Queries) *AdminHandler {
	return &AdminHandler{svc: svc, q: q}
}

func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1", adminGuard)
	g.Get("/verifications", h.list)
	g.Post("/companies/:id/verification", h.setStatus)
}

func (h *AdminHandler) list(c *fiber.Ctx) error {
	var status *string
	if s := c.Query("status"); s != "" {
		status = &s
	}
	limit := parseIntDefault(c.Query("limit"), 50, 500)
	offset := parseIntDefault(c.Query("offset"), 0, 1_000_000)
	rows, err := h.q.ListCompaniesByVerificationStatus(c.Context(), sqlc.ListCompaniesByVerificationStatusParams{
		Status: status, Lim: int32(limit), Off: int32(offset),
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"companies": rows, "count": len(rows)})
}

type setStatusReq struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (h *AdminHandler) setStatus(c *fiber.Ctx) error {
	companyID := c.Params("id")
	var req setStatusReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if err := h.svc.SetStatus(c.Context(), companyID, req.Status, req.Reason); err != nil {
		if err == ErrDomainTaken {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"code": "domain_taken"})
		}
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	v, err := h.q.GetCompanyVerification(c.Context(), companyID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"company_id": companyID, "verification_status": v.VerificationStatus})
}

// parseIntDefault parses a positive int, clamping to [0, maxVal] and falling
// back to def on empty/invalid input. Mirrors internal/suppliers/handler.go's
// copy — Go has no cross-package private sharing.
func parseIntDefault(s string, def, maxVal int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

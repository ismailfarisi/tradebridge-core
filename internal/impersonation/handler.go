// Package impersonation issues scoped, read-only, audited support tokens that
// let an operator view a company exactly as its users would — but never write.
package impersonation

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/audit"
	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	q       *sqlc.Queries
	tokens  *identity.TokenService
	auditor *audit.Auditor
}

func NewHandler(q *sqlc.Queries, tokens *identity.TokenService, auditor *audit.Auditor) *Handler {
	return &Handler{q: q, tokens: tokens, auditor: auditor}
}

func (h *Handler) Register(app *fiber.App, adminGuard fiber.Handler) {
	app.Post("/admin/v1/impersonate", adminGuard, h.impersonate)
}

type impersonateReq struct {
	CompanyID string `json:"company_id"`
}

func (h *Handler) impersonate(c *fiber.Ctx) error {
	var req impersonateReq
	if err := c.BodyParser(&req); err != nil || req.CompanyID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "company_id required")
	}
	users, err := h.q.ListUsersByCompany(c.Context(), req.CompanyID)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		return fiber.NewError(fiber.StatusNotFound, "company has no users")
	}
	target := users[0]
	for _, u := range users {
		if u.Role == "owner" {
			target = u
			break
		}
	}
	adminEmail := middleware.AdminEmail(c)
	tok, err := h.tokens.IssueImpersonation(req.CompanyID, target.ID, target.Role, adminEmail)
	if err != nil {
		return err
	}
	h.auditor.Admin(c.Context(), adminEmail, "support.impersonate", req.CompanyID,
		map[string]any{"user_id": target.ID})
	return c.JSON(fiber.Map{
		"token":      tok,
		"company_id": req.CompanyID,
		"user_id":    target.ID,
		"expires_in": int(identity.ImpersonationTTL.Seconds()),
	})
}

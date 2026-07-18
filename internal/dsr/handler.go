package dsr

import (
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	svc    *Service
	q      *sqlc.Queries
	logger zerolog.Logger
}

// NewHandler builds the DSR handler. q is used directly (not through
// audit.Auditor) to re-verify the acting user's password and to write the
// account.delete audit row with a "user:" actor — Auditor.Admin always
// prefixes "admin:", which would misrepresent self-service deletion.
func NewHandler(svc *Service, q *sqlc.Queries, logger zerolog.Logger) *Handler {
	return &Handler{svc: svc, q: q, logger: logger}
}

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Get("/v1/account/export", append(append([]fiber.Handler{}, guard...), h.export)...)
	app.Post("/v1/account/delete", append(append([]fiber.Handler{}, guard...), h.delete)...)
}

func (h *Handler) export(c *fiber.Ctx) error {
	role := middleware.Role(c)
	if role != "owner" && role != "admin" {
		return fiber.NewError(fiber.StatusForbidden, "owner or admin required")
	}
	bundle, err := h.svc.Export(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	c.Set("Content-Disposition", `attachment; filename="tradebridge-export.json"`)
	return c.JSON(fiber.Map{"bundle": bundle})
}

// delete performs GDPR account deletion: owner-only, re-verifies the acting
// user's password (deletion via a stolen/left-open session token alone is
// not enough), then soft-deletes + anonymizes the company. The audit row is
// written directly with the acting user as actor, best-effort — a write
// failure is logged but does not fail the request (the deletion already
// committed).
func (h *Handler) delete(c *fiber.Ctx) error {
	if middleware.Role(c) != "owner" {
		return fiber.NewError(fiber.StatusForbidden, "owner required")
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	userID := middleware.UserID(c)
	u, err := h.q.GetUserByID(c.Context(), userID)
	if err != nil {
		return err
	}
	if u.PasswordHash == nil || !identity.CheckPassword(*u.PasswordHash, body.Password) {
		return fiber.NewError(fiber.StatusForbidden, "password incorrect")
	}
	companyID := middleware.CompanyID(c)
	if err := h.svc.Delete(c.Context(), companyID, userID); err != nil {
		return err
	}

	if _, err := h.q.CreateAuditLog(c.Context(), sqlc.CreateAuditLogParams{
		CompanyID: companyID,
		Actor:     "user:" + userID,
		Action:    "account.delete",
		TargetID:  &companyID,
		Detail:    []byte("{}"),
	}); err != nil {
		h.logger.Warn().Err(err).Str("company_id", companyID).Msg("account.delete audit write failed")
	}

	return c.JSON(fiber.Map{"deleted": true})
}

package audit

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// AdminHandler serves the read-only audit-log viewer.
type AdminHandler struct {
	q *sqlc.Queries
}

func NewAdminHandler(q *sqlc.Queries) *AdminHandler {
	return &AdminHandler{q: q}
}

// Register mounts GET /admin/v1/audit behind the platform-admin guard.
func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler) {
	app.Get("/admin/v1/audit", adminGuard, h.list)
}

func (h *AdminHandler) list(c *fiber.Ctx) error {
	optional := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	limit := 100
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && n > 0 {
		if n > 500 {
			n = 500
		}
		limit = n
	}
	rows, err := h.q.ListAuditLogs(c.Context(), sqlc.ListAuditLogsParams{
		CompanyID: optional(c.Query("company_id")),
		Actor:     optional(c.Query("actor")),
		Action:    optional(c.Query("action")),
		Lim:       int32(limit),
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"entries": rows, "count": len(rows)})
}

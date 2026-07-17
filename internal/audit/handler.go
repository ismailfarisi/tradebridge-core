package audit

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// auditEntry is the viewer's wire representation of a sqlc.AuditLog row.
// Detail is exposed as json.RawMessage so the stored jsonb bytes are
// serialized as a real JSON object instead of being base64-encoded (the
// default encoding/json behavior for []byte).
type auditEntry struct {
	ID        string          `json:"id"`
	CompanyID string          `json:"company_id"`
	Actor     string          `json:"actor"`
	Action    string          `json:"action"`
	TargetID  *string         `json:"target_id"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"created_at"`
}

func toAuditEntry(row sqlc.AuditLog) auditEntry {
	detail := row.Detail
	if len(detail) == 0 {
		detail = []byte("{}")
	}
	return auditEntry{
		ID:        row.ID,
		CompanyID: row.CompanyID,
		Actor:     row.Actor,
		Action:    row.Action,
		TargetID:  row.TargetID,
		Detail:    json.RawMessage(detail),
		CreatedAt: row.CreatedAt,
	}
}

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
	entries := make([]auditEntry, len(rows))
	for i, row := range rows {
		entries[i] = toAuditEntry(row)
	}
	return c.JSON(fiber.Map{"entries": entries, "count": len(entries)})
}

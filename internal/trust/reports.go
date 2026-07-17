package trust

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/audit"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// ReportsHandler serves company-to-company abuse reports + the admin queue.
type ReportsHandler struct {
	q       *sqlc.Queries
	auditor *audit.Auditor
}

func NewReportsHandler(q *sqlc.Queries, auditor *audit.Auditor) *ReportsHandler {
	return &ReportsHandler{q: q, auditor: auditor}
}

// Register mounts POST /v1/reports (authed, verified, rate-limited).
func (h *ReportsHandler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Post("/v1/reports", append(append([]fiber.Handler{}, guard...), h.create)...)
}

// RegisterAdmin mounts the admin queue under the platform-admin realm.
func (h *ReportsHandler) RegisterAdmin(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1", adminGuard)
	g.Get("/reports", h.list)
	g.Post("/reports/:id", h.resolve)
}

var validReason = map[string]bool{"spam": true, "fraud": true, "abuse": true, "inappropriate": true, "other": true}

type reportReq struct {
	ReportedCompanyID string `json:"reported_company_id"`
	Reason            string `json:"reason"`
	Detail            string `json:"detail"`
}

func (h *ReportsHandler) create(c *fiber.Ctx) error {
	var req reportReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	me := middleware.CompanyID(c)
	if req.ReportedCompanyID == "" || req.ReportedCompanyID == me {
		return fiber.NewError(fiber.StatusBadRequest, "invalid reported company")
	}
	if !validReason[req.Reason] {
		return fiber.NewError(fiber.StatusBadRequest, "invalid reason")
	}
	var detail *string
	if req.Detail != "" {
		detail = &req.Detail
	}
	rep, err := h.q.CreateReport(c.Context(), sqlc.CreateReportParams{
		ReporterCompanyID: me, ReportedCompanyID: req.ReportedCompanyID, Reason: req.Reason, Detail: detail,
	})
	if err != nil {
		return err
	}
	return c.JSON(rep)
}

func (h *ReportsHandler) list(c *fiber.Ctx) error {
	var status *string
	if s := c.Query("status"); s != "" {
		status = &s
	}
	limit := parseIntDefault(c.Query("limit"), 50, 500)
	offset := parseIntDefault(c.Query("offset"), 0, 1_000_000)
	rows, err := h.q.ListReports(c.Context(), sqlc.ListReportsParams{Status: status, Lim: int32(limit), Off: int32(offset)})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"reports": rows, "count": len(rows)})
}

var validResolution = map[string]bool{"reviewed": true, "dismissed": true, "actioned": true, "open": true}

func (h *ReportsHandler) resolve(c *fiber.Ctx) error {
	var body struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if !validResolution[body.Status] {
		return fiber.NewError(fiber.StatusBadRequest, "invalid status")
	}
	rep, err := h.q.ResolveReport(c.Context(), sqlc.ResolveReportParams{ID: c.Params("id"), Status: body.Status})
	if err != nil {
		return err
	}
	h.auditor.Admin(c.Context(), middleware.AdminEmail(c), "report.resolve", rep.ReportedCompanyID,
		map[string]any{"report_id": rep.ID, "status": rep.Status})
	return c.JSON(rep)
}

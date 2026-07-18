package adminconsole

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/audit"
	"github.com/tradebridge/core/internal/cache"
	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct {
	svc     *Service
	q       *sqlc.Queries
	auditor *audit.Auditor
	cache   *cache.Cache
}

// NewHandler builds the admin console handler. c may be nil — the post-
// suspend/unsuspend cache purge then fails open to a no-op.
func NewHandler(q *sqlc.Queries, ent *entitlements.Service, auditor *audit.Auditor, c *cache.Cache) *Handler {
	return &Handler{svc: NewService(q, ent), q: q, auditor: auditor, cache: c}
}

func (h *Handler) Register(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1/companies", adminGuard)
	g.Get("/", h.search)
	g.Get("/:id", h.detail)
	g.Post("/:id/suspend", h.suspend)
	g.Post("/:id/unsuspend", h.unsuspend)
}

func (h *Handler) search(c *fiber.Ctx) error {
	optional := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	limit := 50
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && n > 0 {
		if n > 200 {
			n = 200
		}
		limit = n
	}
	rows, err := h.q.SearchCompaniesForAdmin(c.Context(), sqlc.SearchCompaniesForAdminParams{
		Q:      optional(c.Query("q")),
		Status: optional(c.Query("status")),
		Lim:    int32(limit),
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"companies": rows, "count": len(rows)})
}

func (h *Handler) detail(c *fiber.Ctx) error {
	d, err := h.svc.Detail(c.Context(), c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "company not found")
	}
	return c.JSON(d)
}

type suspendReq struct {
	Reason string `json:"reason"`
}

func (h *Handler) setStatus(c *fiber.Ctx, status, action string) error {
	id := c.Params("id")
	if _, err := h.q.GetCompanyByID(c.Context(), id); err != nil {
		return fiber.NewError(fiber.StatusNotFound, "company not found")
	}
	var req suspendReq
	_ = c.BodyParser(&req)
	if err := h.q.SetCompanyStatus(c.Context(), sqlc.SetCompanyStatusParams{ID: id, Status: status}); err != nil {
		return err
	}
	// Purge the public storefront cache AFTER the status flip commits: a
	// suspended company must stop serving its (now publicly-excluded)
	// storefront immediately, not just after the cache TTL expires.
	h.cache.Delete(c.Context(), cache.StorefrontKey(id))
	h.auditor.Admin(c.Context(), middleware.AdminEmail(c), action, id, map[string]any{"reason": req.Reason})
	return c.JSON(fiber.Map{"company_id": id, "status": status})
}

func (h *Handler) suspend(c *fiber.Ctx) error   { return h.setStatus(c, "suspended", "company.suspend") }
func (h *Handler) unsuspend(c *fiber.Ctx) error { return h.setStatus(c, "active", "company.unsuspend") }

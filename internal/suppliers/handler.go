// Package suppliers serves public supplier profiles and directory search.
// Search runs on Postgres (ILIKE + trigram indexes) for now; Elasticsearch is
// deferred to Phase 2 (plan §10 infra).
package suppliers

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves supplier profile + directory search routes.
type Handler struct {
	q *sqlc.Queries
}

// NewHandler builds the suppliers handler over the query layer.
func NewHandler(q *sqlc.Queries) *Handler {
	return &Handler{q: q}
}

// Register mounts profile routes (tenant-guarded) and the public search route.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/suppliers", guard...)
	g.Get("/me", h.getMine)
	g.Put("/me", h.upsertMine)
	g.Get("/search", h.search)
}

type profileReq struct {
	Description string   `json:"description"`
	Categories  []string `json:"categories"`
	Location    string   `json:"location"`
}

func (h *Handler) upsertMine(c *fiber.Ctx) error {
	var req profileReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Categories == nil {
		req.Categories = []string{}
	}
	p, err := h.q.UpsertSupplierProfile(c.Context(), sqlc.UpsertSupplierProfileParams{
		CompanyID:   middleware.CompanyID(c),
		Description: &req.Description,
		Categories:  req.Categories,
		Location:    &req.Location,
	})
	if err != nil {
		return err
	}
	return c.JSON(p)
}

func (h *Handler) getMine(c *fiber.Ctx) error {
	p, err := h.q.GetSupplierProfileByCompany(c.Context(), middleware.CompanyID(c))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "no supplier profile yet")
	} else if err != nil {
		return err
	}
	return c.JSON(p)
}

func (h *Handler) search(c *fiber.Ctx) error {
	limit := parseIntDefault(c.Query("limit"), 20, 100)
	offset := parseIntDefault(c.Query("offset"), 0, 1_000_000)

	rows, err := h.q.SearchSuppliers(c.Context(), sqlc.SearchSuppliersParams{
		Query: c.Query("q"),
		Lim:   int32(limit),
		Off:   int32(offset),
	})
	if err != nil {
		return err
	}

	// Hide companies blocked in either direction from the searching company.
	blockedRows, err := h.q.ListBlockCompanyIDsFor(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	if len(blockedRows) > 0 {
		blocked := make(map[string]struct{}, len(blockedRows))
		for _, id := range blockedRows {
			blocked[id] = struct{}{}
		}
		filtered := rows[:0]
		for _, r := range rows {
			if _, b := blocked[r.CompanyID]; !b {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	return c.JSON(fiber.Map{"results": rows, "count": len(rows)})
}

// parseIntDefault parses a positive int, clamping to [0, maxVal] and falling
// back to def on empty/invalid input.
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

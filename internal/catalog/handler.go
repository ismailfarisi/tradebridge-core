// Package catalog manages a company's public catalog summaries and search.
package catalog

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves catalog CRUD + search routes.
type Handler struct {
	q   *sqlc.Queries
	ent *entitlements.Service
}

// NewHandler builds the catalog handler over the query layer.
func NewHandler(q *sqlc.Queries, ent *entitlements.Service) *Handler {
	return &Handler{q: q, ent: ent}
}

// Register mounts the catalog routes under the given tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/catalog", guard...)
	g.Post("/", h.create)
	g.Get("/", h.list)
	g.Get("/search", h.search)
	g.Get("/:id", h.get)
	g.Put("/:id", h.update)
	g.Delete("/:id", h.remove)
}

type catalogReq struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Summary  string `json:"summary"`
}

func (h *Handler) create(c *fiber.Ctx) error {
	var req catalogReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Name == "" || req.Category == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name and category required")
	}
	if err := h.ent.CheckCatalogQuota(c.Context(), middleware.CompanyID(c)); err != nil {
		return entitlements.Fail(c, err)
	}
	item, err := h.q.CreateCatalogSummary(c.Context(), sqlc.CreateCatalogSummaryParams{
		CompanyID: middleware.CompanyID(c),
		Name:      req.Name,
		Category:  req.Category,
		Summary:   &req.Summary,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(item)
}

func (h *Handler) list(c *fiber.Ctx) error {
	items, err := h.q.ListCatalogByCompany(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items, "count": len(items)})
}

// mustOwn fetches a catalog item and enforces tenant ownership.
func (h *Handler) mustOwn(c *fiber.Ctx) (sqlc.CatalogSummary, error) {
	item, err := h.q.GetCatalogSummary(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, fiber.NewError(fiber.StatusNotFound, "catalog item not found")
	} else if err != nil {
		return item, err
	}
	if item.CompanyID != middleware.CompanyID(c) {
		// Don't leak existence across tenants.
		return item, fiber.NewError(fiber.StatusNotFound, "catalog item not found")
	}
	return item, nil
}

func (h *Handler) get(c *fiber.Ctx) error {
	item, err := h.mustOwn(c)
	if err != nil {
		return err
	}
	return c.JSON(item)
}

func (h *Handler) update(c *fiber.Ctx) error {
	if _, err := h.mustOwn(c); err != nil {
		return err
	}
	var req catalogReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Name == "" || req.Category == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name and category required")
	}
	item, err := h.q.UpdateCatalogSummary(c.Context(), sqlc.UpdateCatalogSummaryParams{
		ID:       c.Params("id"),
		Name:     req.Name,
		Category: req.Category,
		Summary:  &req.Summary,
	})
	if err != nil {
		return err
	}
	return c.JSON(item)
}

func (h *Handler) remove(c *fiber.Ctx) error {
	if _, err := h.mustOwn(c); err != nil {
		return err
	}
	if err := h.q.DeleteCatalogSummary(c.Context(), c.Params("id")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) search(c *fiber.Ctx) error {
	rows, err := h.q.SearchCatalog(c.Context(), sqlc.SearchCatalogParams{
		Query: c.Query("q"),
		Lim:   int32(parseIntDefault(c.Query("limit"), 20, 100)),
		Off:   int32(parseIntDefault(c.Query("offset"), 0, 1_000_000)),
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"results": rows, "count": len(rows)})
}

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

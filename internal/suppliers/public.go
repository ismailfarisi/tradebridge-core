package suppliers

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// PublicHandler serves the unauthenticated seller storefront. It exposes only a
// tenant-safe projection (see GetPublicSeller) plus a bounded catalog showcase.
type PublicHandler struct {
	q *sqlc.Queries
}

func NewPublicHandler(q *sqlc.Queries) *PublicHandler {
	return &PublicHandler{q: q}
}

// RegisterPublic mounts the public route with NO tenant guard.
func (h *PublicHandler) RegisterPublic(app *fiber.App) {
	app.Get("/public/v1/sellers/:id", h.getSeller)
}

const storefrontCatalogLimit = 24

type catalogItem struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Summary  *string `json:"summary"`
}

func (h *PublicHandler) getSeller(c *fiber.Ctx) error {
	id := c.Params("id")
	s, err := h.q.GetPublicSeller(c.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "seller not found")
	} else if err != nil {
		return err
	}

	rows, err := h.q.ListCatalogByCompany(c.Context(), id)
	if err != nil {
		return err
	}
	catalog := make([]catalogItem, 0, len(rows))
	for i, r := range rows {
		if i >= storefrontCatalogLimit {
			break
		}
		catalog = append(catalog, catalogItem{Name: r.Name, Category: r.Category, Summary: r.Summary})
	}

	return c.JSON(fiber.Map{
		"company_id":          s.CompanyID,
		"name":                s.Name,
		"verification_status": s.VerificationStatus,
		"verified_domain":     s.VerifiedDomain,
		"description":         s.Description,
		"categories":          s.Categories,
		"location":            s.Location,
		"catalog":             catalog,
		"reputation":          nil, // ratings land Sprint 4
	})
}

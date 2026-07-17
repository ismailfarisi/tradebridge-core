package suppliers

import (
	"errors"
	"time"

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

// RegisterPublic mounts the public routes with NO tenant guard.
func (h *PublicHandler) RegisterPublic(app *fiber.App) {
	app.Get("/public/v1/sellers/:id", h.getSeller)
	app.Get("/public/v1/sellers/:id/reviews", h.getReviews)
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

	rep, err := h.q.GetCompanyReputation(c.Context(), id)
	if err != nil {
		return err
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
		"reputation":          fiber.Map{"avg_rating": rep.AvgRating, "review_count": rep.ReviewCount},
	})
}

// reviewView is the public, safe-fields-only projection of a review.
type reviewView struct {
	ID           string `json:"id"`
	Rating       int32  `json:"rating"`
	Body         string `json:"body"`
	ReviewerName string `json:"reviewer_name"`
	CreatedAt    string `json:"created_at"`
}

// getReviews returns the visible reviews left for a seller, newest first.
func (h *PublicHandler) getReviews(c *fiber.Ctx) error {
	id := c.Params("id")
	rows, err := h.q.ListVisibleReviewsForCompany(c.Context(), id)
	if err != nil {
		return err
	}
	out := make([]reviewView, 0, len(rows))
	for _, r := range rows {
		out = append(out, reviewView{
			ID: r.ID, Rating: r.Rating, Body: r.Body, ReviewerName: r.ReviewerName,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}
	return c.JSON(fiber.Map{"reviews": out})
}

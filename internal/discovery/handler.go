package discovery

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
)

// Handler serves discovery search (public + authenticated share one projection).
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register mounts the guarded route. Call RegisterPublic separately for the
// unauthenticated route (with a rate limiter).
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/discovery", guard...)
	g.Get("/", h.search)
	g.Get("", h.search)
	g.Get("/facets", h.facets)
}

// RegisterPublic mounts the unauthenticated route with the supplied middleware
// (rate limiter) and NO tenant guard.
func (h *Handler) RegisterPublic(app *fiber.App, mw ...fiber.Handler) {
	search := append(append([]fiber.Handler{}, mw...), h.search)
	app.Get("/public/v1/discovery", search...)
	facets := append(append([]fiber.Handler{}, mw...), h.facets)
	app.Get("/public/v1/discovery/facets", facets...)
}

func (h *Handler) search(c *fiber.Ctx) error {
	page, err := h.svc.Search(c.Context(), parseQuery(c))
	if err != nil {
		return err
	}
	return c.JSON(page)
}

func (h *Handler) facets(c *fiber.Ctx) error {
	f, err := h.svc.Facets(c.Context(), parseQuery(c))
	if err != nil {
		return err
	}
	return c.JSON(f)
}

func parseQuery(c *fiber.Ctx) Query {
	return Query{
		Q:            c.Query("q"),
		Category:     c.Query("category"),
		Location:     c.Query("location"),
		VerifiedOnly: c.Query("verified") == "true" || c.Query("verified") == "1",
		MinRating:    parseFloatDefault(c.Query("min_rating"), 0),
		Sort:         c.Query("sort"),
		Page:         parseIntDefault(c.Query("page"), 1, 100000),
		PerPage:      parseIntDefault(c.Query("per_page"), defaultPerPage, maxPerPage),
	}
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

func parseFloatDefault(s string, def float64) float64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n < 0 {
		return def
	}
	return n
}

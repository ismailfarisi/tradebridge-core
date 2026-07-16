package trust

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves seller verification routes.
type Handler struct {
	svc *Service
	q   *sqlc.Queries
}

func NewHandler(svc *Service, q *sqlc.Queries) *Handler {
	return &Handler{svc: svc, q: q}
}

// Register mounts verification routes. Mutating routes require owner/admin role
// and a verified email; check is additionally intended to be rate-limited
// (wired in Plan 3).
func (h *Handler) Register(app *fiber.App, verified fiber.Handler, guard ...fiber.Handler) {
	writeGuard := append(append([]fiber.Handler{}, guard...), verified, middleware.RequireRole("owner", "admin"))
	app.Post("/v1/verification/domain", append(append([]fiber.Handler{}, writeGuard...), h.claim)...)
	app.Post("/v1/verification/domain/check", append(append([]fiber.Handler{}, writeGuard...), h.check)...)
	app.Get("/v1/verification", append(append([]fiber.Handler{}, guard...), h.status)...)
}

type domainReq struct {
	Domain string `json:"domain"`
}

func (h *Handler) claim(c *fiber.Ctx) error {
	var req domainReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	ch, err := h.svc.Claim(c.Context(), middleware.CompanyID(c), req.Domain)
	if errors.Is(err, ErrInvalidDomain) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid domain")
	} else if err != nil {
		return err
	}
	return c.JSON(ch)
}

func (h *Handler) check(c *fiber.Ctx) error {
	var req domainReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	res, err := h.svc.Check(c.Context(), middleware.CompanyID(c), req.Domain)
	switch {
	case errors.Is(err, ErrInvalidDomain):
		return fiber.NewError(fiber.StatusBadRequest, "invalid domain")
	case errors.Is(err, ErrNoClaim):
		return fiber.NewError(fiber.StatusNotFound, "no pending verification for this domain")
	case errors.Is(err, ErrDomainTaken):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"code": "domain_taken", "error": "domain already verified by another company"})
	case err != nil:
		return err
	}
	return c.JSON(res)
}

func (h *Handler) status(c *fiber.Ctx) error {
	cid := middleware.CompanyID(c)
	v, err := h.q.GetCompanyVerification(c.Context(), cid)
	if err != nil {
		return err
	}
	pending, err := h.q.ListPendingVerificationByCompany(c.Context(), cid)
	if err != nil {
		return err
	}
	claims := make([]fiber.Map, 0, len(pending))
	for _, p := range pending {
		claims = append(claims, fiber.Map{
			"domain":       p.Domain,
			"record_name":  recordHost + p.Domain,
			"record_type":  "TXT",
			"record_value": p.Token,
		})
	}
	return c.JSON(fiber.Map{
		"verification_status": v.VerificationStatus,
		"verified_domain":     v.VerifiedDomain,
		"verified_at":         v.VerifiedAt,
		"pending":             claims,
	})
}

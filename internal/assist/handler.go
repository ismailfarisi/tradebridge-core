package assist

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the buyer AI assist routes: sharpening an RFQ draft and
// explaining a quote comparison. Both are metered against the company's
// assist quota.
type Handler struct {
	svc *Service
	ent *entitlements.Service
	q   *sqlc.Queries
}

// NewHandler builds the assist handler.
func NewHandler(svc *Service, ent *entitlements.Service, q *sqlc.Queries) *Handler {
	return &Handler{svc: svc, ent: ent, q: q}
}

// Register mounts /v1/assist routes under the tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/assist", guard...)
	g.Post("/rfq", h.improveRFQ)
	g.Post("/compare", h.compare)
}

type improveRFQReq struct {
	Draft string `json:"draft"`
}

func (h *Handler) improveRFQ(c *fiber.Ctx) error {
	var req improveRFQReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	me := middleware.CompanyID(c)
	if err := h.ent.CheckAssistQuota(c.Context(), me); err != nil {
		return entitlements.Fail(c, err)
	}
	out, err := h.svc.ImproveRFQ(c.Context(), req.Draft)
	if err != nil {
		return err
	}
	if out != "" {
		h.ent.IncrementAssistUsage(c.Context(), me)
	}
	return c.JSON(fiber.Map{"suggestion": out})
}

type compareReq struct {
	RFQID string `json:"rfq_id"`
}

func (h *Handler) compare(c *fiber.Ctx) error {
	var req compareReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	me := middleware.CompanyID(c)

	r, err := h.q.GetRFQ(c.Context(), req.RFQID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "rfq not found")
	} else if err != nil {
		return err
	}
	// Buyer-only: a non-buyer (including a quoting seller) gets 404, not 403,
	// so the RFQ's existence isn't leaked to parties who shouldn't see it.
	if r.BuyerCompanyID != me {
		return fiber.NewError(fiber.StatusNotFound, "rfq not found")
	}

	rows, err := h.q.RankQuotesByRFQ(c.Context(), r.ID)
	if err != nil {
		return err
	}
	// No quotes yet: skip the quota check/LLM call entirely and return a
	// friendly message without counting against the company's quota.
	if len(rows) == 0 {
		out, _ := h.svc.ExplainQuotes(c.Context(), r.Title, nil)
		return c.JSON(fiber.Map{"explanation": out})
	}

	if err := h.ent.CheckAssistQuota(c.Context(), me); err != nil {
		return entitlements.Fail(c, err)
	}

	summaries := make([]QuoteSummary, len(rows))
	for i, row := range rows {
		summaries[i] = QuoteSummary{
			SellerName:   row.SellerName,
			PriceCents:   row.PriceCents,
			Currency:     row.Currency,
			DeliveryDays: row.DeliveryDays,
			ReviewCount:  row.SellerReviewCount,
			AvgRating:    row.SellerAvgRating,
			Verified:     row.VerificationStatus == "verified",
		}
	}

	out, err := h.svc.ExplainQuotes(c.Context(), r.Title, summaries)
	if err != nil {
		return err
	}
	if out != "" {
		h.ent.IncrementAssistUsage(c.Context(), me)
	}
	return c.JSON(fiber.Map{"explanation": out})
}

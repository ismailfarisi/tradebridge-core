package rfq

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// sellerPageSize caps one page of the seller RFQ inbox.
const sellerPageSize = 50

// SellerHandler serves the seller-facing RFQ inbox: the RFQs Core matched to
// the caller's company. It is the web counterpart of the rfq.matched events a
// seller agent receives over the Agent Gateway. Visibility is decided by
// rfq_matches alone, so a seller only ever sees RFQs it was matched to.
type SellerHandler struct {
	q *sqlc.Queries
}

// NewSellerHandler builds the seller RFQ handler over the query layer.
func NewSellerHandler(q *sqlc.Queries) *SellerHandler {
	return &SellerHandler{q: q}
}

// Register mounts the seller RFQ routes under the tenant guard.
func (h *SellerHandler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/seller/rfqs", guard...)
	g.Get("/", h.list)
	g.Get("/:id", h.get)
}

// sellerQuoteSummary is the caller's own quote on an RFQ, if any.
type sellerQuoteSummary struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	PriceCents   int64  `json:"price_cents"`
	Currency     string `json:"currency"`
	DeliveryDays *int32 `json:"delivery_days"`
	Notes        string `json:"notes,omitempty"`
	Version      int32  `json:"version,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

// sellerRFQView is the RFQ as a matched seller sees it. It carries the same
// information as the rfq.matched agent event (requirements, score, reason)
// and never the buyer's intake chat.
type sellerRFQView struct {
	ID           string              `json:"id"`
	Title        string              `json:"title"`
	RawText      string              `json:"raw_text"`
	Requirements Requirements        `json:"requirements"`
	Category     string              `json:"category"`
	Status       string              `json:"status"`
	CreatedAt    string              `json:"created_at"`
	ExpiresAt    *string             `json:"expires_at"`
	MatchScore   float64             `json:"match_score"`
	MatchReason  string              `json:"match_reason"`
	MatchedAt    string              `json:"matched_at"`
	Unseen       bool                `json:"unseen,omitempty"`
	Quote        *sellerQuoteSummary `json:"quote"`
}

var sellerTabs = map[string]bool{"": true, "open": true, "quoted": true, "closed": true}

func (h *SellerHandler) list(c *fiber.Ctx) error {
	tab := c.Query("tab")
	if !sellerTabs[tab] {
		return fiber.NewError(fiber.StatusBadRequest, "tab must be one of open|quoted|closed")
	}
	off := 0
	if s := c.Query("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return fiber.NewError(fiber.StatusBadRequest, "offset must be a non-negative integer")
		}
		off = n
	}
	rows, err := h.q.ListSellerRFQs(c.Context(), sqlc.ListSellerRFQsParams{
		CompanyID: middleware.CompanyID(c),
		Tab:       tab,
		Q:         c.Query("q"),
		Lim:       sellerPageSize,
		Off:       int32(off),
	})
	if err != nil {
		return err
	}
	out := make([]sellerRFQView, 0, len(rows))
	for _, r := range rows {
		v := sellerRFQView{
			ID:           r.ID,
			Title:        r.Title,
			RawText:      r.RawText,
			Requirements: decodeRequirements(r.Requirements),
			Category:     r.Category,
			Status:       r.Status,
			CreatedAt:    formatTime(r.CreatedAt),
			ExpiresAt:    formatTimestamptz(r.ExpiresAt),
			MatchScore:   r.Score,
			MatchReason:  r.Reason,
			MatchedAt:    formatTime(r.MatchedAt),
			Unseen:       r.Unseen,
		}
		if r.QuoteID != nil {
			v.Quote = &sellerQuoteSummary{
				ID:         *r.QuoteID,
				Status:     deref(r.QuoteStatus),
				PriceCents: derefInt(r.QuotePriceCents),
				Currency:   deref(r.QuoteCurrency),
			}
		}
		out = append(out, v)
	}
	return c.JSON(fiber.Map{"rfqs": out, "count": len(out)})
}

func (h *SellerHandler) get(c *fiber.Ctx) error {
	ctx := c.Context()
	me := middleware.CompanyID(c)
	r, err := h.q.GetSellerRFQ(ctx, sqlc.GetSellerRFQParams{RfqID: c.Params("id"), CompanyID: me})
	if errors.Is(err, pgx.ErrNoRows) {
		// Unmatched and nonexistent look the same so existence never leaks.
		return fiber.NewError(fiber.StatusNotFound, "rfq not found")
	} else if err != nil {
		return err
	}
	// Best-effort: clears the "new" flag in the seller's list.
	_ = h.q.UpsertRFQView(ctx, sqlc.UpsertRFQViewParams{CompanyID: me, RfqID: r.ID})

	v := sellerRFQView{
		ID:           r.ID,
		Title:        r.Title,
		RawText:      r.RawText,
		Requirements: decodeRequirements(r.Requirements),
		Category:     r.Category,
		Status:       r.Status,
		CreatedAt:    formatTime(r.CreatedAt),
		ExpiresAt:    formatTimestamptz(r.ExpiresAt),
		MatchScore:   r.Score,
		MatchReason:  r.Reason,
		MatchedAt:    formatTime(r.MatchedAt),
	}
	q, err := h.q.GetQuoteBySeller(ctx, sqlc.GetQuoteBySellerParams{RfqID: r.ID, SellerCompanyID: me})
	switch {
	case err == nil:
		v.Quote = &sellerQuoteSummary{
			ID:           q.ID,
			Status:       q.Status,
			PriceCents:   q.PriceCents,
			Currency:     q.Currency,
			DeliveryDays: q.DeliveryDays,
			Notes:        q.Notes,
			Version:      q.Version,
			UpdatedAt:    formatTime(q.UpdatedAt),
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}

	atts, err := h.q.ListBuyerAttachmentsByRFQ(ctx, &r.ID)
	if err != nil {
		return err
	}
	metas := make([]attachments.Meta, len(atts))
	for i, a := range atts {
		metas[i] = attachments.MetaOf(a)
	}
	return c.JSON(fiber.Map{"rfq": v, "attachments": metas})
}

func decodeRequirements(raw []byte) Requirements {
	var req Requirements
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	return req
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func formatTimestamptz(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := formatTime(t.Time)
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}

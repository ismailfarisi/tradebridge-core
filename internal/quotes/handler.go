package quotes

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves buyer + seller quote routes.
type Handler struct {
	svc *Service
	q   *sqlc.Queries
}

// NewHandler builds the quotes handler.
func NewHandler(svc *Service, q *sqlc.Queries) *Handler {
	return &Handler{svc: svc, q: q}
}

// Register mounts quote routes under the tenant guard. messageRL rate-limits
// the add-message route.
func (h *Handler) Register(app *fiber.App, messageRL fiber.Handler, guard ...fiber.Handler) {
	app.Add("GET", "/v1/rfqs/:id/quotes", append(guard, h.listForRFQ)...)
	g := app.Group("/v1/quotes", guard...)
	g.Get("/:id", h.get)
	g.Post("/:id/accept", h.accept)
	g.Get("/:id/messages", h.listMessages)
	g.Post("/:id/messages", messageRL, h.addMessage)
}

type quoteView struct {
	ID                       string  `json:"id"`
	RFQID                    string  `json:"rfq_id"`
	SellerID                 string  `json:"seller_company_id"`
	SellerName               string  `json:"seller_name,omitempty"`
	SellerVerificationStatus string  `json:"seller_verification_status,omitempty"`
	PriceCents               int64   `json:"price_cents"`
	Currency                 string  `json:"currency"`
	DeliveryDays             *int32  `json:"delivery_days"`
	Notes                    string  `json:"notes"`
	Completeness             float64 `json:"completeness"`
	Status                   string  `json:"status"`
	Rank                     int     `json:"rank,omitempty"`
}

// listForRFQ returns the buyer's ranked quotes for one RFQ.
func (h *Handler) listForRFQ(c *fiber.Ctx) error {
	r, err := h.q.GetRFQ(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "rfq not found")
	} else if err != nil {
		return err
	}
	if r.BuyerCompanyID != middleware.CompanyID(c) {
		return fiber.NewError(fiber.StatusNotFound, "rfq not found")
	}
	rows, err := h.q.RankQuotesByRFQ(c.Context(), r.ID)
	if err != nil {
		return err
	}
	out := make([]quoteView, len(rows))
	for i, q := range rows {
		out[i] = quoteView{
			ID: q.ID, RFQID: q.RfqID, SellerID: q.SellerCompanyID, SellerName: q.SellerName,
			SellerVerificationStatus: q.VerificationStatus,
			PriceCents:               q.PriceCents, Currency: q.Currency, DeliveryDays: q.DeliveryDays,
			Notes: q.Notes, Completeness: q.Completeness, Status: q.Status, Rank: i + 1,
		}
	}
	return c.JSON(fiber.Map{"quotes": out, "count": len(out)})
}

// mustParty loads a quote and confirms the caller is a party to it (the buyer
// on the RFQ or the seller who submitted it), returning 404 otherwise.
func (h *Handler) mustParty(c *fiber.Ctx) (sqlc.Quote, error) {
	q, err := h.q.GetQuote(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return q, fiber.NewError(fiber.StatusNotFound, "quote not found")
	} else if err != nil {
		return q, err
	}
	me := middleware.CompanyID(c)
	if q.SellerCompanyID == me {
		return q, nil
	}
	r, err := h.q.GetRFQ(c.Context(), q.RfqID)
	if err != nil {
		return q, err
	}
	if r.BuyerCompanyID != me {
		return q, fiber.NewError(fiber.StatusNotFound, "quote not found")
	}
	return q, nil
}

func (h *Handler) get(c *fiber.Ctx) error {
	q, err := h.mustParty(c)
	if err != nil {
		return err
	}
	return c.JSON(quoteView{
		ID: q.ID, RFQID: q.RfqID, SellerID: q.SellerCompanyID,
		PriceCents: q.PriceCents, Currency: q.Currency, DeliveryDays: q.DeliveryDays,
		Notes: q.Notes, Completeness: q.Completeness, Status: q.Status,
	})
}

func (h *Handler) accept(c *fiber.Ctx) error {
	order, err := h.svc.Accept(c.Context(), middleware.CompanyID(c), c.Params("id"))
	if errors.Is(err, ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, "quote not found")
	}
	var conflict ConflictError
	if errors.As(err, &conflict) {
		return fiber.NewError(fiber.StatusConflict, conflict.Msg)
	}
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"order_id":    order.ID,
		"rfq_id":      order.RfqID,
		"quote_id":    order.QuoteID,
		"total_cents": order.TotalCents,
		"currency":    order.Currency,
		"status":      order.Status,
	})
}

type messageReq struct {
	Body          string   `json:"body"`
	AttachmentIDs []string `json:"attachment_ids"`
}

// quoteMessageView renders a quote_messages row for the API, decoding the
// JSONB attachments column into a real JSON array — sqlc types it as []byte,
// which encoding/json would otherwise marshal as a base64 string.
type quoteMessageView struct {
	ID          string          `json:"id"`
	QuoteID     string          `json:"quote_id"`
	Author      string          `json:"author"`
	Body        string          `json:"body"`
	Attachments json.RawMessage `json:"attachments"`
	CreatedAt   time.Time       `json:"created_at"`
}

func messageView(m sqlc.QuoteMessage) quoteMessageView {
	v := quoteMessageView{
		ID: m.ID, QuoteID: m.QuoteID, Author: m.Author, Body: m.Body, CreatedAt: m.CreatedAt,
	}
	if len(m.Attachments) > 0 {
		v.Attachments = json.RawMessage(m.Attachments)
	}
	return v
}

func (h *Handler) addMessage(c *fiber.Ctx) error {
	q, err := h.mustParty(c)
	if err != nil {
		return err
	}
	var req messageReq
	if err := c.BodyParser(&req); err != nil || (req.Body == "" && len(req.AttachmentIDs) == 0) {
		return fiber.NewError(fiber.StatusBadRequest, "body or attachments required")
	}
	if len(req.AttachmentIDs) > attachments.MaxPerMessage {
		return fiber.NewError(fiber.StatusBadRequest, "too many attachments (max 5 per message)")
	}
	msg, err := h.svc.AddMessage(c.Context(), q, middleware.CompanyID(c), req.Body, req.AttachmentIDs)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(messageView(msg))
}

func (h *Handler) listMessages(c *fiber.Ctx) error {
	q, err := h.mustParty(c)
	if err != nil {
		return err
	}
	msgs, err := h.q.ListQuoteMessages(c.Context(), q.ID)
	if err != nil {
		return err
	}
	views := make([]quoteMessageView, len(msgs))
	for i, m := range msgs {
		views[i] = messageView(m)
	}
	return c.JSON(fiber.Map{"messages": views, "count": len(views)})
}

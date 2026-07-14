package rfq

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/valyala/fasthttp"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the buyer-facing RFQ routes.
type Handler struct {
	svc *Service
	q   *sqlc.Queries
}

// NewHandler builds the RFQ handler over the service and query layer.
func NewHandler(svc *Service, q *sqlc.Queries) *Handler {
	return &Handler{svc: svc, q: q}
}

// Register mounts the RFQ routes under the given tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/rfqs", guard...)
	g.Post("/", h.create)
	g.Get("/", h.list)
	g.Get("/:id", h.get)
	g.Post("/:id/close", h.close)

	app.Post("/v1/rfq-chat", append(append([]fiber.Handler{}, guard...), h.chat)...)
	app.Post("/v1/rfq-chat/stream", append(append([]fiber.Handler{}, guard...), h.chatStream)...)
}

type createReq struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// rfqView is the JSON shape returned to buyers, with requirements decoded.
type rfqView struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	RawText      string        `json:"raw_text"`
	Requirements Requirements  `json:"requirements"`
	Category     string        `json:"category"`
	Status       string        `json:"status"`
	MatchCount   int32         `json:"match_count"`
	Messages     []ChatMessage `json:"messages,omitempty"`
	CreatedAt    string        `json:"created_at"`
}

func view(r sqlc.Rfq) rfqView {
	var req Requirements
	if len(r.Requirements) > 0 {
		_ = json.Unmarshal(r.Requirements, &req)
	}
	var msgs []ChatMessage
	if len(r.Messages) > 0 {
		_ = json.Unmarshal(r.Messages, &msgs)
	}
	return rfqView{
		ID:           r.ID,
		Title:        r.Title,
		RawText:      r.RawText,
		Requirements: req,
		Category:     r.Category,
		Status:       r.Status,
		MatchCount:   r.MatchCount,
		Messages:     msgs,
		CreatedAt:    r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func (h *Handler) create(c *fiber.Ctx) error {
	var req createReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Title == "" || req.Text == "" {
		return fiber.NewError(fiber.StatusBadRequest, "title and text required")
	}

	rfq, matches, err := h.svc.CreateAndMatch(
		c.Context(),
		middleware.CompanyID(c),
		middleware.UserID(c),
		req.Title,
		req.Text,
	)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"rfq":         view(rfq),
		"match_count": len(matches),
		"matches":     matches,
	})
}

func (h *Handler) list(c *fiber.Ctx) error {
	rfqs, err := h.q.ListRFQsByBuyer(c.Context(), sqlc.ListRFQsByBuyerParams{
		BuyerCompanyID: middleware.CompanyID(c),
		Lim:            50,
		Off:            0,
	})
	if err != nil {
		return err
	}
	out := make([]rfqView, len(rfqs))
	for i, r := range rfqs {
		out[i] = view(r)
	}
	return c.JSON(fiber.Map{"rfqs": out, "count": len(out)})
}

// mustOwn fetches an RFQ and enforces buyer-tenant ownership, returning 404
// across tenants so existence never leaks.
func (h *Handler) mustOwn(c *fiber.Ctx) (sqlc.Rfq, error) {
	rfq, err := h.q.GetRFQ(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return rfq, fiber.NewError(fiber.StatusNotFound, "rfq not found")
	} else if err != nil {
		return rfq, err
	}
	if rfq.BuyerCompanyID != middleware.CompanyID(c) {
		return rfq, fiber.NewError(fiber.StatusNotFound, "rfq not found")
	}
	return rfq, nil
}

func (h *Handler) get(c *fiber.Ctx) error {
	rfq, err := h.mustOwn(c)
	if err != nil {
		return err
	}
	rows, err := h.q.ListMatchesByRFQ(c.Context(), rfq.ID)
	if err != nil {
		return err
	}
	matches := make([]MatchResult, len(rows))
	for i, r := range rows {
		matches[i] = MatchResult{
			SellerCompanyID: r.SellerCompanyID,
			SellerName:      r.SellerName,
			Score:           r.Score,
			Reason:          r.Reason,
		}
	}
	return c.JSON(fiber.Map{
		"rfq":         view(rfq),
		"match_count": len(matches),
		"matches":     matches,
	})
}

type chatReq struct {
	Messages []ChatMessage `json:"messages"`
}

func (h *Handler) chat(c *fiber.Ctx) error {
	var req chatReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if len(req.Messages) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "messages required")
	}
	res, err := h.svc.Chat(c.Context(), middleware.CompanyID(c), middleware.UserID(c), req.Messages)
	if err != nil {
		return err
	}
	out := fiber.Map{"done": res.Done, "assistant": res.Assistant}
	if res.Done && res.RFQ != nil {
		out["rfq"] = view(*res.RFQ)
		out["match_count"] = res.MatchCount
	}
	return c.JSON(out)
}

// chatStream is the SSE variant of chat: assistant text arrives as `delta`
// events while the model generates, then one `done` event carries the same
// payload the non-streaming endpoint returns.
func (h *Handler) chatStream(c *fiber.Ctx) error {
	var req chatReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if len(req.Messages) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "messages required")
	}
	companyID, userID := middleware.CompanyID(c), middleware.UserID(c)

	c.Set(fiber.HeaderContentType, "text/event-stream")
	c.Set(fiber.HeaderCacheControl, "no-cache")
	c.Set("X-Accel-Buffering", "no")

	// The fasthttp ctx outlives this handler and is canceled on disconnect,
	// so it is the right context for the model call inside the writer.
	ctx := c.Context()
	ctx.SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		emit := func(event string, payload any) {
			data, err := json.Marshal(payload)
			if err != nil {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			_ = w.Flush()
		}
		res, err := h.svc.ChatStream(ctx, companyID, userID, req.Messages, func(text string) {
			emit("delta", fiber.Map{"text": text})
		})
		if err != nil {
			emit("error", fiber.Map{"message": err.Error()})
			return
		}
		out := fiber.Map{"done": res.Done, "assistant": res.Assistant}
		if res.Done && res.RFQ != nil {
			out["rfq"] = view(*res.RFQ)
			out["match_count"] = res.MatchCount
		}
		emit("done", out)
	}))
	return nil
}

func (h *Handler) close(c *fiber.Ctx) error {
	rfq, err := h.mustOwn(c)
	if err != nil {
		return err
	}
	if err := ValidateTransition(rfq.Status, StatusClosed); err != nil {
		return fiber.NewError(fiber.StatusConflict, err.Error())
	}
	updated, err := h.q.SetRFQStatus(c.Context(), sqlc.SetRFQStatusParams{
		ID:     rfq.ID,
		Status: StatusClosed,
	})
	if err != nil {
		return err
	}
	return c.JSON(view(updated))
}

package feedback

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

type Handler struct{ q *sqlc.Queries }

func NewHandler(q *sqlc.Queries) *Handler { return &Handler{q: q} }

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Post("/v1/feedback", append(append([]fiber.Handler{}, guard...), h.create)...)
}

func (h *Handler) RegisterAdmin(app *fiber.App, adminGuard fiber.Handler) {
	app.Get("/admin/v1/feedback", adminGuard, h.list)
}

type createReq struct {
	Kind    string `json:"kind"`
	Score   *int32 `json:"score"`
	Message string `json:"message"`
}

func (h *Handler) create(c *fiber.Ctx) error {
	var req createReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	switch req.Kind {
	case "nps":
		if req.Score == nil || *req.Score < 0 || *req.Score > 10 {
			return fiber.NewError(fiber.StatusBadRequest, "score 0-10 required for nps")
		}
	case "feedback":
		if req.Message == "" {
			return fiber.NewError(fiber.StatusBadRequest, "message required for feedback")
		}
	default:
		return fiber.NewError(fiber.StatusBadRequest, "kind must be feedback or nps")
	}
	var msg *string
	if req.Message != "" {
		msg = &req.Message
	}
	row, err := h.q.CreateFeedback(c.Context(), sqlc.CreateFeedbackParams{
		CompanyID: middleware.CompanyID(c),
		UserID:    middleware.UserID(c),
		Kind:      req.Kind,
		Score:     req.Score,
		Message:   msg,
	})
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(row)
}

func (h *Handler) list(c *fiber.Ctx) error {
	rows, err := h.q.ListFeedback(c.Context(), 500)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"feedback": rows, "count": len(rows)})
}

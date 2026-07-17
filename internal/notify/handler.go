package notify

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the in-app notification inbox and per-event-type delivery
// preferences over HTTP.
type Handler struct {
	q *sqlc.Queries
}

// NewHandler builds the notify handler over the query layer.
func NewHandler(q *sqlc.Queries) *Handler {
	return &Handler{q: q}
}

// Register mounts the notification inbox and preferences routes. read-all is
// registered before the :id route so it isn't shadowed by it.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/notifications", guard...)
	g.Get("/", h.list)
	g.Post("/read-all", h.readAll)
	g.Post("/:id/read", h.markRead)
	g.Get("/preferences", h.getPreferences)
	g.Put("/preferences", h.putPreference)
}

// notificationView is the wire representation of a single notification.
type notificationView struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Data      json.RawMessage `json:"data,omitempty"`
	Read      bool            `json:"read"`
	CreatedAt string          `json:"created_at"`
}

// list returns a page of the caller's notifications plus their unread count.
func (h *Handler) list(c *fiber.Ctx) error {
	userID := middleware.UserID(c)
	limit := clampInt(c.QueryInt("limit", 30), 1, 100)
	offset := c.QueryInt("offset", 0)
	if offset < 0 {
		offset = 0
	}

	rows, err := h.q.ListNotificationsByUser(c.Context(), sqlc.ListNotificationsByUserParams{
		UserID: userID,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return err
	}
	unread, err := h.q.CountUnreadNotifications(c.Context(), userID)
	if err != nil {
		return err
	}

	out := make([]notificationView, 0, len(rows))
	for _, r := range rows {
		out = append(out, notificationView{
			ID:        r.ID,
			Type:      r.Type,
			Title:     r.Title,
			Body:      r.Body,
			Data:      json.RawMessage(r.Data),
			Read:      r.ReadAt.Valid,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}

	return c.JSON(fiber.Map{"notifications": out, "unread": unread})
}

// markRead marks a single notification, scoped to the caller, as read.
func (h *Handler) markRead(c *fiber.Ctx) error {
	if err := h.q.MarkNotificationRead(c.Context(), sqlc.MarkNotificationReadParams{
		ID:     c.Params("id"),
		UserID: middleware.UserID(c),
	}); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// readAll marks every unread notification belonging to the caller as read.
func (h *Handler) readAll(c *fiber.Ctx) error {
	if err := h.q.MarkAllNotificationsRead(c.Context(), middleware.UserID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// preferenceView is the wire representation of a per-event-type preference.
type preferenceView struct {
	EventType string `json:"event_type"`
	Email     bool   `json:"email"`
	InApp     bool   `json:"in_app"`
}

// getPreferences returns the caller's notification preferences.
func (h *Handler) getPreferences(c *fiber.Ctx) error {
	rows, err := h.q.GetNotificationPreferences(c.Context(), middleware.UserID(c))
	if err != nil {
		return err
	}
	out := make([]preferenceView, 0, len(rows))
	for _, r := range rows {
		out = append(out, preferenceView{EventType: r.EventType, Email: r.Email, InApp: r.InApp})
	}
	return c.JSON(fiber.Map{"preferences": out})
}

type putPreferenceReq struct {
	EventType string `json:"event_type"`
	Email     bool   `json:"email"`
	InApp     bool   `json:"in_app"`
}

// putPreference upserts the caller's preference for a single event type.
func (h *Handler) putPreference(c *fiber.Ctx) error {
	var req putPreferenceReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if !validEventType(req.EventType) {
		return fiber.NewError(fiber.StatusBadRequest, "unknown event type")
	}

	pref, err := h.q.UpsertNotificationPreference(c.Context(), sqlc.UpsertNotificationPreferenceParams{
		UserID:    middleware.UserID(c),
		EventType: req.EventType,
		Email:     req.Email,
		InApp:     req.InApp,
	})
	if err != nil {
		return err
	}
	return c.JSON(preferenceView{EventType: pref.EventType, Email: pref.Email, InApp: pref.InApp})
}

// clampInt clamps v to [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// validEventType reports whether t is a user-configurable event type.
func validEventType(t string) bool {
	switch t {
	case TypeRFQMatched, TypeQuoteSubmitted, TypeOrderCreated,
		TypeOrderConfirmed, TypeOrderFulfilled, TypeOrderCompleted, TypeOrderCancelled:
		return true
	default:
		return false
	}
}

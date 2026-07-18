package waitlist

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// AdminHandler exposes the platform-admin waitlist surface: listing signups
// and sending launch invites. Registered only inside the admin realm (see
// httpapi.NewRouter's cfg.AdminJWTSecret block).
type AdminHandler struct {
	q       *sqlc.Queries
	sender  notify.Sender
	baseURL string
	logger  zerolog.Logger
}

// NewAdminHandler builds the waitlist admin handler.
func NewAdminHandler(q *sqlc.Queries, sender notify.Sender, baseURL string, logger zerolog.Logger) *AdminHandler {
	return &AdminHandler{q: q, sender: sender, baseURL: baseURL, logger: logger}
}

// Register mounts the admin waitlist routes behind adminGuard.
func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1/waitlist", adminGuard)
	g.Get("/", h.list)
	g.Post("/invite", h.invite)
}

func (h *AdminHandler) list(c *fiber.Ctx) error {
	var only *string
	if s := c.Query("status"); s == "pending" || s == "invited" {
		only = &s
	}
	limit := 500
	if n, err := strconv.Atoi(c.Query("limit")); err == nil && n > 0 && n <= 2000 {
		limit = n
	}
	rows, err := h.q.ListWaitlist(c.Context(), sqlc.ListWaitlistParams{StatusFilter: only, Lim: int32(limit)})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"signups": rows, "count": len(rows)})
}

type inviteReq struct {
	IDs []string `json:"ids"`
}

type inviteTarget struct {
	ID    string
	Email string
}

func (h *AdminHandler) invite(c *fiber.Ctx) error {
	var req inviteReq
	_ = c.BodyParser(&req)

	var targets []inviteTarget
	if len(req.IDs) == 0 {
		rows, err := h.q.ListPendingWaitlistEmails(c.Context())
		if err != nil {
			return err
		}
		for _, r := range rows {
			targets = append(targets, inviteTarget{ID: r.ID, Email: r.Email})
		}
	} else {
		rows, err := h.q.GetWaitlistByIDs(c.Context(), req.IDs)
		if err != nil {
			return err
		}
		for _, r := range rows {
			targets = append(targets, inviteTarget{ID: r.ID, Email: r.Email})
		}
	}

	sent := 0
	ids := make([]string, 0, len(targets))
	for _, t := range targets {
		email := notify.Email{
			To:      []string{t.Email},
			Subject: "You're invited to TradeBridge",
			HTML:    `<p>TradeBridge is open. <a href="` + h.baseURL + `/register">Create your account</a>.</p>`,
			Text:    "TradeBridge is open. Create your account: " + h.baseURL + "/register",
		}
		if err := h.sender.Send(c.Context(), email); err != nil {
			// Per-recipient send failures are logged, not audited: a waitlist
			// signup has no company_id, and audit_logs.company_id is NOT
			// NULL. The row stays un-invited so a retry (next admin run)
			// picks it back up — see ListPendingWaitlistEmails above.
			h.logger.Warn().Err(err).Str("email", t.Email).Msg("waitlist invite send failed")
			continue
		}
		sent++
		ids = append(ids, t.ID)
	}

	if len(ids) > 0 {
		if err := h.q.MarkWaitlistInvited(c.Context(), ids); err != nil {
			return err
		}
	}

	return c.JSON(fiber.Map{"sent": sent, "attempted": len(targets)})
}

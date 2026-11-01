package negotiation

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// maxPauseReason bounds the free-text reason stored with a kill switch.
const maxPauseReason = 500

// pauseView is the kill-switch state as the buyer UI renders it.
type pauseView struct {
	PauseState
	Paused   bool   `json:"paused"`
	PausedBy string `json:"paused_by"` // "platform" | "company" | ""
	Message  string `json:"message"`
}

func viewOf(p PauseState) pauseView {
	return pauseView{PauseState: p, Paused: p.Paused(), PausedBy: p.PausedBy(), Message: p.Message()}
}

// SetCompanyPause engages or releases a buyer company's kill switch and
// appends the flip to the kill-switch log and the company audit trail, all in
// one transaction.
func (e *Engine) SetCompanyPause(ctx context.Context, companyID, userID string, paused bool, reason string) (PauseState, error) {
	var by *string
	if userID != "" {
		by = &userID
	}
	err := e.inTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.UpsertCompanyNegotiationControl(ctx, sqlc.UpsertCompanyNegotiationControlParams{
			CompanyID: companyID, Paused: paused, Reason: reason, UpdatedBy: by,
		}); err != nil {
			return err
		}
		actor := "user:" + userID
		if _, err := q.InsertNegotiationKillSwitchLog(ctx, sqlc.InsertNegotiationKillSwitchLogParams{
			Scope: "company", CompanyID: &companyID, Paused: paused, Reason: reason, Actor: actor,
		}); err != nil {
			return err
		}
		action := "negotiation.company_resume"
		if paused {
			action = "negotiation.company_pause"
		}
		detail, err := json.Marshal(map[string]any{"paused": paused, "reason": reason})
		if err != nil {
			return err
		}
		_, err = q.CreateAuditLog(ctx, sqlc.CreateAuditLogParams{CompanyID: companyID, Actor: actor, Action: action, TargetID: &companyID, Detail: detail})
		return err
	})
	if err != nil {
		return PauseState{}, err
	}
	e.logger.Info().Str("company_id", companyID).Str("user_id", userID).Bool("paused", paused).Str("reason", reason).Msg("negotiation: company kill switch")
	return e.PauseState(ctx, companyID)
}

// SetPlatformPause engages or releases the platform-wide kill switch. The
// UPDATE waits for engine move transactions that already read the switch
// under FOR SHARE, so once this returns no further counter can commit.
func (e *Engine) SetPlatformPause(ctx context.Context, adminEmail string, paused bool, reason string) (sqlc.NegotiationPlatformControl, error) {
	var out sqlc.NegotiationPlatformControl
	actor := "admin:" + adminEmail
	err := e.inTx(ctx, func(q *sqlc.Queries) error {
		var err error
		if out, err = q.SetPlatformNegotiationControl(ctx, sqlc.SetPlatformNegotiationControlParams{Paused: paused, Reason: reason, UpdatedBy: actor}); err != nil {
			return err
		}
		_, err = q.InsertNegotiationKillSwitchLog(ctx, sqlc.InsertNegotiationKillSwitchLogParams{Scope: "platform", Paused: paused, Reason: reason, Actor: actor})
		return err
	})
	if err != nil {
		return out, err
	}
	// Platform actions have no target company, so they cannot go to the
	// per-company audit_logs; the kill-switch log is their audit trail.
	e.logger.Warn().Str("admin", adminEmail).Bool("paused", paused).Str("reason", reason).Msg("negotiation: platform kill switch")
	return out, nil
}

func (e *Engine) inTx(ctx context.Context, fn func(q *sqlc.Queries) error) error {
	if e.pool == nil {
		return fn(e.q)
	}
	return pgx.BeginFunc(ctx, e.pool, func(tx pgx.Tx) error { return fn(e.q.WithTx(tx)) })
}

type pauseInput struct {
	Paused *bool  `json:"paused"`
	Reason string `json:"reason"`
}

func (in *pauseInput) parse(c *fiber.Ctx) error {
	if err := c.BodyParser(in); err != nil || in.Paused == nil {
		return fiber.NewError(fiber.StatusBadRequest, "body must include paused (boolean)")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if len(in.Reason) > maxPauseReason {
		return fiber.NewError(fiber.StatusBadRequest, "reason is too long")
	}
	return nil
}

// getControls returns the caller company's effective kill-switch state.
func (h *Handler) getControls(c *fiber.Ctx) error {
	p, err := h.eng.PauseState(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(viewOf(p))
}

// putControls flips the caller company's kill switch (owner/admin only; the
// role gate is on the route).
func (h *Handler) putControls(c *fiber.Ctx) error {
	var in pauseInput
	if err := in.parse(c); err != nil {
		return err
	}
	p, err := h.eng.SetCompanyPause(c.Context(), middleware.CompanyID(c), middleware.UserID(c), *in.Paused, in.Reason)
	if err != nil {
		return err
	}
	if !*in.Paused {
		h.tickCompany(c.Context(), middleware.CompanyID(c))
	}
	return c.JSON(viewOf(p))
}

// tickCompany nudges the company's active negotiations after a resume so the
// buyer sees progress without waiting for the next engine interval. Errors
// are logged: the background worker will retry.
func (h *Handler) tickCompany(ctx context.Context, companyID string) {
	rows, err := h.q.ListNegotiationsByBuyer(ctx, sqlc.ListNegotiationsByBuyerParams{BuyerCompanyID: companyID, Lim: 200})
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.Status == StatusActive {
			if err := h.eng.Tick(ctx, r.RfqID); err != nil {
				h.eng.logger.Error().Err(err).Str("rfq_id", r.RfqID).Msg("negotiation: tick after resume")
			}
		}
	}
}

// AdminHandler serves the platform-wide kill switch under /admin/v1.
type AdminHandler struct {
	q   *sqlc.Queries
	eng *Engine
}

func NewAdminHandler(q *sqlc.Queries, eng *Engine) *AdminHandler {
	return &AdminHandler{q: q, eng: eng}
}

// Register mounts the routes behind the platform-admin guard.
func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1/negotiations", adminGuard)
	g.Get("/kill-switch", h.get)
	g.Put("/kill-switch", h.put)
}

func (h *AdminHandler) get(c *fiber.Ctx) error {
	ctrl, err := h.q.GetPlatformNegotiationControl(c.Context())
	if err != nil {
		return err
	}
	return h.respond(c, ctrl)
}

func (h *AdminHandler) put(c *fiber.Ctx) error {
	var in pauseInput
	if err := in.parse(c); err != nil {
		return err
	}
	if *in.Paused && in.Reason == "" {
		return fiber.NewError(fiber.StatusBadRequest, "a reason is required to pause negotiations platform-wide")
	}
	ctrl, err := h.eng.SetPlatformPause(c.Context(), middleware.AdminEmail(c), *in.Paused, in.Reason)
	if err != nil {
		return err
	}
	return h.respond(c, ctrl)
}

func (h *AdminHandler) respond(c *fiber.Ctx, ctrl sqlc.NegotiationPlatformControl) error {
	logRows, err := h.q.ListNegotiationKillSwitchLog(c.Context(), sqlc.ListNegotiationKillSwitchLogParams{Lim: 50})
	if err != nil {
		return err
	}
	if logRows == nil {
		logRows = []sqlc.ListNegotiationKillSwitchLogRow{}
	}
	return c.JSON(fiber.Map{"control": ctrl, "history": logRows})
}

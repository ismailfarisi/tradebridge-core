package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/activation"
	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler wires the identity endpoints to the query layer + token service.
type Handler struct {
	q        *sqlc.Queries
	tokens   *TokenService
	notifier *notify.Notifier
	ent      *entitlements.Service
}

// NewHandler builds the identity handler over the query layer + token service.
func NewHandler(q *sqlc.Queries, tokens *TokenService, notifier *notify.Notifier, ent *entitlements.Service) *Handler {
	return &Handler{q: q, tokens: tokens, notifier: notifier, ent: ent}
}

// Register mounts the auth + user routes. Public routes go on the root group;
// tenant-scoped routes are guarded by the caller-supplied middleware.
// signupRL rate-limits register + login (both IP-abuse prone).
func (h *Handler) Register(app *fiber.App, signupRL fiber.Handler, guard ...fiber.Handler) {
	auth := app.Group("/v1/auth")
	auth.Post("/register", signupRL, h.register)
	auth.Post("/login", signupRL, h.login)
	auth.Post("/accept-invite", h.acceptInvite)
	auth.Post("/verify-email", h.verifyEmail)

	users := app.Group("/v1/users", guard...)
	users.Get("/", h.listUsers)
	users.Post("/invite", middleware.RequireRole("owner", "admin"), h.invite)
	users.Post("/resend-verification", h.resendVerification)
}

type registerReq struct {
	CompanyName string `json:"company_name"`
	CompanyType string `json:"company_type"`
	Email       string `json:"email"`
	Password    string `json:"password"`
}

type authResp struct {
	Token     string `json:"token"`
	CompanyID string `json:"company_id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
}

func (h *Handler) register(c *fiber.Ctx) error {
	var req registerReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.CompanyName == "" || req.Email == "" || len(req.Password) < 8 {
		return fiber.NewError(fiber.StatusBadRequest, "company_name, email, and password (≥8 chars) required")
	}
	if req.CompanyType == "" {
		req.CompanyType = "both"
	}
	if req.CompanyType != "buyer" && req.CompanyType != "seller" && req.CompanyType != "both" {
		return fiber.NewError(fiber.StatusBadRequest, "company_type must be buyer|seller|both")
	}

	ctx := c.Context()
	if _, err := h.q.GetUserByEmail(ctx, req.Email); err == nil {
		return fiber.NewError(fiber.StatusConflict, "email already registered")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	company, err := h.q.CreateCompany(ctx, sqlc.CreateCompanyParams{
		Name: req.CompanyName,
		Type: req.CompanyType,
	})
	if err != nil {
		return err
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		return err
	}
	user, err := h.q.CreateUserWithPassword(ctx, sqlc.CreateUserWithPasswordParams{
		CompanyID:    company.ID,
		Email:        req.Email,
		Role:         "owner",
		PasswordHash: &hash,
	})
	if err != nil {
		return err
	}

	h.sendVerification(ctx, user)

	activation.Emit(ctx, h.q, company.ID, user.ID, activation.EventSignup, map[string]any{"company_type": req.CompanyType})

	return h.issue(c, fiber.StatusCreated, user)
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(c *fiber.Ctx) error {
	var req loginReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	user, err := h.q.GetUserByEmail(c.Context(), req.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	} else if err != nil {
		return err
	}
	if user.PasswordHash == nil || !CheckPassword(*user.PasswordHash, req.Password) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	}
	if st, err := h.q.GetCompanyStatus(c.Context(), user.CompanyID); err == nil && st == "suspended" {
		return fiber.NewError(fiber.StatusForbidden, "company suspended")
	}
	return h.issue(c, fiber.StatusOK, user)
}

type inviteReq struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h *Handler) invite(c *fiber.Ctx) error {
	var req inviteReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Email == "" {
		return fiber.NewError(fiber.StatusBadRequest, "email required")
	}
	if req.Role != "admin" && req.Role != "member" {
		return fiber.NewError(fiber.StatusBadRequest, "role must be admin|member")
	}
	if err := h.ent.CheckSeatQuota(c.Context(), middleware.CompanyID(c)); err != nil {
		return entitlements.Fail(c, err)
	}

	token, err := randomToken()
	if err != nil {
		return err
	}
	inv, err := h.q.CreateInvite(c.Context(), sqlc.CreateInviteParams{
		CompanyID: middleware.CompanyID(c),
		Email:     req.Email,
		Role:      req.Role,
		Token:     token,
		InvitedBy: middleware.UserID(c),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	})
	if err != nil {
		return err
	}
	// The token would be emailed in production; returned here so the invite is usable.
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"invite_id": inv.ID,
		"email":     inv.Email,
		"role":      inv.Role,
		"token":     token,
	})
}

type acceptInviteReq struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (h *Handler) acceptInvite(c *fiber.Ctx) error {
	var req acceptInviteReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Token == "" || len(req.Password) < 8 {
		return fiber.NewError(fiber.StatusBadRequest, "token and password (≥8 chars) required")
	}

	ctx := c.Context()
	inv, err := h.q.GetInviteByToken(ctx, req.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "invite not found or expired")
	} else if err != nil {
		return err
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		return err
	}
	user, err := h.q.CreateUserWithPassword(ctx, sqlc.CreateUserWithPasswordParams{
		CompanyID:    inv.CompanyID,
		Email:        inv.Email,
		Role:         inv.Role,
		PasswordHash: &hash,
	})
	if err != nil {
		return err
	}
	if err := h.q.MarkInviteAccepted(ctx, inv.ID); err != nil {
		return err
	}
	return h.issue(c, fiber.StatusCreated, user)
}

type userResp struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (h *Handler) listUsers(c *fiber.Ctx) error {
	users, err := h.q.ListUsersByCompany(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	out := make([]userResp, len(users))
	for i, u := range users {
		out[i] = userResp{ID: u.ID, Email: u.Email, Role: u.Role}
	}
	return c.JSON(fiber.Map{"users": out})
}

func (h *Handler) issue(c *fiber.Ctx, status int, u sqlc.User) error {
	token, err := h.tokens.Issue(u.CompanyID, u.ID, u.Role)
	if err != nil {
		return err
	}
	return c.Status(status).JSON(authResp{
		Token:     token,
		CompanyID: u.CompanyID,
		UserID:    u.ID,
		Role:      u.Role,
	})
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *Handler) sendVerification(ctx context.Context, u sqlc.User) {
	if h.notifier == nil {
		return
	}
	token, err := randomToken()
	if err != nil {
		return
	}
	if _, err := h.q.CreateEmailVerification(ctx, sqlc.CreateEmailVerificationParams{
		UserID: u.ID, Token: token, ExpiresAt: time.Now().Add(48 * time.Hour),
	}); err != nil {
		return
	}
	subject, html, text := notify.RenderVerificationEmail(h.notifier.BaseURL(), token)
	h.notifier.SendTransactional(u.Email, subject, html, text)
}

type verifyEmailReq struct {
	Token string `json:"token"`
}

func (h *Handler) verifyEmail(c *fiber.Ctx) error {
	var req verifyEmailReq
	if err := c.BodyParser(&req); err != nil || req.Token == "" {
		return fiber.NewError(fiber.StatusBadRequest, "token required")
	}
	ctx := c.Context()
	ev, err := h.q.GetEmailVerificationByToken(ctx, req.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "invalid or expired token")
	} else if err != nil {
		return err
	}
	if ev.ConsumedAt.Valid || time.Now().After(ev.ExpiresAt) {
		return fiber.NewError(fiber.StatusBadRequest, "token already used or expired")
	}
	if err := h.q.ConsumeEmailVerification(ctx, ev.ID); err != nil {
		return err
	}
	if err := h.q.MarkUserEmailVerified(ctx, ev.UserID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"verified": true})
}

func (h *Handler) resendVerification(c *fiber.Ctx) error {
	u, err := h.q.GetUserByID(c.Context(), middleware.UserID(c))
	if err != nil {
		return err
	}
	if u.EmailVerifiedAt.Valid {
		return c.JSON(fiber.Map{"verified": true})
	}
	h.sendVerification(c.Context(), u)
	return c.JSON(fiber.Map{"sent": true})
}

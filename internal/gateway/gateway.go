// Package gateway implements the Agent Gateway protocol: an authenticated SSE
// stream of durable events with replay-on-reconnect, per-event acks, validated
// idempotent agent commands, and an audit trail. It is the contract the seller
// agent's connector (Track B) speaks.
package gateway

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// RateLimitConfig configures the gateway's per-credential and per-IP limits.
type RateLimitConfig struct {
	PerMin            int
	AuthfailMax       int
	AuthfailWindowSec int
}

// Handler serves the agent-facing gateway routes.
type Handler struct {
	q      *sqlc.Queries
	redis  *redis.Client // may be nil; live pub/sub is then disabled (replay still works)
	store  *attachments.Store
	logger zerolog.Logger
	rl     *rateLimiter
}

// NewHandler builds the gateway handler.
func NewHandler(q *sqlc.Queries, rdb *redis.Client, store *attachments.Store, logger zerolog.Logger, rlc RateLimitConfig) *Handler {
	var c counter
	if rdb != nil {
		c = redisCounter{rdb: rdb}
	} else {
		logger.Warn().Msg("gateway rate limiting disabled: no Redis (fail-open)")
	}
	rl := &rateLimiter{
		c:       c,
		perMin:  rlc.PerMin,
		failMax: rlc.AuthfailMax,
		failWin: time.Duration(rlc.AuthfailWindowSec) * time.Second,
		logger:  logger,
	}
	return &Handler{q: q, redis: rdb, store: store, logger: logger, rl: rl}
}

// Register mounts the gateway routes.
func (h *Handler) Register(app *fiber.App) {
	app.Get("/agent/v1/stream", h.stream)
	app.Post("/agent/v1/commands", h.command)
	app.Post("/agent/v1/events/:seq/ack", h.ack)
	app.Post("/agent/v1/heartbeat", h.heartbeat)
	app.Get("/agent/v1/attachments/:id", h.attachment)
}

// authedAgent identifies the connected agent after credential verification.
type authedAgent struct {
	CredentialID string
	CompanyID    string
}

// authenticate verifies the `Authorization: Bearer <id>.<secret>` credential a
// seller's agent presents, and returns its identity. The credential is the
// `<id>.<raw>` value issued once by the Core API (Sprint A1).
func (h *Handler) authenticate(c *fiber.Ctx) (authedAgent, error) {
	ctx := c.Context()
	ip := clientIP(c)
	if h.rl.authFailBlocked(ctx, ip) {
		c.Set("Retry-After", "60")
		return authedAgent{}, fiber.NewError(fiber.StatusTooManyRequests, "too many failed auth attempts")
	}

	raw := strings.TrimSpace(c.Get("Authorization"))
	raw = strings.TrimPrefix(raw, "Bearer ")
	id, secret, ok := strings.Cut(raw, ".")
	if !ok || id == "" || secret == "" {
		h.rl.recordAuthFailure(ctx, ip)
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "missing or malformed agent credential")
	}

	cred, err := h.q.GetAgentCredentialByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		h.rl.recordAuthFailure(ctx, ip)
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "invalid agent credential")
	} else if err != nil {
		return authedAgent{}, err
	}
	if cred.Status != "active" || !identity.CheckPassword(cred.SecretHash, secret) {
		h.rl.recordAuthFailure(ctx, ip)
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "invalid agent credential")
	}
	if credExpired(cred.ExpiresAt, time.Now()) {
		// Not a brute-force signal — do not record an auth failure.
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "credential expired")
	}
	if !h.rl.allowCred(ctx, cred.ID, time.Now().Unix()/60) {
		c.Set("Retry-After", "60")
		return authedAgent{}, fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded")
	}
	return authedAgent{CredentialID: cred.ID, CompanyID: cred.CompanyID}, nil
}

// heartbeat marks the agent as recently seen (online).
func (h *Handler) heartbeat(c *fiber.Ctx) error {
	agent, err := h.authenticate(c)
	if err != nil {
		return err
	}
	if err := h.q.TouchAgentCredentialLastSeen(c.Context(), agent.CredentialID); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"status": "ok"})
}

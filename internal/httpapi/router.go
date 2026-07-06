// Package httpapi assembles the Core API's HTTP routes. Extracted from main so
// tests can build the exact same router against a test database.
package httpapi

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/agent"
	"github.com/tradebridge/core/internal/catalog"
	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/internal/suppliers"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Config holds the settings the router needs: JWT secret, token TTL, and logger.
type Config struct {
	JWTSecret string
	TokenTTL  time.Duration
	Logger    zerolog.Logger
}

// New builds the Core API with all Sprint A1 routes mounted.
func New(q *sqlc.Queries, cfg Config) *fiber.App {
	f := fiber.New(fiber.Config{AppName: "tradebridge-core-api"})
	f.Use(middleware.RequestLogger(cfg.Logger))

	f.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Tenant guard chain, applied per-group (never as a broad "/v1" prefix) so
	// public /v1/auth/* routes stay open.
	guard := []fiber.Handler{
		middleware.JWT(cfg.JWTSecret),
		middleware.TenantGuard(),
	}

	f.Get("/v1/me", append(guard, func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"company_id": middleware.CompanyID(c),
			"user_id":    middleware.UserID(c),
			"role":       middleware.Role(c),
		})
	})...)

	ttl := cfg.TokenTTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	tokens := identity.NewTokenService(cfg.JWTSecret, ttl)

	identity.NewHandler(q, tokens).Register(f, guard...)
	suppliers.NewHandler(q).Register(f, guard...)
	catalog.NewHandler(q).Register(f, guard...)
	agent.NewHandler(q).Register(f, guard...)

	return f
}

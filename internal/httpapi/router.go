// Package httpapi assembles the Core API's HTTP routes. Extracted from main so
// tests can build the exact same router against a test database.
package httpapi

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/admin"
	"github.com/tradebridge/core/internal/agent"
	"github.com/tradebridge/core/internal/catalog"
	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/internal/orders"
	"github.com/tradebridge/core/internal/quotes"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/internal/settings"
	"github.com/tradebridge/core/internal/suppliers"
	"github.com/tradebridge/core/pkg/cryptobox"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Config holds the settings the router needs: JWT secret, token TTL, logger,
// Redis (event broadcast), the pool (for transactional quote acceptance), and
// the RFQ extractor configuration.
type Config struct {
	JWTSecret       string
	TokenTTL        time.Duration
	Logger          zerolog.Logger
	Redis           *redis.Client
	Pool            *pgxpool.Pool
	AnthropicAPIKey string
	AnthropicModel  string
	SettingsEncKey  string
	CORSOrigins     string

	// Platform superadmin realm (separate from company auth).
	AdminJWTSecret        string
	PlatformAdminEmail    string
	PlatformAdminPassword string

	// RFQTTLDays is how long a matched RFQ stays open before the sweeper
	// (Task C4) expires it.
	RFQTTLDays int
}

// New builds the Core API with all Phase 1 routes mounted.
func New(q *sqlc.Queries, cfg Config) *fiber.App {
	f := fiber.New(fiber.Config{AppName: "tradebridge-core-api"})
	f.Use(middleware.RequestLogger(cfg.Logger))

	// The frontend runs on a different origin than the API; allow it explicitly.
	origins := cfg.CORSOrigins
	if origins == "" {
		origins = "http://localhost:3010"
	}
	f.Use(cors.New(cors.Config{
		AllowOrigins: origins,
		AllowHeaders: "Origin, Content-Type, Accept, Authorization",
		AllowMethods: "GET, POST, PUT, DELETE, OPTIONS",
	}))

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
		out := fiber.Map{
			"company_id": middleware.CompanyID(c),
			"user_id":    middleware.UserID(c),
			"role":       middleware.Role(c),
		}
		// Include company name + type so the UI can render role-aware nav.
		if co, err := q.GetCompanyByID(c.Context(), middleware.CompanyID(c)); err == nil {
			out["company_name"] = co.Name
			out["company_type"] = co.Type
		}
		return c.JSON(out)
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

	pub := eventbus.New(q, cfg.Redis, cfg.Logger)

	// Settings hold the global (platform-wide) provider config; the encryption
	// key falls back to the JWT secret when a dedicated one isn't configured.
	// Built here (outside the admin block) because rfq needs settingsSvc as its
	// extractor resolver even when the admin realm is disabled; its HTTP
	// handler is only registered below, inside the admin guard.
	encSecret := cfg.SettingsEncKey
	if encSecret == "" {
		encSecret = cfg.JWTSecret
	}
	box, err := cryptobox.New(encSecret)
	if err != nil {
		panic(err)
	}
	envExtraction := rfq.ProviderConfig{Model: cfg.AnthropicModel, APIKey: cfg.AnthropicAPIKey}
	if cfg.AnthropicAPIKey != "" {
		envExtraction.Provider = "anthropic"
	}
	settingsSvc := settings.NewService(q, box, envExtraction)

	rfq.NewHandler(rfq.NewService(q, pub, settingsSvc, cfg.Logger, cfg.RFQTTLDays), q).Register(f, guard...)
	quotes.NewHandler(quotes.NewService(q, cfg.Pool, pub), q).Register(f, guard...)
	orders.NewHandler(q).Register(f, guard...)

	// Platform superadmin realm: a separate credential store and JWT secret
	// from company auth. Fail-closed — routes are not mounted at all when no
	// secret is configured, rather than mounting with a guessable default.
	if cfg.AdminJWTSecret != "" {
		adminSvc := admin.NewService(q)
		// Seed the platform admin from env (idempotent). Errors are logged, not fatal.
		if err := adminSvc.Bootstrap(context.Background(), cfg.PlatformAdminEmail, cfg.PlatformAdminPassword); err != nil {
			cfg.Logger.Error().Err(err).Msg("platform admin bootstrap failed")
		}
		adminTokens := admin.NewTokenService(cfg.AdminJWTSecret, 24*time.Hour)
		adminGuard := middleware.PlatformAdminGuard(cfg.AdminJWTSecret)
		admin.NewHandler(adminSvc, adminTokens).Register(f, adminGuard)
		settings.NewHandler(settingsSvc).Register(f, adminGuard)
	} else {
		cfg.Logger.Warn().Msg("ADMIN_JWT_SECRET unset — platform admin routes disabled")
	}

	return f
}

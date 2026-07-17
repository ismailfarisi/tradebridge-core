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
	"github.com/tradebridge/core/internal/assist"
	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/internal/billing"
	"github.com/tradebridge/core/internal/catalog"
	"github.com/tradebridge/core/internal/disputes"
	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/internal/invoicing"
	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/internal/orders"
	"github.com/tradebridge/core/internal/quotes"
	"github.com/tradebridge/core/internal/reviews"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/internal/settings"
	"github.com/tradebridge/core/internal/suppliers"
	"github.com/tradebridge/core/internal/thread"
	"github.com/tradebridge/core/internal/trust"
	"github.com/tradebridge/core/internal/waitlist"
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

	// ResendAPIKey, EmailFrom, AppBaseURL configure the notify package's
	// transactional email sender and link-building base URL.
	ResendAPIKey string
	EmailFrom    string
	AppBaseURL   string

	// Platform superadmin realm (separate from company auth).
	AdminJWTSecret        string
	PlatformAdminEmail    string
	PlatformAdminPassword string

	// RFQTTLDays is how long a matched RFQ stays open before the sweeper
	// (Task C4) expires it.
	RFQTTLDays int

	// AttachmentsDir is where RFQ attachment bytes are stored on disk.
	AttachmentsDir string

	// AgentCredTTLDays is how long an issued agent credential stays valid.
	AgentCredTTLDays int
	// AgentCredRotateGraceDays is the grace window for rotated credentials.
	AgentCredRotateGraceDays int

	// Stripe billing (Plan 2). Empty SecretKey => StubBilling.
	StripeSecretKey          string
	StripeWebhookSecret      string
	StripePriceProMonth      string
	StripePriceProYear       string
	StripePriceBusinessMonth string
	StripePriceBusinessYear  string
	// BillingDevMode allows the unsigned StubBilling webhook/checkout when no
	// Stripe key is configured — DEV/TEST ONLY. Never set in prod.
	BillingDevMode bool

	// VerifyDNSTimeout bounds each DNS-TXT lookup during domain verification.
	VerifyDNSTimeout time.Duration
	// VerifyRequiredForMatch gates the RFQ match pool to verified sellers only.
	VerifyRequiredForMatch bool

	// Abuse controls (Sprint 3). RateLimitEnabled is the master switch; each
	// limit is "N/DURATION" (e.g. "5/1h"). All fail OPEN.
	RateLimitEnabled     bool
	RateLimitSignup      string
	RateLimitRFQ         string
	RateLimitMessage     string
	RateLimitVerifyCheck string
	RateLimitReport      string
}

// New builds the Core API with all Phase 1 routes mounted.
func New(q *sqlc.Queries, cfg Config) *fiber.App {
	f := fiber.New(fiber.Config{AppName: "tradebridge-core-api", BodyLimit: 12 * 1024 * 1024})
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

	// Waitlist: PUBLIC, pre-signup capture — no tenant guard.
	waitlist.NewHandler(q).Register(f)

	// Public seller storefront (unauthenticated; rate-limited in Plan 3).
	suppliers.NewPublicHandler(q).RegisterPublic(f)

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
		// Include email verification status so the frontend verify-banner
		// knows whether to prompt the user.
		if u, err := q.GetUserByID(c.Context(), middleware.UserID(c)); err == nil {
			out["email_verified"] = u.EmailVerifiedAt.Valid
		}
		return c.JSON(out)
	})...)

	ttl := cfg.TokenTTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	tokens := identity.NewTokenService(cfg.JWTSecret, ttl)

	pub := eventbus.New(q, cfg.Redis, cfg.Logger)

	sender := notify.NewSender(cfg.ResendAPIKey, cfg.EmailFrom, cfg.Logger)
	notifier := notify.New(q, sender, cfg.AppBaseURL, cfg.Logger)
	entSvc := entitlements.NewService(q, cfg.Logger)

	// Fail-open Redis rate limiters (Sprint 3 abuse controls). A nil Redis
	// client or RateLimitEnabled=false makes every limiter a pass-through.
	rlCounter := middleware.NewRedisRateCounter(cfg.Redis)
	rlOn := cfg.RateLimitEnabled
	mkRL := func(scope, spec string, def middleware.RateLimit, key middleware.KeyFunc) fiber.Handler {
		return middleware.RateLimiter(rlOn, rlCounter, scope, middleware.ParseRateLimit(spec, def), key, cfg.Logger)
	}
	rlSignup := mkRL("signup", cfg.RateLimitSignup, middleware.RateLimit{Limit: 5, Window: time.Hour}, middleware.KeyIP)
	rlRFQ := mkRL("rfq", cfg.RateLimitRFQ, middleware.RateLimit{Limit: 20, Window: time.Hour}, middleware.KeyCompanyOrIP)
	rlMessage := mkRL("message", cfg.RateLimitMessage, middleware.RateLimit{Limit: 120, Window: time.Hour}, middleware.KeyCompanyOrIP)
	rlVerify := mkRL("verify", cfg.RateLimitVerifyCheck, middleware.RateLimit{Limit: 10, Window: time.Hour}, middleware.KeyCompanyOrIP)
	// rlReport (Task 3's reports handler) is wired in further down where the
	// reports handler is registered, to keep it visibly close to its use.

	identity.NewHandler(q, tokens, notifier, entSvc).Register(f, rlSignup, guard...)
	suppliers.NewHandler(q).Register(f, guard...)
	trustSvc := trust.NewService(q, cfg.Pool, trust.NewNetResolver(cfg.VerifyDNSTimeout), cfg.Logger)
	trust.NewHandler(trustSvc, q).Register(f, middleware.RequireVerified(q), rlVerify, guard...)
	catalog.NewHandler(q, entSvc).Register(f, guard...)
	agent.NewHandler(q, agent.Config{TTLDays: cfg.AgentCredTTLDays, GraceDays: cfg.AgentCredRotateGraceDays}).Register(f, guard...)

	notify.NewHandler(q).Register(f, guard...)

	// Billing is registered BEFORE entitlements: its Register mounts the
	// PUBLIC webhook via app.Post at the "/v1/billing" prefix, and in Fiber
	// a Use-middleware group (like entitlements' guarded "/v1/billing" group
	// below) only applies to routes registered AFTER it — registering
	// billing first keeps the webhook unauthenticated as intended.
	billingProvider := billing.NewBilling(billing.StripeConfig{
		SecretKey:     cfg.StripeSecretKey,
		WebhookSecret: cfg.StripeWebhookSecret,
		Prices: billing.Prices{
			ProMonth:      cfg.StripePriceProMonth,
			ProYear:       cfg.StripePriceProYear,
			BusinessMonth: cfg.StripePriceBusinessMonth,
			BusinessYear:  cfg.StripePriceBusinessYear,
		},
	}, cfg.Logger)
	billingSvc := billing.NewService(q, cfg.Logger)
	// billingActive gates the stub webhook/checkout: a keyless deploy
	// (Enabled()==false) only accepts unsigned billing requests when an
	// operator has explicitly opted into BILLING_DEV_MODE (dev/test only).
	billingActive := billingProvider.Enabled() || cfg.BillingDevMode
	billing.NewHandler(q, billingProvider, billingSvc, appBaseURLOr(cfg.AppBaseURL), billingActive).Register(f, guard...)

	entitlements.NewHandler(entSvc).Register(f, guard...)

	// Buyer AI assist: metered via entSvc's assist quota. A keyless deploy
	// (AnthropicAPIKey=="") runs the deterministic stub — no network.
	assistSvc := assist.NewService(cfg.AnthropicAPIKey, cfg.Logger)
	assist.NewHandler(assistSvc, entSvc, q).Register(f, guard...)

	// Attachments: bytes on local disk, metadata in Postgres. Constructed
	// once here; attLoader is exposed to later tasks' rfq/quotes services.
	attDir, err := attachments.ResolveDir(cfg.AttachmentsDir)
	if err != nil {
		panic(err)
	}
	attStore, err := attachments.NewStore(attDir)
	if err != nil {
		panic(err)
	}
	attLoader := attachments.NewLoader(q, attStore)
	attachments.NewHandler(q, attStore).Register(f, guard...)

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
	llmHealth := rfq.NewHealthTracker(20)
	settingsSvc := settings.NewService(q, box, envExtraction, llmHealth)

	rfq.NewHandler(rfq.NewService(q, pub, settingsSvc, attLoader, cfg.Logger, cfg.RFQTTLDays, llmHealth, notifier, cfg.VerifyRequiredForMatch), q, entSvc).Register(f, middleware.RequireVerified(q), rlRFQ, guard...)
	// invSvc is shared: quotes.Accept creates the invoice inside its
	// transaction, and the invoicing handler serves it back as a PDF.
	invSvc := invoicing.NewService(cfg.Logger)
	threadPub := thread.NewPublisher(cfg.Redis, cfg.Logger)
	quotes.NewHandler(quotes.NewService(q, cfg.Pool, pub, attLoader, cfg.Logger, notifier, invSvc, entSvc, threadPub), q).Register(f, rlMessage, guard...)
	thread.NewHandler(q, cfg.Redis, cfg.Logger).Register(f, guard...)
	orders.NewHandler(orders.NewService(q, cfg.Pool, notifier, cfg.Logger), q).Register(f, guard...)
	invoicing.NewHandler(invSvc, q).Register(f, guard...)

	// Reviews: party routes (create/list) under the tenant guard; the public
	// GET /public/v1/sellers/:id/reviews route lives inside
	// suppliers.RegisterPublic above. Admin remove is registered in the admin
	// block below.
	reviewsSvc := reviews.NewService(q, notifier, cfg.Logger)
	reviews.NewHandler(reviewsSvc).Register(f, guard...)

	// Disputes: party raise/list under the tenant guard; admin queue +
	// resolve registered in the admin block below.
	disputesSvc := disputes.NewService(q, cfg.Pool, notifier, cfg.Logger)
	disputes.NewHandler(disputesSvc).Register(f, guard...)

	// Abuse reports: rate-limited, requires a verified email.
	rlReport := mkRL("report", cfg.RateLimitReport, middleware.RateLimit{Limit: 30, Window: time.Hour}, middleware.KeyCompanyOrIP)
	reports := trust.NewReportsHandler(q)
	reports.Register(f, append(append([]fiber.Handler{}, guard...), middleware.RequireVerified(q), rlReport)...)

	trust.NewBlocksHandler(q).Register(f, guard...)

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
		entitlements.NewAdminHandler(q, entSvc).Register(f, adminGuard)
		trust.NewAdminHandler(trustSvc, q).Register(f, adminGuard)
		reports.RegisterAdmin(f, adminGuard)
		reviews.NewAdminHandler(reviewsSvc).Register(f, adminGuard)
		disputes.NewAdminHandler(disputesSvc).RegisterAdmin(f, adminGuard)
	} else {
		cfg.Logger.Warn().Msg("ADMIN_JWT_SECRET unset — platform admin routes disabled")
	}

	return f
}

// appBaseURLOr returns the configured base URL or the local dev default.
func appBaseURLOr(u string) string {
	if u == "" {
		return "http://localhost:3010"
	}
	return u
}

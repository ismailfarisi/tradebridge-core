// Package config loads runtime configuration from environment variables
// (and an optional .env file) via Viper.
package config

import (
	"strings"

	"github.com/spf13/viper"
)

// Config holds all runtime configuration for the Core API and Gateway.
type Config struct {
	Port        string `mapstructure:"PORT"`
	GatewayPort string `mapstructure:"GATEWAY_PORT"`
	DatabaseURL string `mapstructure:"DATABASE_URL"`
	RedisURL    string `mapstructure:"REDIS_URL"`
	JWTSecret   string `mapstructure:"JWT_SECRET"`
	LogLevel    string `mapstructure:"LOG_LEVEL"`

	// AnthropicAPIKey enables Claude-backed RFQ requirement extraction. When
	// empty, the RFQ engine falls back to a deterministic heuristic extractor.
	AnthropicAPIKey string `mapstructure:"ANTHROPIC_API_KEY"`
	// AnthropicModel overrides the extraction model (default claude-haiku-4-5).
	AnthropicModel string `mapstructure:"ANTHROPIC_MODEL"`

	// SettingsEncKey encrypts provider API keys stored via the settings UI.
	// When empty, a key is derived from JWT_SECRET (set a dedicated key in prod).
	SettingsEncKey string `mapstructure:"SETTINGS_ENC_KEY"`

	// CORSOrigins is a comma-separated allowlist of browser origins permitted to
	// call the API (the frontend runs on a different origin than the API).
	CORSOrigins string `mapstructure:"CORS_ORIGINS"`

	// Platform superadmin realm (separate from company auth).
	AdminJWTSecret        string `mapstructure:"ADMIN_JWT_SECRET"`
	PlatformAdminEmail    string `mapstructure:"PLATFORM_ADMIN_EMAIL"`
	PlatformAdminPassword string `mapstructure:"PLATFORM_ADMIN_PASSWORD"`

	// RFQTTLDays is how long an RFQ stays open before the sweeper expires it.
	RFQTTLDays int `mapstructure:"RFQ_TTL_DAYS"`
	// RFQSweepSeconds is the expiry-sweeper tick interval (Task C4).
	RFQSweepSeconds int `mapstructure:"RFQ_SWEEP_SECONDS"`

	// AgentCredTTLDays is how long an issued agent credential stays valid
	// before it expires (<= 0 means never expires).
	AgentCredTTLDays int `mapstructure:"AGENT_CRED_TTL_DAYS"`
	// AgentCredRotateGraceDays is the grace window during which a rotated
	// (superseded) agent credential still authenticates.
	AgentCredRotateGraceDays int `mapstructure:"AGENT_CRED_ROTATE_GRACE_DAYS"`
	// AgentRLPerMin is the per-credential agent request rate limit.
	AgentRLPerMin int `mapstructure:"AGENT_RL_PER_MIN"`
	// AgentAuthfailMax is the max failed auth attempts allowed within the
	// AgentAuthfailWindowSec window before lockout.
	AgentAuthfailMax int `mapstructure:"AGENT_AUTHFAIL_MAX"`
	// AgentAuthfailWindowSec is the sliding window (seconds) for AgentAuthfailMax.
	AgentAuthfailWindowSec int `mapstructure:"AGENT_AUTHFAIL_WINDOW_SEC"`

	// AttachmentsDir is where RFQ attachment bytes are stored on disk.
	// Empty means ~/tradebridge-data/attachments.
	AttachmentsDir string `mapstructure:"ATTACHMENTS_DIR"`

	// ResendAPIKey enables transactional email via Resend. When empty, the
	// notify package logs email instead of sending it.
	ResendAPIKey string `mapstructure:"RESEND_API_KEY"`
	// EmailFrom is the From address used for transactional email.
	EmailFrom string `mapstructure:"EMAIL_FROM"`
	// AppBaseURL is used to build links in rendered emails.
	AppBaseURL string `mapstructure:"APP_BASE_URL"`
	// SentryDSN enables error reporting to Sentry when set.
	SentryDSN string `mapstructure:"SENTRY_DSN"`

	// Stripe billing (Sprint 2). All empty => StubBilling (dev/test); checkout
	// returns a stub URL. Set these to go live (activation follow-up).
	StripeSecretKey          string `mapstructure:"STRIPE_SECRET_KEY"`
	StripeWebhookSecret      string `mapstructure:"STRIPE_WEBHOOK_SECRET"`
	StripePriceProMonth      string `mapstructure:"STRIPE_PRICE_PRO_MONTH"`
	StripePriceProYear       string `mapstructure:"STRIPE_PRICE_PRO_YEAR"`
	StripePriceBusinessMonth string `mapstructure:"STRIPE_PRICE_BUSINESS_MONTH"`
	StripePriceBusinessYear  string `mapstructure:"STRIPE_PRICE_BUSINESS_YEAR"`
}

// Load reads configuration from the environment, falling back to a local
// .env file and then to sane defaults. Environment variables always win.
func Load() (*Config, error) {
	v := viper.New()

	v.SetDefault("PORT", "8080")
	v.SetDefault("GATEWAY_PORT", "8081")
	v.SetDefault("DATABASE_URL", "postgres://tradebridge:tradebridge@localhost:5442/tradebridge?sslmode=disable")
	v.SetDefault("REDIS_URL", "redis://localhost:6389/0")
	v.SetDefault("JWT_SECRET", "dev-secret-change-me")
	v.SetDefault("LOG_LEVEL", "info")
	v.SetDefault("ANTHROPIC_API_KEY", "")
	v.SetDefault("ANTHROPIC_MODEL", "claude-haiku-4-5")
	v.SetDefault("SETTINGS_ENC_KEY", "")
	v.SetDefault("CORS_ORIGINS", "http://localhost:3010")
	v.SetDefault("ADMIN_JWT_SECRET", "")
	v.SetDefault("PLATFORM_ADMIN_EMAIL", "")
	v.SetDefault("PLATFORM_ADMIN_PASSWORD", "")
	v.SetDefault("RFQ_TTL_DAYS", 14)
	v.SetDefault("RFQ_SWEEP_SECONDS", 300)
	v.SetDefault("ATTACHMENTS_DIR", "")
	v.SetDefault("AGENT_CRED_TTL_DAYS", 90)
	v.SetDefault("AGENT_CRED_ROTATE_GRACE_DAYS", 7)
	v.SetDefault("AGENT_RL_PER_MIN", 120)
	v.SetDefault("AGENT_AUTHFAIL_MAX", 20)
	v.SetDefault("AGENT_AUTHFAIL_WINDOW_SEC", 300)
	v.SetDefault("RESEND_API_KEY", "")
	v.SetDefault("EMAIL_FROM", "TradeBridge <no-reply@switeaz.com>")
	v.SetDefault("APP_BASE_URL", "http://localhost:3010")
	v.SetDefault("SENTRY_DSN", "")

	v.SetDefault("STRIPE_SECRET_KEY", "")
	v.SetDefault("STRIPE_WEBHOOK_SECRET", "")
	v.SetDefault("STRIPE_PRICE_PRO_MONTH", "")
	v.SetDefault("STRIPE_PRICE_PRO_YEAR", "")
	v.SetDefault("STRIPE_PRICE_BUSINESS_MONTH", "")
	v.SetDefault("STRIPE_PRICE_BUSINESS_YEAR", "")

	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	// Missing .env is fine — env vars and defaults cover it.
	_ = v.ReadInConfig()

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

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

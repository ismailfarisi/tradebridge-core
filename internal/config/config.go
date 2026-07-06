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

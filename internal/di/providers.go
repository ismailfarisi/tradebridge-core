// Package di wires the application's dependencies together with Google Wire.
// providers.go holds the concrete constructors; wire.go holds the injector.
package di

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/config"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// App is the fully-assembled dependency graph handed to the entrypoints.
type App struct {
	Config  *config.Config
	Pool    *pgxpool.Pool
	Redis   *redis.Client
	Queries *sqlc.Queries
	Logger  zerolog.Logger
}

// NewLogger builds a zerolog logger at the configured level.
func NewLogger(cfg *config.Config) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	return zerolog.New(os.Stdout).Level(level).With().Timestamp().Logger()
}

// NewPool opens a pgx connection pool. The returned cleanup closes it.
func NewPool(cfg *config.Config) (*pgxpool.Pool, func(), error) {
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	return pool, func() { pool.Close() }, nil
}

// NewRedis opens a go-redis client. The returned cleanup closes it.
func NewRedis(cfg *config.Config) (*redis.Client, func(), error) {
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, nil, err
	}
	client := redis.NewClient(opt)
	return client, func() { _ = client.Close() }, nil
}

// NewQueries binds the sqlc-generated query set to the pool.
func NewQueries(pool *pgxpool.Pool) *sqlc.Queries {
	return sqlc.New(pool)
}

package middleware

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// RateCounter increments a windowed counter; the first hit in a window sets TTL.
type RateCounter interface {
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

type redisRateCounter struct{ rdb *redis.Client }

func (r redisRateCounter) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := r.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		_ = r.rdb.Expire(ctx, key, ttl).Err()
	}
	return n, nil
}

// NewRedisRateCounter returns a Redis-backed counter, or nil when rdb is nil so
// RateLimiter becomes a pass-through (fail open).
func NewRedisRateCounter(rdb *redis.Client) RateCounter {
	if rdb == nil {
		return nil
	}
	return redisRateCounter{rdb: rdb}
}

// RateLimit is a cap over a rolling fixed window.
type RateLimit struct {
	Limit  int
	Window time.Duration
}

// ParseRateLimit parses "N/DURATION" (e.g. "5/1h", "20/1m"), falling back to def.
func ParseRateLimit(s string, def RateLimit) RateLimit {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || n <= 0 {
		return def
	}
	w, err := time.ParseDuration(strings.TrimSpace(parts[1]))
	if err != nil || w <= 0 {
		return def
	}
	return RateLimit{Limit: n, Window: w}
}

// KeyFunc derives the per-request bucket key.
type KeyFunc func(c *fiber.Ctx) string

// KeyIP buckets by client IP.
func KeyIP(c *fiber.Ctx) string { return "ip:" + ClientIP(c) }

// KeyCompanyOrIP buckets by company id when authenticated, else IP.
func KeyCompanyOrIP(c *fiber.Ctx) string {
	if cid := CompanyID(c); cid != "" {
		return "co:" + cid
	}
	return "ip:" + ClientIP(c)
}

// ClientIP resolves the real caller behind Cloudflare/Caddy.
func ClientIP(c *fiber.Ctx) string {
	if ip := strings.TrimSpace(c.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if xff := c.Get("X-Forwarded-For"); xff != "" {
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	return c.IP()
}

// RateLimiter enforces lim per window per key. FAILS OPEN on !enabled, nil
// counter, or counter error. Over-limit → 429 with a JSON body + Retry-After.
func RateLimiter(enabled bool, counter RateCounter, scope string, lim RateLimit, key KeyFunc, logger zerolog.Logger) fiber.Handler {
	winSecs := int64(lim.Window / time.Second)
	if winSecs < 1 {
		winSecs = 1
	}
	return func(c *fiber.Ctx) error {
		if !enabled || counter == nil {
			return c.Next()
		}
		window := time.Now().Unix() / winSecs
		k := fmt.Sprintf("rl:%s:%s:%d", scope, key(c), window)
		n, err := counter.Incr(c.Context(), k, lim.Window+5*time.Second)
		if err != nil {
			logger.Warn().Err(err).Str("scope", scope).Msg("rate limiter failing open")
			return c.Next()
		}
		if n > int64(lim.Limit) {
			retry := int(lim.Window.Seconds())
			c.Set("Retry-After", strconv.Itoa(retry))
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"code":                "rate_limited",
				"retry_after_seconds": retry,
			})
		}
		return c.Next()
	}
}

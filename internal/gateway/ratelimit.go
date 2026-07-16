package gateway

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// counter is the minimal Redis surface the limiter needs, so the limiter logic
// is testable with an in-memory fake.
type counter interface {
	incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
	get(ctx context.Context, key string) (int64, error)
}

type redisCounter struct{ rdb *redis.Client }

func (r redisCounter) incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := r.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		_ = r.rdb.Expire(ctx, key, ttl).Err()
	}
	return n, nil
}

func (r redisCounter) get(ctx context.Context, key string) (int64, error) {
	n, err := r.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return n, err
}

// rateLimiter enforces per-credential request caps and per-IP failed-auth
// blocks. A nil counter (no Redis) or any counter error fails OPEN — the gateway
// must never break because rate limiting is unavailable.
type rateLimiter struct {
	c       counter // nil → disabled (fail-open)
	perMin  int
	failMax int
	failWin time.Duration
	logger  zerolog.Logger
}

// allowCred returns false when the credential is over perMin for this window.
func (rl *rateLimiter) allowCred(ctx context.Context, credID string, minute int64) bool {
	if rl == nil || rl.c == nil {
		return true
	}
	n, err := rl.c.incr(ctx, fmt.Sprintf("rl:cred:%s:%d", credID, minute), 65*time.Second)
	if err != nil {
		return true // fail open
	}
	return n <= int64(rl.perMin)
}

// authFailBlocked reports whether an IP has reached the failed-auth threshold.
func (rl *rateLimiter) authFailBlocked(ctx context.Context, ip string) bool {
	if rl == nil || rl.c == nil {
		return false
	}
	n, err := rl.c.get(ctx, "rl:authfail:"+ip)
	if err != nil {
		return false // fail open
	}
	return n >= int64(rl.failMax)
}

// recordAuthFailure bumps an IP's failed-auth counter within the window.
func (rl *rateLimiter) recordAuthFailure(ctx context.Context, ip string) {
	if rl == nil || rl.c == nil {
		return
	}
	_, _ = rl.c.incr(ctx, "rl:authfail:"+ip, rl.failWin)
}

// clientIP resolves the real caller behind Cloudflare/Caddy.
func clientIP(c *fiber.Ctx) string {
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

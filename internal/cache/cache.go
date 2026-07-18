// Package cache is a fail-open Redis JSON cache for hot read paths. A nil or
// unreachable Redis makes every operation a miss / no-op — correctness never
// depends on the cache.
package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

type Cache struct {
	rdb    *redis.Client
	logger zerolog.Logger
}

func New(rdb *redis.Client, logger zerolog.Logger) *Cache {
	return &Cache{rdb: rdb, logger: logger}
}

// GetJSON reports whether key was a hit and, on hit, unmarshals into dest.
func (c *Cache) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, nil
	}
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		c.logger.Debug().Err(err).Str("key", key).Msg("cache get failed (fail-open)")
		return false, nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return false, nil
	}
	return true, nil
}

func (c *Cache) SetJSON(ctx context.Context, key string, v any, ttl time.Duration) {
	if c == nil || c.rdb == nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	if err := c.rdb.Set(ctx, key, raw, ttl).Err(); err != nil {
		c.logger.Debug().Err(err).Str("key", key).Msg("cache set failed (fail-open)")
	}
}

func (c *Cache) Delete(ctx context.Context, keys ...string) {
	if c == nil || c.rdb == nil || len(keys) == 0 {
		return
	}
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		c.logger.Debug().Err(err).Msg("cache delete failed (fail-open)")
	}
}

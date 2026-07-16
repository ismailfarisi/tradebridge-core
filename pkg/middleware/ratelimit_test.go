package middleware

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type fakeCounter struct {
	m   map[string]int64
	err error
}

func (f *fakeCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	if f.m == nil {
		f.m = map[string]int64{}
	}
	f.m[key]++
	return f.m[key], nil
}

func appWith(h fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Get("/x", h, func(c *fiber.Ctx) error { return c.SendString("ok") })
	return app
}

func status(t *testing.T, app *fiber.App) int {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest("GET", "/x", nil), -1)
	require.NoError(t, err)
	return resp.StatusCode
}

func TestRateLimiterAllowsUpToLimitThen429(t *testing.T) {
	fc := &fakeCounter{}
	rl := RateLimiter(true, fc, "test", RateLimit{Limit: 2, Window: time.Hour}, func(*fiber.Ctx) string { return "k" }, zerolog.Nop())
	app := appWith(rl)
	require.Equal(t, 200, status(t, app)) // 1
	require.Equal(t, 200, status(t, app)) // 2
	require.Equal(t, 429, status(t, app)) // 3 over
}

func TestRateLimiterFailsOpenOnCounterError(t *testing.T) {
	fc := &fakeCounter{err: errors.New("redis down")}
	rl := RateLimiter(true, fc, "test", RateLimit{Limit: 1, Window: time.Hour}, func(*fiber.Ctx) string { return "k" }, zerolog.Nop())
	app := appWith(rl)
	require.Equal(t, 200, status(t, app))
	require.Equal(t, 200, status(t, app)) // still allowed — fail open
}

func TestRateLimiterDisabledOrNilCounterPassThrough(t *testing.T) {
	rlDisabled := RateLimiter(false, &fakeCounter{}, "t", RateLimit{Limit: 1, Window: time.Hour}, func(*fiber.Ctx) string { return "k" }, zerolog.Nop())
	require.Equal(t, 200, status(t, appWith(rlDisabled)))
	require.Equal(t, 200, status(t, appWith(rlDisabled)))

	rlNil := RateLimiter(true, nil, "t", RateLimit{Limit: 1, Window: time.Hour}, func(*fiber.Ctx) string { return "k" }, zerolog.Nop())
	require.Equal(t, 200, status(t, appWith(rlNil)))
	require.Equal(t, 200, status(t, appWith(rlNil)))
}

func TestParseRateLimit(t *testing.T) {
	def := RateLimit{Limit: 9, Window: time.Minute}
	require.Equal(t, RateLimit{Limit: 5, Window: time.Hour}, ParseRateLimit("5/1h", def))
	require.Equal(t, RateLimit{Limit: 20, Window: time.Minute}, ParseRateLimit("20/1m", def))
	require.Equal(t, def, ParseRateLimit("garbage", def))
	require.Equal(t, def, ParseRateLimit("", def))
	require.Equal(t, def, ParseRateLimit("5/", def))
}

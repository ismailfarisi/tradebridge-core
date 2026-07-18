package waitlist

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

func wlQueriesOrSkip(t *testing.T) *sqlc.Queries {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool)
}

func postWaitlist(app *fiber.App, email string) int {
	body := []byte(`{"email":"` + email + `"}`)
	req := httptest.NewRequest("POST", "/v1/waitlist", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		panic(err)
	}
	return resp.StatusCode
}

// The public /v1/waitlist endpoint is unauthenticated, so — like every other
// public write (signup, discovery, reports) — it must be behind a fail-open
// per-IP rate limiter, or a single bad actor can flood the waitlist table
// unbounded. This asserts the limiter is actually wired to the route: a low
// cap trips a 429 on the request that exceeds it.
func TestWaitlistJoinIsRateLimited(t *testing.T) {
	q := wlQueriesOrSkip(t)
	// NewRedisRateCounter(nil) returns a nil RateCounter (fail-open), which
	// would never trip — so build the limiter with an explicit in-memory
	// counter instead to actually exercise the blocking path.
	rl := middleware.RateLimiter(true, memCounter{counts: map[string]int64{}}, "waitlist-test",
		middleware.RateLimit{Limit: 1, Window: time.Hour}, middleware.KeyIP, zerolog.Nop())

	app := fiber.New()
	NewHandler(q).Register(app, rl)

	require.Equal(t, 200, postWaitlist(app, "first@ratelimit.test"), "first request within limit must succeed")
	require.Equal(t, 429, postWaitlist(app, "second@ratelimit.test"), "request over the per-IP cap must be rate limited")
}

// With no Redis configured (the production fail-open default), the waitlist
// route must remain fully usable — the limiter is a defense, never a hard
// dependency.
func TestWaitlistJoinFailsOpenWithoutRedis(t *testing.T) {
	q := wlQueriesOrSkip(t)
	rl := middleware.RateLimiter(true, middleware.NewRedisRateCounter(nil), "waitlist-failopen",
		middleware.RateLimit{Limit: 1, Window: time.Hour}, middleware.KeyIP, zerolog.Nop())

	app := fiber.New()
	NewHandler(q).Register(app, rl)

	require.Equal(t, 200, postWaitlist(app, "a@failopen.test"))
	require.Equal(t, 200, postWaitlist(app, "b@failopen.test"), "nil Redis counter must fail open, not block")
}

// memCounter is a trivial in-memory middleware.RateCounter for tests.
type memCounter struct{ counts map[string]int64 }

func (m memCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	m.counts[key]++
	return m.counts[key], nil
}

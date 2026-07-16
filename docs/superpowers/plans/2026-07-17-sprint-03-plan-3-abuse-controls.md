# Sprint 3 — Plan 3: Abuse Controls — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Add Redis-backed (fail-open) rate limiting on abuse-prone routes, report/block between companies with an admin queue, block-aware matching, and the Sprint E2E #3.

**Architecture:** A reusable `pkg/middleware` rate limiter mirrors the gateway's proven fixed-window Redis counter (Incr + first-hit Expire, fail-open). Reports and blocks are a new migration + queries served from `internal/trust`. Blocked pairs are filtered out of RFQ matching (post-query, both directions) and supplier search. Quote-submit is already rate-limited per-credential by the gateway, so it needs no new limiter.

**Tech Stack:** Go / Fiber v2 / pgx v5 / sqlc / go-redis v9 / zerolog. Rate-limit tests use an in-memory fake counter (no miniredis dependency). DB tests via `TEST_DATABASE_URL`.

## Global Constraints

- Rate limiting **FAILS OPEN**: no Redis (nil counter), any Redis error, or `RATE_LIMIT_ENABLED=false` → the request proceeds. A Redis blip must never take the marketplace down. Over-limit → HTTP 429 `{code:"rate_limited", retry_after_seconds}` + `Retry-After` header.
- Rate-limit scopes/keys: signup by IP; RFQ-create / quote-message / verify-check / report by company (fall back to IP when unauthenticated).
- A company cannot report or block itself (reject 400). Block is one-directional in storage but filters matching in BOTH directions.
- Report reasons exactly `spam|fraud|abuse|inappropriate|other`; report status `open|reviewed|dismissed|actioned`.
- Reuse existing patterns: the gateway's `clientIP` logic (CF-Connecting-IP → X-Forwarded-For → c.IP()); admin routes under the platform-admin realm (`adminGuard`).
- Do not weaken any existing guard. Rate-limit middleware runs AFTER auth guards on authenticated routes (so `CompanyID` is populated) and standalone on public signup.

---

### Task 1: Rate-limit middleware + config parsing

**Files:**
- Create: `pkg/middleware/ratelimit.go`
- Create: `pkg/middleware/ratelimit_test.go`

**Interfaces:**
- Produces: `RateCounter` interface, `NewRedisRateCounter(*redis.Client) RateCounter` (nil-safe), `RateLimit{Limit int; Window time.Duration}`, `ParseRateLimit(s string, def RateLimit) RateLimit`, `KeyIP`, `KeyCompanyOrIP`, `ClientIP`, and `RateLimiter(enabled bool, counter RateCounter, scope string, lim RateLimit, key KeyFunc, logger zerolog.Logger) fiber.Handler`.

- [ ] **Step 1: Write the failing test**

`pkg/middleware/ratelimit_test.go`:
```go
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/middleware/ -run TestRateLimiter -v`
Expected: FAIL (compile error — symbols undefined).

- [ ] **Step 3: Implement the middleware**

`pkg/middleware/ratelimit.go`:
```go
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
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/middleware/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/middleware/ratelimit.go pkg/middleware/ratelimit_test.go
git commit -m "feat(trust): reusable fail-open Redis rate-limit middleware"
```

---

### Task 2: Wire rate limits + config

**Files:**
- Modify: `internal/config/config.go` (add rate-limit fields + defaults)
- Modify: `internal/httpapi/router.go` (build counters/limiters; thread into Registers; add `RateLimitEnabled` + raw limit strings to `httpapi.Config`)
- Modify: `cmd/api/main.go` (map new config → httpapi.Config)
- Modify: `internal/identity/handler.go` (`Register` accepts a signup limiter)
- Modify: `internal/rfq/handler.go` (`Register` accepts a create limiter)
- Modify: `internal/quotes/handler.go` (`Register` accepts a message limiter)
- Modify: `internal/trust/handler.go` (`Register` accepts a check limiter)
- Update the direct test callers of those `Register` methods (already identified — update exactly these):
  - `internal/trust/handler_test.go:38` — `NewHandler(svc, q).Register(app, verified, stubIdentity(...))` → insert a pass-through check limiter: `Register(app, verified, passRL, stubIdentity(...))` where `passRL := func(c *fiber.Ctx) error { return c.Next() }`.
  - `internal/rfq/handler_stream_test.go:26` and `:61` — `NewHandler(...).Register(app, passFn)` (currently `verified` only) → add the createRL arg: `Register(app, passFn, passFn)`.
  - `identity` and `quotes` packages have NO direct test callers of `Register` (verified) — only the router. `cmd/gateway/main.go` is unaffected (it registers the gateway handler, not these).

**Interfaces:**
- Consumes: `RateLimiter`, `ParseRateLimit`, key funcs (Task 1); `cfg.Redis`.

- [ ] **Step 1: Add config**

In `internal/config/config.go`, add fields:
```go
	// Abuse controls (Sprint 3). RateLimitEnabled is the master switch; each
	// limit is "N/DURATION" (e.g. "5/1h"). All fail OPEN.
	RateLimitEnabled     bool   `mapstructure:"RATE_LIMIT_ENABLED"`
	RateLimitSignup      string `mapstructure:"RATE_LIMIT_SIGNUP"`
	RateLimitRFQ         string `mapstructure:"RATE_LIMIT_RFQ"`
	RateLimitMessage     string `mapstructure:"RATE_LIMIT_MESSAGE"`
	RateLimitVerifyCheck string `mapstructure:"RATE_LIMIT_VERIFY_CHECK"`
	RateLimitReport      string `mapstructure:"RATE_LIMIT_REPORT"`
```
Defaults in `Load`:
```go
	v.SetDefault("RATE_LIMIT_ENABLED", true)
	v.SetDefault("RATE_LIMIT_SIGNUP", "5/1h")
	v.SetDefault("RATE_LIMIT_RFQ", "20/1h")
	v.SetDefault("RATE_LIMIT_MESSAGE", "120/1h")
	v.SetDefault("RATE_LIMIT_VERIFY_CHECK", "10/1h")
	v.SetDefault("RATE_LIMIT_REPORT", "30/1h")
```

- [ ] **Step 2: Add the same fields to `httpapi.Config`** and map them in `cmd/api/main.go` from `*config.Config`.

- [ ] **Step 3: Build limiters in the router**

In `internal/httpapi/router.go`, after `guard` is defined and before the handler registrations, add:
```go
	rlCounter := middleware.NewRedisRateCounter(cfg.Redis)
	rlOn := cfg.RateLimitEnabled
	mkRL := func(scope, spec string, def middleware.RateLimit, key middleware.KeyFunc) fiber.Handler {
		return middleware.RateLimiter(rlOn, rlCounter, scope, middleware.ParseRateLimit(spec, def), key, cfg.Logger)
	}
	rlSignup := mkRL("signup", cfg.RateLimitSignup, middleware.RateLimit{Limit: 5, Window: time.Hour}, middleware.KeyIP)
	rlRFQ := mkRL("rfq", cfg.RateLimitRFQ, middleware.RateLimit{Limit: 20, Window: time.Hour}, middleware.KeyCompanyOrIP)
	rlMessage := mkRL("message", cfg.RateLimitMessage, middleware.RateLimit{Limit: 120, Window: time.Hour}, middleware.KeyCompanyOrIP)
	rlVerify := mkRL("verify", cfg.RateLimitVerifyCheck, middleware.RateLimit{Limit: 10, Window: time.Hour}, middleware.KeyCompanyOrIP)
	rlReport := mkRL("report", cfg.RateLimitReport, middleware.RateLimit{Limit: 30, Window: time.Hour}, middleware.KeyCompanyOrIP)
```
`rlReport` is consumed in Task 3's report handler registration; keep it here (or move to Task 3 if the reports handler is registered later). Wire the others now:
- `identity.NewHandler(...).Register(f, rlSignup, guard...)`
- `rfq.NewHandler(...).Register(f, middleware.RequireVerified(q), rlRFQ, guard...)`
- `quotes.NewHandler(...).Register(f, rlMessage, guard...)`
- `trust.NewHandler(trustSvc, q).Register(f, middleware.RequireVerified(q), rlVerify, guard...)`

- [ ] **Step 4: Update `identity.Register`**

Change signature to `Register(app *fiber.App, signupRL fiber.Handler, guard ...fiber.Handler)` and apply the limiter to signup + login (both IP-abuse prone):
```go
	auth := app.Group("/v1/auth")
	auth.Post("/register", signupRL, h.register)
	auth.Post("/login", signupRL, h.login)
	auth.Post("/accept-invite", h.acceptInvite)
	auth.Post("/verify-email", h.verifyEmail)
```
Update the router call and any identity handler test that calls `Register` (grep `identity.NewHandler(`/`\.Register(` in `internal/identity`).

- [ ] **Step 5: Update `rfq.Register`**

Change to `Register(app *fiber.App, verified fiber.Handler, createRL fiber.Handler, guard ...fiber.Handler)`; apply `createRL` to the create route only:
```go
	g.Post("/", append(append([]fiber.Handler{}, verified, createRL), h.create)...)
```
Update router call + any rfq test that calls `Register`.

- [ ] **Step 6: Update `quotes.Register`**

Change to `Register(app *fiber.App, messageRL fiber.Handler, guard ...fiber.Handler)`; apply to the add-message route only:
```go
	g.Post("/:id/messages", messageRL, h.addMessage)
```
Update router call + any quotes test that calls `Register`.

- [ ] **Step 7: Update `trust.Handler.Register`**

Change to `Register(app *fiber.App, verified fiber.Handler, checkRL fiber.Handler, guard ...fiber.Handler)`; apply `checkRL` to the check route (append into its writeGuard chain before `h.check`). Update the router call and `internal/trust/handler_test.go` (its `NewTestApp` calls `Register(app, verified, stubIdentity(...))` — add a pass-through limiter arg, e.g. `func(c *fiber.Ctx) error { return c.Next() }`).

- [ ] **Step 8: Build + full suite**

Run: `go build ./... && go test ./...` (with `TEST_DATABASE_URL` set)
Expected: build clean; whole suite green. Fix any missed `Register` call site the compiler flags.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "feat(trust): wire fail-open rate limits on signup/rfq/message/verify + config"
```

---

### Task 3: Migration 018 + reports (create + admin queue)

**Files:**
- Create: `pkg/db/migrations/018_trust_abuse.up.sql`, `pkg/db/migrations/018_trust_abuse.down.sql`
- Create: `pkg/db/query/trust_abuse.sql`
- Modify (generated): `pkg/db/sqlc/*` via `sqlc generate`
- Create: `internal/trust/reports.go`, `internal/trust/reports_test.go`
- Modify: `internal/httpapi/router.go` (register reports handler; admin queue in admin block)

**Interfaces:**
- Produces tables `reports`, `blocks`; queries `CreateReport`, `ListReports`, `ResolveReport`, `CreateBlock`, `DeleteBlock`, `ListBlocksByCompany`, `ListBlockCompanyIDsFor`.
- Produces routes `POST /v1/reports`, admin `GET /admin/v1/reports`, `POST /admin/v1/reports/:id`.

- [ ] **Step 1: Write the up migration**

`pkg/db/migrations/018_trust_abuse.up.sql`:
```sql
-- Sprint 3 — Trust & Safety I: abuse reports + blocks.
CREATE TABLE reports (
    id                  TEXT PRIMARY KEY DEFAULT gen_ulid(),
    reporter_company_id TEXT NOT NULL REFERENCES companies(id),
    reported_company_id TEXT NOT NULL REFERENCES companies(id),
    reason              TEXT NOT NULL
        CHECK (reason IN ('spam','fraud','abuse','inappropriate','other')),
    detail              TEXT,
    status              TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open','reviewed','dismissed','actioned')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at         TIMESTAMPTZ,
    CHECK (reporter_company_id <> reported_company_id)
);
CREATE INDEX idx_reports_status ON reports (status, created_at DESC);
CREATE INDEX idx_reports_reported ON reports (reported_company_id);

CREATE TABLE blocks (
    id                 TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id         TEXT NOT NULL REFERENCES companies(id),
    blocked_company_id TEXT NOT NULL REFERENCES companies(id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (company_id, blocked_company_id),
    CHECK (company_id <> blocked_company_id)
);
CREATE INDEX idx_blocks_company ON blocks (company_id);
CREATE INDEX idx_blocks_blocked ON blocks (blocked_company_id);
```

`pkg/db/migrations/018_trust_abuse.down.sql`:
```sql
DROP TABLE IF EXISTS blocks;
DROP TABLE IF EXISTS reports;
```

- [ ] **Step 2: Apply up/down/up on the test DB**

Run:
```bash
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" down 1
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" version
```
Expected: ends at `18`, no dirty flag.

- [ ] **Step 3: Write the queries**

`pkg/db/query/trust_abuse.sql`:
```sql
-- name: CreateReport :one
INSERT INTO reports (reporter_company_id, reported_company_id, reason, detail)
VALUES (@reporter_company_id, @reported_company_id, @reason, @detail)
RETURNING *;

-- name: ListReports :many
SELECT r.*, rc.name AS reporter_name, tc.name AS reported_name
FROM reports r
JOIN companies rc ON rc.id = r.reporter_company_id
JOIN companies tc ON tc.id = r.reported_company_id
WHERE (sqlc.narg(status)::text IS NULL OR r.status = sqlc.narg(status)::text)
ORDER BY r.created_at DESC
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);

-- name: ResolveReport :one
UPDATE reports SET status = @status, resolved_at = NOW() WHERE id = @id RETURNING *;

-- name: CreateBlock :one
INSERT INTO blocks (company_id, blocked_company_id)
VALUES (@company_id, @blocked_company_id)
ON CONFLICT (company_id, blocked_company_id) DO NOTHING
RETURNING *;

-- name: DeleteBlock :exec
DELETE FROM blocks WHERE company_id = @company_id AND blocked_company_id = @blocked_company_id;

-- name: ListBlocksByCompany :many
SELECT b.*, c.name AS blocked_name
FROM blocks b JOIN companies c ON c.id = b.blocked_company_id
WHERE b.company_id = @company_id
ORDER BY b.created_at DESC;

-- name: ListBlockCompanyIDsFor :many
-- Every company in a block relationship with @company_id, either direction.
SELECT blocked_company_id AS company_id FROM blocks WHERE company_id = @company_id
UNION
SELECT company_id AS company_id FROM blocks WHERE blocked_company_id = @company_id;
```
Run `sqlc generate`.

- [ ] **Step 4: Write the reports test**

`internal/trust/reports_test.go`:
```go
package trust

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

func TestCreateReportAndAdminResolve(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	reporter := makeCompany(t, q, "reporter-co")
	reported := makeCompany(t, q, "reported-co")

	app := fiber.New()
	NewReportsHandler(q).Register(app, stubIdentity(reporter, "owner"), func(c *fiber.Ctx) error { return c.Next() })
	NewReportsHandler(q).RegisterAdmin(app, func(c *fiber.Ctx) error { return c.Next() })

	code, out := doJSON(t, app, "POST", "/v1/reports", map[string]string{"reported_company_id": reported, "reason": "spam", "detail": "spammy"})
	require.Equal(t, 200, code)
	id := out["id"].(string)
	require.NotEmpty(t, id)

	code, list := doJSON(t, app, "GET", "/admin/v1/reports?status=open&limit=100", nil)
	require.Equal(t, 200, code)
	require.Contains(t, list, "reports")

	code, res := doJSON(t, app, "POST", "/admin/v1/reports/"+id, map[string]string{"status": "dismissed"})
	require.Equal(t, 200, code)
	require.Equal(t, "dismissed", res["status"])
}

func TestCannotReportSelf(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	me := makeCompany(t, q, "self-report-co")
	app := fiber.New()
	NewReportsHandler(q).Register(app, stubIdentity(me, "owner"), func(c *fiber.Ctx) error { return c.Next() })
	code, _ := doJSON(t, app, "POST", "/v1/reports", map[string]string{"reported_company_id": me, "reason": "spam"})
	require.Equal(t, 400, code)
}
```

- [ ] **Step 5: Run to verify it fails**

Run: `go test ./internal/trust/ -run TestCreateReport -v`
Expected: FAIL (`NewReportsHandler` undefined).

- [ ] **Step 6: Implement the reports handler**

`internal/trust/reports.go`:
```go
package trust

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// ReportsHandler serves company-to-company abuse reports + the admin queue.
type ReportsHandler struct {
	q *sqlc.Queries
}

func NewReportsHandler(q *sqlc.Queries) *ReportsHandler {
	return &ReportsHandler{q: q}
}

// Register mounts POST /v1/reports (authed, verified, rate-limited).
func (h *ReportsHandler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Post("/v1/reports", append(append([]fiber.Handler{}, guard...), h.create)...)
}

// RegisterAdmin mounts the admin queue under the platform-admin realm.
func (h *ReportsHandler) RegisterAdmin(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1", adminGuard)
	g.Get("/reports", h.list)
	g.Post("/reports/:id", h.resolve)
}

var validReason = map[string]bool{"spam": true, "fraud": true, "abuse": true, "inappropriate": true, "other": true}

type reportReq struct {
	ReportedCompanyID string `json:"reported_company_id"`
	Reason            string `json:"reason"`
	Detail            string `json:"detail"`
}

func (h *ReportsHandler) create(c *fiber.Ctx) error {
	var req reportReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	me := middleware.CompanyID(c)
	if req.ReportedCompanyID == "" || req.ReportedCompanyID == me {
		return fiber.NewError(fiber.StatusBadRequest, "invalid reported company")
	}
	if !validReason[req.Reason] {
		return fiber.NewError(fiber.StatusBadRequest, "invalid reason")
	}
	var detail *string
	if req.Detail != "" {
		detail = &req.Detail
	}
	rep, err := h.q.CreateReport(c.Context(), sqlc.CreateReportParams{
		ReporterCompanyID: me, ReportedCompanyID: req.ReportedCompanyID, Reason: req.Reason, Detail: detail,
	})
	if err != nil {
		return err
	}
	return c.JSON(rep)
}

func (h *ReportsHandler) list(c *fiber.Ctx) error {
	var status *string
	if s := c.Query("status"); s != "" {
		status = &s
	}
	limit := parseIntDefault(c.Query("limit"), 50, 500)
	offset := parseIntDefault(c.Query("offset"), 0, 1_000_000)
	rows, err := h.q.ListReports(c.Context(), sqlc.ListReportsParams{Status: status, Lim: int32(limit), Off: int32(offset)})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"reports": rows, "count": len(rows)})
}

var validResolution = map[string]bool{"reviewed": true, "dismissed": true, "actioned": true, "open": true}

func (h *ReportsHandler) resolve(c *fiber.Ctx) error {
	var body struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&body); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if !validResolution[body.Status] {
		return fiber.NewError(fiber.StatusBadRequest, "invalid status")
	}
	rep, err := h.q.ResolveReport(c.Context(), sqlc.ResolveReportParams{ID: c.Params("id"), Status: body.Status})
	if err != nil {
		return err
	}
	return c.JSON(rep)
}
```
(`parseIntDefault` already exists in the `trust` package from Plan 1's `admin.go` — reuse it, don't redefine.)

- [ ] **Step 7: Register in the router**

In `internal/httpapi/router.go`: mount the report create route with the report limiter + verified + guard:
```go
	reports := trust.NewReportsHandler(q)
	reports.Register(f, append(append([]fiber.Handler{}, guard...), middleware.RequireVerified(q), rlReport)...)
```
and inside the admin block: `reports.RegisterAdmin(f, adminGuard)`.

- [ ] **Step 8: Run tests + build**

Run: `go build ./... && go test ./internal/trust/ -v`
Expected: build clean; PASS.

- [ ] **Step 9: Commit**

```bash
git add pkg/db/migrations/018_trust_abuse.up.sql pkg/db/migrations/018_trust_abuse.down.sql pkg/db/query/trust_abuse.sql pkg/db/sqlc internal/trust/reports.go internal/trust/reports_test.go internal/httpapi/router.go
git commit -m "feat(trust): migration 018 + abuse reports (create + admin queue)"
```

---

### Task 4: Blocks + block-aware matching & search

**Files:**
- Create: `internal/trust/blocks.go`, `internal/trust/blocks_test.go`
- Modify: `internal/rfq/service.go` (`findMatches` filters blocked pairs)
- Modify: `internal/suppliers/handler.go` (`search` hides blocked companies)
- Modify: `internal/httpapi/router.go` (register blocks handler)

**Interfaces:**
- Consumes: `ListBlockCompanyIDsFor` (Task 3).
- Produces: `POST /v1/blocks`, `DELETE /v1/blocks/:blockedID`, `GET /v1/blocks`.

- [ ] **Step 1: Write the blocks handler**

`internal/trust/blocks.go`:
```go
package trust

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// BlocksHandler serves company block/unblock/list.
type BlocksHandler struct {
	q *sqlc.Queries
}

func NewBlocksHandler(q *sqlc.Queries) *BlocksHandler {
	return &BlocksHandler{q: q}
}

func (h *BlocksHandler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/blocks", guard...)
	g.Get("/", h.list)
	g.Post("/", h.create)
	g.Delete("/:blockedID", h.remove)
}

type blockReq struct {
	BlockedCompanyID string `json:"blocked_company_id"`
}

func (h *BlocksHandler) create(c *fiber.Ctx) error {
	var req blockReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	me := middleware.CompanyID(c)
	if req.BlockedCompanyID == "" || req.BlockedCompanyID == me {
		return fiber.NewError(fiber.StatusBadRequest, "invalid blocked company")
	}
	// ON CONFLICT DO NOTHING returns no row on a duplicate; treat as success.
	_, err := h.q.CreateBlock(c.Context(), sqlc.CreateBlockParams{CompanyID: me, BlockedCompanyID: req.BlockedCompanyID})
	if err != nil && err.Error() != "no rows in result set" {
		return err
	}
	return c.JSON(fiber.Map{"blocked_company_id": req.BlockedCompanyID, "status": "blocked"})
}

func (h *BlocksHandler) remove(c *fiber.Ctx) error {
	if err := h.q.DeleteBlock(c.Context(), sqlc.DeleteBlockParams{
		CompanyID: middleware.CompanyID(c), BlockedCompanyID: c.Params("blockedID"),
	}); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *BlocksHandler) list(c *fiber.Ctx) error {
	rows, err := h.q.ListBlocksByCompany(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"blocks": rows, "count": len(rows)})
}
```
Note: the `CreateBlock` duplicate path — `ON CONFLICT DO NOTHING ... RETURNING *` returns `pgx.ErrNoRows` on conflict. Match that precisely with `errors.Is(err, pgx.ErrNoRows)` (import `errors` + `github.com/jackc/pgx/v5`) rather than string-matching; adjust the implementation accordingly.

- [ ] **Step 2: Write the block-filter test (matching)**

`internal/trust/blocks_test.go`:
```go
package trust

import (
	"context"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestListBlockCompanyIDsBothDirections(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	ctx := context.Background()
	a := makeCompany(t, q, "block-a")
	b := makeCompany(t, q, "block-b")
	x := makeCompany(t, q, "block-x")

	_, err := q.CreateBlock(ctx, sqlc.CreateBlockParams{CompanyID: a, BlockedCompanyID: b})
	require.NoError(t, err)
	_, err = q.CreateBlock(ctx, sqlc.CreateBlockParams{CompanyID: x, BlockedCompanyID: a})
	require.NoError(t, err)

	ids, err := q.ListBlockCompanyIDsFor(ctx, a)
	require.NoError(t, err)
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	require.True(t, set[b], "a blocked b → b present")
	require.True(t, set[x], "x blocked a → x present (reverse direction)")
}

func TestCreateBlockIdempotent(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	me := makeCompany(t, q, "block-idem-me")
	other := makeCompany(t, q, "block-idem-other")
	app := fiber.New()
	NewBlocksHandler(q).Register(app, stubIdentity(me, "owner"))
	code, _ := doJSON(t, app, "POST", "/v1/blocks/", map[string]string{"blocked_company_id": other})
	require.Equal(t, 200, code)
	code, _ = doJSON(t, app, "POST", "/v1/blocks/", map[string]string{"blocked_company_id": other})
	require.Equal(t, 200, code, "duplicate block is a no-op success, not 500")
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/trust/ -run TestListBlockCompanyIDs -v`
Expected: FAIL (blocks handler / query not present until Task 3's sqlc gen — if Task 3 ran, the query exists; the handler is new here).

- [ ] **Step 4: Filter blocked pairs in matching**

In `internal/rfq/service.go` `findMatches`, after building the `out` slice (before the sort), load and apply the block set:
```go
	blockedRows, err := s.q.ListBlockCompanyIDsFor(ctx, buyerCompanyID)
	if err != nil {
		return nil, err
	}
	if len(blockedRows) > 0 {
		blocked := make(map[string]struct{}, len(blockedRows))
		for _, id := range blockedRows {
			blocked[id] = struct{}{}
		}
		kept := out[:0]
		for _, m := range out {
			if _, isBlocked := blocked[m.SellerCompanyID]; !isBlocked {
				kept = append(kept, m)
			}
		}
		out = kept
	}
```

- [ ] **Step 5: Hide blocked companies in supplier search**

In `internal/suppliers/handler.go` `search`, after fetching `rows`, filter out companies blocked in either direction with `middleware.CompanyID(c)`:
```go
	blockedRows, err := h.q.ListBlockCompanyIDsFor(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	if len(blockedRows) > 0 {
		blocked := make(map[string]struct{}, len(blockedRows))
		for _, id := range blockedRows {
			blocked[id] = struct{}{}
		}
		filtered := rows[:0]
		for _, r := range rows {
			if _, b := blocked[r.CompanyID]; !b {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
```
(Confirm `SearchSuppliersRow` has `CompanyID` — from Plan 2 it selects `sp.*` which includes `company_id`.)

- [ ] **Step 6: Register the blocks handler**

In `internal/httpapi/router.go`: `trust.NewBlocksHandler(q).Register(f, guard...)`.

- [ ] **Step 7: Run tests + build + full suite**

Run: `go build ./... && go test ./...`
Expected: build clean; whole suite green.

- [ ] **Step 8: Commit**

```bash
git add internal/trust/blocks.go internal/trust/blocks_test.go internal/rfq/service.go internal/suppliers/handler.go internal/httpapi/router.go
git commit -m "feat(trust): company blocks + block-aware matching and search"
```

---

### Task 5: E2E #3 — verify → badge/match; block → excluded

**Files:**
- Create: `internal/trust/e2e_test.go`

**Interfaces:**
- Consumes: verification `Service` (Plan 1), `MatchSuppliers` gate, blocks.

- [ ] **Step 1: Write the E2E test**

`internal/trust/e2e_test.go`:
```go
package trust

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #3: an unverified seller is excluded from the gated match pool; after
// verification it is included; a block removes it again.
func TestE2EVerifyMatchAndBlock(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()
	res := &fakeResolver{records: map[string][]string{}}
	svc := NewService(q, pool, res, zerolog.Nop())

	buyer := makeCompany(t, q, "e2e-buyer")
	seller := makeCompany(t, q, "e2e-seller")
	_, err := q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: seller.ID, Description: sptr("e2e widgets"), Categories: []string{"e2ewidgets"}, Location: sptr("SF"),
	})
	require.NoError(t, err)
	const domain = "e2e-seller.example"
	releaseDomain(t, pool, domain)
	t.Cleanup(func() { releaseDomain(t, pool, domain) })

	inPool := func(verifiedOnly bool) bool {
		rows, err := q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{
			ExcludeCompany: buyer, Needle: "e2ewidgets", VerifiedOnly: verifiedOnly, Lim: 50,
		})
		require.NoError(t, err)
		for _, r := range rows {
			if r.CompanyID == seller {
				return true
			}
		}
		return false
	}

	// Gate ON, unverified → excluded.
	require.False(t, inPool(true), "unverified seller excluded when gate on")
	require.True(t, inPool(false), "gate off → present regardless")

	// Verify via DNS-TXT, then included with gate ON.
	ch, err := svc.Claim(ctx, seller, domain)
	require.NoError(t, err)
	res.records[ch.RecordName] = []string{ch.RecordValue}
	r, err := svc.Check(ctx, seller, domain)
	require.NoError(t, err)
	require.True(t, r.Verified)
	require.True(t, inPool(true), "verified seller included when gate on")

	// Buyer blocks the seller → excluded in either direction.
	_, err = q.CreateBlock(ctx, sqlc.CreateBlockParams{CompanyID: buyer, BlockedCompanyID: seller})
	require.NoError(t, err)
	ids, err := q.ListBlockCompanyIDsFor(ctx, buyer)
	require.NoError(t, err)
	require.Contains(t, ids, seller, "block relationship visible to the match filter")
}

func sptr(s string) *string { return &s }
```
Note: if `sptr`/`sp`/`strptr` already exist in the `trust` test package (Plan 1 added `strptr`), reuse the existing helper and delete this duplicate to avoid a redeclaration compile error. Grep `func sptr\|func strptr\|func sp(` in `internal/trust`.

- [ ] **Step 2: Run the E2E**

Run: `go test ./internal/trust/ -run TestE2EVerifyMatchAndBlock -v`
Expected: PASS.

- [ ] **Step 3: Full suite + commit**

Run: `go build ./... && go test ./...`
Expected: green.
```bash
git add internal/trust/e2e_test.go
git commit -m "test(trust): E2E #3 — verify unlocks gated match pool; block excludes"
```

---

## Deferred / follow-ups

- Free-text content-moderation guard (profanity/PII/link) — later sprint.
- Rate-limit tuning / per-endpoint 429 telemetry — S10 observability.
- Reported-company auto-actions (auto-hide after N actioned reports) — later.
- Frontend report/block affordances + 429 handling — Plan 4.

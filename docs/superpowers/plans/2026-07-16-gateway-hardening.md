# Gateway Hardening (Rate Limiting + Credential Lifecycle) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-credential + per-IP rate limiting to the agent gateway and give agent credentials a TTL with zero-downtime rotation.

**Architecture:** Credential expiry/TTL/grace math lives in small pure helpers (unit-tested, no DB). The gateway enforces expiry + rate limits inside the single `authenticate()` choke point, with a `counter` interface over Redis so the limiter is fail-open and fake-able in tests. Issuance/rotation/list live in the API process (`internal/agent`).

**Tech Stack:** Go (Fiber, pgx/sqlc, go-redis, zerolog), Postgres, Redis; + a one-line agent change (Bun/TypeScript).

## Global Constraints

- **Do not break the live agent:** migration grandfathers existing active credentials to `NOW() + INTERVAL '90 days'` (fixed 90d — migrations can't read env), never retroactively expired.
- `expires_at IS NULL` ⇒ never expires. `AGENT_CRED_TTL_DAYS = 0` ⇒ issue with NULL expiry (escape hatch).
- Rate limiting is **fail-open**: `h.redis == nil` or any Redis error ⇒ allow the request. It must never return 5xx or block the gateway.
- Defaults: `AGENT_CRED_TTL_DAYS=90`, `AGENT_CRED_ROTATE_GRACE_DAYS=7`, `AGENT_RL_PER_MIN=120`, `AGENT_AUTHFAIL_MAX=20`, `AGENT_AUTHFAIL_WINDOW_SEC=300`.
- Client IP resolution order: `CF-Connecting-IP` → first hop of `X-Forwarded-For` → `c.IP()` (deployment is behind Cloudflare).
- Credential expiry is **not** counted as a failed-auth attempt (don't let a rotation-lapse trip the brute-force block).
- Go on PATH: `export PATH="$HOME/.local/bin:$HOME/go-sdk/go/bin:$HOME/go/bin:$PATH"` (has `go`, `sqlc`, `migrate`). Test DB (for any integration test): `TEST_DATABASE_URL=postgres://noorluna:REDACTED-DB-PASSWORD@127.0.0.1:5432/tradebridge_core_test?sslmode=disable`.

---

### Task 1: Schema migration + sqlc queries

**Files:**
- Create: `pkg/db/migrations/013_credential_expiry.up.sql`, `pkg/db/migrations/013_credential_expiry.down.sql`
- Modify: `pkg/db/query/agent_credentials.sql`
- Regenerate: `pkg/db/sqlc/*` via `sqlc generate`

**Interfaces:**
- Produces: `agent_credentials.expires_at` column; `CreateAgentCredentialParams` gains `ExpiresAt pgtype.Timestamptz`; new `SetAgentCredentialExpiry(ctx, {ID, CompanyID, ExpiresAt})`; `AgentCredential` struct gains `ExpiresAt pgtype.Timestamptz` (via `SELECT *`).

- [ ] **Step 1: Write the migration**

`pkg/db/migrations/013_credential_expiry.up.sql`:

```sql
-- Agent credentials gain an optional expiry (NULL = never). Grandfather existing
-- ACTIVE credentials onto the policy with a full 90-day runway from deploy so the
-- live agent keeps working (migrations are static SQL and cannot read the env TTL).
ALTER TABLE agent_credentials ADD COLUMN expires_at TIMESTAMPTZ;

UPDATE agent_credentials
   SET expires_at = NOW() + INTERVAL '90 days'
 WHERE expires_at IS NULL AND status = 'active';
```

`pkg/db/migrations/013_credential_expiry.down.sql`:

```sql
ALTER TABLE agent_credentials DROP COLUMN expires_at;
```

- [ ] **Step 2: Update the queries**

In `pkg/db/query/agent_credentials.sql`, change `CreateAgentCredential` and add `SetAgentCredentialExpiry` (leave the other queries as-is):

```sql
-- name: CreateAgentCredential :one
INSERT INTO agent_credentials (company_id, label, secret_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: SetAgentCredentialExpiry :exec
UPDATE agent_credentials SET expires_at = $3
WHERE id = $1 AND company_id = $2;
```

- [ ] **Step 3: Regenerate sqlc + build**

Run:
```bash
export PATH="$HOME/.local/bin:$HOME/go-sdk/go/bin:$HOME/go/bin:$PATH"
sqlc generate && go build ./...
```
Expected: `sqlc generate` succeeds; build fails ONLY at the existing `CreateAgentCredential(...)` call site in `internal/agent/handler.go` (missing `ExpiresAt`). That call is updated in Task 2 — to keep this task's build green, pass `ExpiresAt: pgtype.Timestamptz{}` at that one call site now (import `github.com/jackc/pgx/v5/pgtype`), a NULL placeholder Task 2 replaces.

After that placeholder edit: `go build ./...` clean.

- [ ] **Step 4: Verify the migration up/down against the test DB**

Run:
```bash
export PATH="$HOME/.local/bin:$HOME/go-sdk/go/bin:$HOME/go/bin:$PATH"
TDB="postgres://noorluna:REDACTED-DB-PASSWORD@127.0.0.1:5432/tradebridge_core_test?sslmode=disable"
migrate -path pkg/db/migrations -database "$TDB" up
migrate -path pkg/db/migrations -database "$TDB" down 1 && migrate -path pkg/db/migrations -database "$TDB" up
```
Expected: applies and rolls back cleanly (no errors), leaving the column present.

- [ ] **Step 5: Commit**

```bash
git add pkg/db/migrations/013_credential_expiry.up.sql pkg/db/migrations/013_credential_expiry.down.sql pkg/db/query/agent_credentials.sql pkg/db/sqlc/ internal/agent/handler.go
git commit -m "feat(db): agent_credentials.expires_at + SetAgentCredentialExpiry (migration 013)"
```

---

### Task 2: Config + credential TTL on issuance + expiry enforcement

**Files:**
- Modify: `internal/config/config.go` (5 fields + defaults)
- Create: `internal/agent/expiry.go` + `internal/agent/expiry_test.go` (`credExpiry` helper)
- Modify: `internal/agent/handler.go` (`Config`, `NewHandler` signature, `issue` sets expiry, `list` returns expiry)
- Create: `internal/gateway/expiry.go` + `internal/gateway/expiry_test.go` (`credExpired` helper)
- Modify: `internal/gateway/gateway.go` (`authenticate` rejects expired)
- Modify: `internal/httpapi/router.go` + `cmd/api/main.go` (thread TTL/grace into `agent.NewHandler`)

**Interfaces:**
- Consumes: `pgtype.Timestamptz`, `AgentCredential.ExpiresAt`, `CreateAgentCredentialParams.ExpiresAt` (Task 1).
- Produces:
  - `agent.Config{ TTLDays int; GraceDays int }`, `agent.NewHandler(q *sqlc.Queries, cfg Config) *Handler`
  - `agent.credExpiry(now time.Time, ttlDays int) pgtype.Timestamptz`
  - `gateway.credExpired(expiresAt pgtype.Timestamptz, now time.Time) bool`
  - config fields `AgentCredTTLDays`, `AgentCredRotateGraceDays`, `AgentRLPerMin`, `AgentAuthfailMax`, `AgentAuthfailWindowSec`

- [ ] **Step 1: Write the failing helper tests**

`internal/agent/expiry_test.go`:

```go
package agent

import (
	"testing"
	"time"
)

func TestCredExpiry(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	e := credExpiry(now, 90)
	if !e.Valid || !e.Time.Equal(now.Add(90*24*time.Hour)) {
		t.Fatalf("ttl=90 → %+v", e)
	}
	if z := credExpiry(now, 0); z.Valid {
		t.Fatalf("ttl=0 must be NULL (never), got %+v", z)
	}
	if z := credExpiry(now, -5); z.Valid {
		t.Fatalf("ttl<0 must be NULL, got %+v", z)
	}
}
```

`internal/gateway/expiry_test.go`:

```go
package gateway

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCredExpired(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	past := pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
	future := pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}
	never := pgtype.Timestamptz{} // NULL
	if !credExpired(past, now) {
		t.Fatal("past expiry should be expired")
	}
	if credExpired(future, now) {
		t.Fatal("future expiry should not be expired")
	}
	if credExpired(never, now) {
		t.Fatal("NULL expiry never expires")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `export PATH="$HOME/.local/bin:$HOME/go-sdk/go/bin:$HOME/go/bin:$PATH" && go test ./internal/agent/ ./internal/gateway/ -run 'TestCredExpiry|TestCredExpired'`
Expected: FAIL — `credExpiry`/`credExpired` undefined.

- [ ] **Step 3: Write the helpers**

`internal/agent/expiry.go`:

```go
package agent

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// credExpiry returns the expiry for a newly issued credential: now + ttlDays,
// or NULL (never) when ttlDays <= 0.
func credExpiry(now time.Time, ttlDays int) pgtype.Timestamptz {
	if ttlDays <= 0 {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: now.Add(time.Duration(ttlDays) * 24 * time.Hour), Valid: true}
}
```

`internal/gateway/expiry.go`:

```go
package gateway

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// credExpired reports whether a credential's expiry has passed. A NULL expiry
// (Valid == false) never expires.
func credExpired(expiresAt pgtype.Timestamptz, now time.Time) bool {
	return expiresAt.Valid && !expiresAt.Time.After(now)
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/agent/ ./internal/gateway/ -run 'TestCredExpiry|TestCredExpired'`
Expected: PASS.

- [ ] **Step 5: Add config fields + defaults**

In `internal/config/config.go`, add to the `Config` struct (near `RFQTTLDays`):

```go
	AgentCredTTLDays         int `mapstructure:"AGENT_CRED_TTL_DAYS"`
	AgentCredRotateGraceDays int `mapstructure:"AGENT_CRED_ROTATE_GRACE_DAYS"`
	AgentRLPerMin            int `mapstructure:"AGENT_RL_PER_MIN"`
	AgentAuthfailMax         int `mapstructure:"AGENT_AUTHFAIL_MAX"`
	AgentAuthfailWindowSec   int `mapstructure:"AGENT_AUTHFAIL_WINDOW_SEC"`
```

and the defaults (near `RFQ_SWEEP_SECONDS`):

```go
	v.SetDefault("AGENT_CRED_TTL_DAYS", 90)
	v.SetDefault("AGENT_CRED_ROTATE_GRACE_DAYS", 7)
	v.SetDefault("AGENT_RL_PER_MIN", 120)
	v.SetDefault("AGENT_AUTHFAIL_MAX", 20)
	v.SetDefault("AGENT_AUTHFAIL_WINDOW_SEC", 300)
```

- [ ] **Step 6: Thread TTL/grace into the agent handler + wire issuance/expiry**

In `internal/agent/handler.go`:

Add `"time"` and `"github.com/jackc/pgx/v5/pgtype"` imports. Add config + new constructor:

```go
// Config carries the credential-lifecycle policy for issuance + rotation.
type Config struct {
	TTLDays   int
	GraceDays int
}

// Handler serves agent-credential issuance + management routes.
type Handler struct {
	q   *sqlc.Queries
	cfg Config
}

// NewHandler builds the agent-credentials handler over the query layer.
func NewHandler(q *sqlc.Queries, cfg Config) *Handler {
	return &Handler{q: q, cfg: cfg}
}
```

In `issue`, set the expiry on create (replace the `CreateAgentCredential` call):

```go
	cred, err := h.q.CreateAgentCredential(c.Context(), sqlc.CreateAgentCredentialParams{
		CompanyID:  middleware.CompanyID(c),
		Label:      req.Label,
		SecretHash: hash,
		ExpiresAt:  credExpiry(time.Now(), h.cfg.TTLDays),
	})
```

Extend `credResp` + `list` to surface expiry:

```go
type credResp struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	LastSeen  any    `json:"last_seen"`
	ExpiresAt any    `json:"expires_at"`
	CreatedAt any    `json:"created_at"`
}
```

and in the `list` loop:

```go
	for i, cr := range creds {
		var last, exp any
		if cr.LastSeen.Valid {
			last = cr.LastSeen.Time
		}
		if cr.ExpiresAt.Valid {
			exp = cr.ExpiresAt.Time
		}
		out[i] = credResp{ID: cr.ID, Label: cr.Label, Status: cr.Status, LastSeen: last, ExpiresAt: exp, CreatedAt: cr.CreatedAt}
	}
```

In `internal/gateway/gateway.go` `authenticate`, add the expiry check AFTER the status+password check (and add `"time"` import):

```go
	if cred.Status != "active" || !identity.CheckPassword(cred.SecretHash, secret) {
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "invalid agent credential")
	}
	if credExpired(cred.ExpiresAt, time.Now()) {
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "credential expired")
	}
	return authedAgent{CredentialID: cred.ID, CompanyID: cred.CompanyID}, nil
```

- [ ] **Step 7: Wire the config through the API process**

In `internal/httpapi/router.go`: add `AgentCredTTLDays int` and `AgentCredRotateGraceDays int` to the router `Config` struct, and change the `agent.NewHandler` call (line ~108):

```go
	agent.NewHandler(q, agent.Config{TTLDays: cfg.AgentCredTTLDays, GraceDays: cfg.AgentCredRotateGraceDays}).Register(f, guard...)
```

In `cmd/api/main.go`, set those two fields on the router config from `app.Config.AgentCredTTLDays` / `app.Config.AgentCredRotateGraceDays` (alongside the existing `RFQTTLDays:` line).

- [ ] **Step 8: Run tests + build**

Run: `go test ./internal/agent/ ./internal/gateway/ ./internal/config/ && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 9: Commit**

```bash
git add internal/config/config.go internal/agent/ internal/gateway/expiry.go internal/gateway/expiry_test.go internal/gateway/gateway.go internal/httpapi/router.go cmd/api/main.go
git commit -m "feat(agent): credential TTL on issue + expiry enforcement + config"
```

---

### Task 3: Credential rotation endpoint

**Files:**
- Modify: `internal/agent/expiry.go` + `internal/agent/expiry_test.go` (add `graceExpiry`)
- Modify: `internal/agent/handler.go` (route + `rotate` handler)

**Interfaces:**
- Consumes: `credExpiry` (Task 2), `SetAgentCredentialExpiry` (Task 1), `GetAgentCredentialByID`, `CreateAgentCredential`.
- Produces: `agent.graceExpiry(existing pgtype.Timestamptz, now time.Time, graceDays int) pgtype.Timestamptz`; route `POST /v1/agent-credentials/:id/rotate`.

- [ ] **Step 1: Write the failing helper test**

Append to `internal/agent/expiry_test.go`:

```go
func TestGraceExpiry(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	// No existing expiry (NULL) → now + grace.
	g := graceExpiry(pgtype.Timestamptz{}, now, 7)
	if !g.Valid || !g.Time.Equal(now.Add(7*24*time.Hour)) {
		t.Fatalf("null existing → %+v", g)
	}
	// Existing expiry SOONER than now+grace → keep the sooner one (never extend).
	soon := pgtype.Timestamptz{Time: now.Add(24 * time.Hour), Valid: true}
	if g := graceExpiry(soon, now, 7); !g.Time.Equal(soon.Time) {
		t.Fatalf("must not extend a sooner expiry: %+v", g)
	}
	// Existing expiry LATER than now+grace → clamp down to now+grace.
	far := pgtype.Timestamptz{Time: now.Add(30 * 24 * time.Hour), Valid: true}
	if g := graceExpiry(far, now, 7); !g.Time.Equal(now.Add(7 * 24 * time.Hour)) {
		t.Fatalf("must clamp a later expiry to grace: %+v", g)
	}
}
```

(Add `"github.com/jackc/pgx/v5/pgtype"` to the test imports if not present.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agent/ -run TestGraceExpiry`
Expected: FAIL — `graceExpiry` undefined.

- [ ] **Step 3: Write `graceExpiry`**

Append to `internal/agent/expiry.go`:

```go
// graceExpiry returns when a rotated-away credential's OLD secret should stop
// working: now + graceDays, but never LATER than its current expiry (rotation
// only ever shortens, never extends, an old secret's life).
func graceExpiry(existing pgtype.Timestamptz, now time.Time, graceDays int) pgtype.Timestamptz {
	cand := pgtype.Timestamptz{Time: now.Add(time.Duration(graceDays) * 24 * time.Hour), Valid: true}
	if existing.Valid && existing.Time.Before(cand.Time) {
		return existing
	}
	return cand
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agent/ -run TestGraceExpiry`
Expected: PASS.

- [ ] **Step 5: Add the rotate route + handler**

In `internal/agent/handler.go` `Register`, add the route (owner/admin, like issue):

```go
	g.Post("/:id/rotate", middleware.RequireRole("owner", "admin"), h.rotate)
```

Add `"errors"` and `"github.com/jackc/pgx/v5"` imports, then the handler:

```go
// rotate issues a fresh secret (new credential) for the same company/label and
// puts the old secret on a grace clock, so the operator can update the agent's
// .env within the grace window with zero downtime. The old secret keeps working
// until then. Returns the new full secret exactly once.
func (h *Handler) rotate(c *fiber.Ctx) error {
	companyID := middleware.CompanyID(c)
	old, err := h.q.GetAgentCredentialByID(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && old.CompanyID != companyID) {
		return fiber.NewError(fiber.StatusNotFound, "credential not found")
	} else if err != nil {
		return err
	}
	if old.Status != "active" {
		return fiber.NewError(fiber.StatusConflict, "credential is not active")
	}

	rawSecret, err := randomHex(32)
	if err != nil {
		return err
	}
	hash, err := identity.HashPassword(rawSecret)
	if err != nil {
		return err
	}
	newCred, err := h.q.CreateAgentCredential(c.Context(), sqlc.CreateAgentCredentialParams{
		CompanyID:  companyID,
		Label:      old.Label,
		SecretHash: hash,
		ExpiresAt:  credExpiry(time.Now(), h.cfg.TTLDays),
	})
	if err != nil {
		return err
	}
	// Old secret keeps authenticating until now+grace (never extended).
	if err := h.q.SetAgentCredentialExpiry(c.Context(), sqlc.SetAgentCredentialExpiryParams{
		ID:        old.ID,
		CompanyID: companyID,
		ExpiresAt: graceExpiry(old.ExpiresAt, time.Now(), h.cfg.GraceDays),
	}); err != nil {
		return err
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":     newCred.ID,
		"label":  newCred.Label,
		"secret": newCred.ID + "." + rawSecret,
	})
}
```

- [ ] **Step 6: Run tests + build**

Run: `go test ./internal/agent/ && go build ./...`
Expected: PASS + clean build. (The rotate handler's DB path is exercised in live-verify — the pure grace/TTL math is unit-tested above.)

- [ ] **Step 7: Commit**

```bash
git add internal/agent/expiry.go internal/agent/expiry_test.go internal/agent/handler.go
git commit -m "feat(agent): POST /v1/agent-credentials/:id/rotate with grace window"
```

---

### Task 4: Gateway rate limiting

**Files:**
- Create: `internal/gateway/ratelimit.go` + `internal/gateway/ratelimit_test.go`
- Modify: `internal/gateway/gateway.go` (`Handler` gets `rl *rateLimiter`; `NewHandler` signature; `authenticate` integration; `clientIP`)
- Modify: `cmd/gateway/main.go` (pass `RateLimitConfig`)

**Interfaces:**
- Consumes: `*redis.Client`, config RL fields.
- Produces:
  - `gateway.RateLimitConfig{ PerMin, AuthfailMax, AuthfailWindowSec int }`
  - `gateway.NewHandler(q *sqlc.Queries, rdb *redis.Client, store *attachments.Store, logger zerolog.Logger, rl RateLimitConfig) *Handler`
  - internal: `counter` interface, `redisCounter`, `rateLimiter{ allowCred, authFailBlocked, recordAuthFailure }`, `clientIP`

- [ ] **Step 1: Write the failing tests**

`internal/gateway/ratelimit_test.go`:

```go
package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeCounter is an in-memory counter for testing the limiter logic.
type fakeCounter struct {
	vals map[string]int64
	err  error
}

func newFake() *fakeCounter { return &fakeCounter{vals: map[string]int64{}} }
func (f *fakeCounter) incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.vals[key]++
	return f.vals[key], nil
}
func (f *fakeCounter) get(_ context.Context, key string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.vals[key], nil
}

func TestRateLimiter_PerCred(t *testing.T) {
	c := newFake()
	rl := &rateLimiter{c: c, perMin: 3, logger: zerolog.Nop()}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !rl.allowCred(ctx, "cred1", 100) {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	if rl.allowCred(ctx, "cred1", 100) {
		t.Fatal("4th call in the same window should be blocked")
	}
	if !rl.allowCred(ctx, "cred1", 101) {
		t.Fatal("next window should reset")
	}
}

func TestRateLimiter_AuthFail(t *testing.T) {
	c := newFake()
	rl := &rateLimiter{c: c, failMax: 2, failWin: time.Minute, logger: zerolog.Nop()}
	ctx := context.Background()
	if rl.authFailBlocked(ctx, "1.2.3.4") {
		t.Fatal("fresh IP not blocked")
	}
	rl.recordAuthFailure(ctx, "1.2.3.4")
	rl.recordAuthFailure(ctx, "1.2.3.4")
	if !rl.authFailBlocked(ctx, "1.2.3.4") {
		t.Fatal("IP should be blocked after reaching failMax")
	}
	if rl.authFailBlocked(ctx, "9.9.9.9") {
		t.Fatal("other IPs must be independent")
	}
}

func TestRateLimiter_FailOpen(t *testing.T) {
	// nil counter → always allow, never block.
	rl := &rateLimiter{c: nil, perMin: 1, failMax: 1, logger: zerolog.Nop()}
	ctx := context.Background()
	if !rl.allowCred(ctx, "x", 1) || !rl.allowCred(ctx, "x", 1) {
		t.Fatal("nil counter must fail open (allow)")
	}
	rl.recordAuthFailure(ctx, "ip")
	if rl.authFailBlocked(ctx, "ip") {
		t.Fatal("nil counter must not block")
	}
	// Erroring counter → also fail open.
	rl2 := &rateLimiter{c: &fakeCounter{err: context.DeadlineExceeded}, perMin: 1, failMax: 1, logger: zerolog.Nop()}
	if !rl2.allowCred(ctx, "x", 1) {
		t.Fatal("counter error must fail open (allow)")
	}
	if rl2.authFailBlocked(ctx, "ip") {
		t.Fatal("counter error must not block")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/gateway/ -run TestRateLimiter`
Expected: FAIL — `rateLimiter` / `counter` undefined.

- [ ] **Step 3: Write the limiter**

`internal/gateway/ratelimit.go`:

```go
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
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/gateway/ -run TestRateLimiter`
Expected: PASS (3 tests).

- [ ] **Step 5: Wire the limiter into the handler + authenticate**

In `internal/gateway/gateway.go`:

Add `"time"` (if not already added in Task 2) and keep existing imports. Add the config type + limiter field:

```go
// RateLimitConfig configures the gateway's per-credential and per-IP limits.
type RateLimitConfig struct {
	PerMin            int
	AuthfailMax       int
	AuthfailWindowSec int
}

// Handler serves the agent-facing gateway routes.
type Handler struct {
	q      *sqlc.Queries
	redis  *redis.Client
	store  *attachments.Store
	logger zerolog.Logger
	rl     *rateLimiter
}

// NewHandler builds the gateway handler.
func NewHandler(q *sqlc.Queries, rdb *redis.Client, store *attachments.Store, logger zerolog.Logger, rlc RateLimitConfig) *Handler {
	var c counter
	if rdb != nil {
		c = redisCounter{rdb: rdb}
	} else {
		logger.Warn().Msg("gateway rate limiting disabled: no Redis (fail-open)")
	}
	rl := &rateLimiter{
		c:       c,
		perMin:  rlc.PerMin,
		failMax: rlc.AuthfailMax,
		failWin: time.Duration(rlc.AuthfailWindowSec) * time.Second,
		logger:  logger,
	}
	return &Handler{q: q, redis: rdb, store: store, logger: logger, rl: rl}
}
```

Rewrite `authenticate` to enforce limits (record failures, block IPs, cap per credential):

```go
func (h *Handler) authenticate(c *fiber.Ctx) (authedAgent, error) {
	ctx := c.Context()
	ip := clientIP(c)
	if h.rl.authFailBlocked(ctx, ip) {
		c.Set("Retry-After", "60")
		return authedAgent{}, fiber.NewError(fiber.StatusTooManyRequests, "too many failed auth attempts")
	}

	raw := strings.TrimSpace(c.Get("Authorization"))
	raw = strings.TrimPrefix(raw, "Bearer ")
	id, secret, ok := strings.Cut(raw, ".")
	if !ok || id == "" || secret == "" {
		h.rl.recordAuthFailure(ctx, ip)
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "missing or malformed agent credential")
	}

	cred, err := h.q.GetAgentCredentialByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		h.rl.recordAuthFailure(ctx, ip)
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "invalid agent credential")
	} else if err != nil {
		return authedAgent{}, err
	}
	if cred.Status != "active" || !identity.CheckPassword(cred.SecretHash, secret) {
		h.rl.recordAuthFailure(ctx, ip)
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "invalid agent credential")
	}
	if credExpired(cred.ExpiresAt, time.Now()) {
		// Not a brute-force signal — do not record an auth failure.
		return authedAgent{}, fiber.NewError(fiber.StatusUnauthorized, "credential expired")
	}
	if !h.rl.allowCred(ctx, cred.ID, time.Now().Unix()/60) {
		c.Set("Retry-After", "60")
		return authedAgent{}, fiber.NewError(fiber.StatusTooManyRequests, "rate limit exceeded")
	}
	return authedAgent{CredentialID: cred.ID, CompanyID: cred.CompanyID}, nil
}
```

- [ ] **Step 6: Pass config from cmd/gateway**

In `cmd/gateway/main.go`, update the `gateway.NewHandler(...)` call (line ~38):

```go
	gateway.NewHandler(app.Queries, app.Redis, store, app.Logger, gateway.RateLimitConfig{
		PerMin:            app.Config.AgentRLPerMin,
		AuthfailMax:       app.Config.AgentAuthfailMax,
		AuthfailWindowSec: app.Config.AgentAuthfailWindowSec,
	}).Register(f)
```

- [ ] **Step 7: Run tests + build; update any NewHandler test call sites**

Run: `go test ./internal/gateway/ && go build ./...`
Expected: PASS + clean build. If `internal/gateway/stream_test.go` (or any test) calls the old 4-arg `NewHandler`, add a fifth arg `gateway.RateLimitConfig{PerMin: 120, AuthfailMax: 20, AuthfailWindowSec: 300}` (or zero-value — nil redis in tests fails open regardless). Grep first: `grep -rn "NewHandler(" internal/gateway/*_test.go`.

- [ ] **Step 8: Commit**

```bash
git add internal/gateway/ratelimit.go internal/gateway/ratelimit_test.go internal/gateway/gateway.go cmd/gateway/main.go
git commit -m "feat(gateway): per-credential + per-IP-authfail rate limiting (Redis, fail-open)"
```

---

### Task 5: Agent-side legible 401

**Files:**
- Modify: `tradebridge-agent/src/connector/index.ts`

**Interfaces:**
- Consumes: the connector's existing `res.status` check on the `/agent/v1/stream` response and its `logAgent` (or equivalent) logger.

- [ ] **Step 1: Add the loud 401 log**

Work from `/home/deploy/b2b/tradebridge-agent`. In `src/connector/index.ts`, at the point where the stream response is checked (currently throws `stream returned HTTP ${res.status}` on non-OK, ~line 70), add a specific 401 branch BEFORE the generic throw:

```typescript
      if (res.status === 401) {
        await logAgent(
          "error",
          "core.credential_rejected",
          {
            hint: "Core rejected the agent credential (401). It may be expired or revoked — rotate AGENT_TOKEN in .env and restart the agent.",
          },
        );
        throw new Error(`stream unauthorized (HTTP 401)`);
      }
      if (!res.ok) {
        throw new Error(`stream returned HTTP ${res.status}`);
      }
```

Confirm the exact current lines first by reading the file; match its existing `logAgent` import/signature (grep `logAgent` usage in the file — if the connector uses a different logger, use that). Keep the existing backoff/reconnect behavior unchanged (the throw still lands in the retry loop — a 401 during a rotation cutover should still retry).

- [ ] **Step 2: Type-check**

Run: `cd /home/deploy/b2b/tradebridge-agent && export PATH="$HOME/.bun/bin:$PATH" && bunx tsc --noEmit`
Expected: no type errors.

- [ ] **Step 3: Commit** (in the agent repo)

```bash
cd /home/deploy/b2b/tradebridge-agent
git add src/connector/index.ts
git commit -m "feat(connector): loud log on Core 401 so credential rotation is operable"
```

---

## Live Verification (after Task 5, before publish)

Per the `verify` skill. Core api :8080, gateway :8081; agent :3011. Use a company owner JWT (register/login a throwaway company) for the credential endpoints, and the real agent for the gateway.

1. **Migration non-breaking:** after `migrate up` on the live DB, the existing agent still authenticates (`/health` core_connected true), and `GET /v1/agent-credentials` shows its credential with a populated `expires_at` (~90d out).
2. **TTL on issue:** `POST /v1/agent-credentials` → `GET` shows `expires_at ≈ now+90d`.
3. **Rotation:** `POST /v1/agent-credentials/:id/rotate` → returns a new secret; `GET` shows the OLD credential's `expires_at ≈ now+7d` and a NEW active credential; a gateway call with the OLD secret still 200s (grace), and with the NEW secret 200s too.
4. **Expiry enforcement:** set a credential's `expires_at` to the past (SQL) → a gateway call with it → 401 "credential expired".
5. **Rate limit:** fire >120 gateway calls/min with one credential → 429 + `Retry-After`. Fire >20 bad-secret calls from one IP → subsequent calls 429.
6. **Fail-open sanity:** confirmed by unit tests; note in the report that live Redis is up.

Then `publish` — Core Go changed (rebuild api + gateway) + **migration 013** (apply to the live DB before restart) + agent changed (restart supervisor). No web build needed.

## Self-Review

- **Spec coverage:** migration + grandfather → Task 1; TTL issuance + expiry enforcement + config + list visibility → Task 2; rotation w/ grace → Task 3; per-cred + per-IP rate limiting, fail-open, CF-IP → Task 4; agent loud-401 → Task 5. All covered.
- **Placeholder scan:** none — every code step is complete.
- **Type consistency:** `agent.Config{TTLDays,GraceDays}` + `agent.NewHandler(q, cfg)` consistent across Tasks 2–3 and the router wiring; `gateway.NewHandler(...5 args...)` + `RateLimitConfig` consistent between Task 4 and `cmd/gateway`; `credExpiry`/`graceExpiry` (agent) and `credExpired` (gateway) signatures match their call sites; `SetAgentCredentialExpiryParams{ID,CompanyID,ExpiresAt}` matches the Task 1 query column order (`WHERE id=$1 AND company_id=$2 SET expires_at=$3`).

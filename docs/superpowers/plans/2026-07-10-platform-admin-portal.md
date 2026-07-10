# Platform Admin Portal — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A separately-authenticated platform-admin portal (`/admin`) that controls the global LLM config + the RFQ extraction/chat-intake system prompts, isolated from all company logins.

**Architecture:** A new admin auth realm (`platform_admins` table, bootstrap-from-env, `POST /admin/v1/login`, an admin JWT signed by a dedicated `ADMIN_JWT_SECRET`, a `PlatformAdminGuard`) plus a global singleton `platform_settings` store. The `internal/settings` service is repurposed from per-company to global and becomes the RFQ resolver; the two editable prompts thread into `BuildExtractor`/`BuildIntake` with today's constants as fallbacks. A separate `/admin` web portal (own `tb_admin_token`) replaces the removed company admin section.

**Tech Stack:** Go 1.26 / Fiber / sqlc / pgx / golang-jwt v5 / bcrypt (Core); Next.js App Router / React 19 / TypeScript (web).

## Global Constraints

- **Admin realm is cryptographically isolated.** The admin JWT is signed with `ADMIN_JWT_SECRET` (distinct from `JWT_SECRET`) and carries `scope:"platform_admin"`. `PlatformAdminGuard` must reject: no token (401), a company token (401), and any token missing `scope == "platform_admin"` (401). The company `TenantGuard` must reject an admin token.
- **Fail closed.** If `ADMIN_JWT_SECRET` is empty, the admin routes are NOT mounted (logged warning). If `PLATFORM_ADMIN_EMAIL`/`PLATFORM_ADMIN_PASSWORD` are empty, no admin is seeded (login always 401). Bootstrap is idempotent: it creates the admin only if the email row is absent; it NEVER updates an existing admin's password.
- **Global, not per-company.** `settings.Service.ExtractionConfig(ctx, companyID)` reads the single `platform_settings` row (id `'global'`) layered over the env defaults, ignoring `companyID`. The per-company `/v1/settings/extraction` routes and the company `company_settings` reads are removed from the request path (the table is left in place, unused).
- **Prompt fallback.** A blank `extraction_system_prompt`/`intake_system_prompt` means "use the built-in constant" (`rfq.DefaultExtractionPrompt`/`rfq.DefaultIntakePrompt`). Prompts apply only to the LLM path; the heuristic path is unaffected.
- **Secrets.** The provider API key is stored AES-256-GCM-encrypted (`cryptobox`) and never returned in plaintext — reads expose only `has_key` + `key_last4`. `login` returns a generic "invalid credentials" for both unknown-email and bad-password.
- **Provider validation:** `extraction_provider`, when set, must be `"anthropic"` or `"openai_compatible"`.
- **Web:** the admin portal uses a SEPARATE localStorage token key `tb_admin_token` (never `tb_core_token`). Frontend gate = clean `npm run build` + FULLY clean `npm run lint`. Reuse the dark design tokens in `web/app/globals.css`.

**Environment for implementers (every shell):**
`export PATH="$HOME/go-sdk/go/bin:$HOME/go/bin:$HOME/.local/bin:$HOME/node/bin:$PATH"`
Integration tests need the dedicated test DB (never the live one):
`export TEST_DATABASE_URL="postgres://noorluna:REDACTED-DB-PASSWORD@127.0.0.1:5432/tradebridge_core_test?sslmode=disable"` (migrated to v9; each backend task with a new migration runs `migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up`). Web commands run from `web/`.

---

## File Structure

**Backend:**
- `pkg/db/migrations/010_platform_admin.{up,down}.sql` — `platform_admins` + `platform_settings`.
- `pkg/db/query/platform.sql` — admin + settings queries.
- `pkg/db/sqlc/*` — regenerated.
- `pkg/middleware/admin.go` — `AdminClaims`, `PlatformAdminGuard`, `AdminID`/`AdminEmail`, `ScopePlatformAdmin`.
- `internal/admin/token.go` — admin JWT issuer.
- `internal/admin/service.go` — `Bootstrap` + `Authenticate`.
- `internal/admin/handler.go` — `POST /admin/v1/login`, `GET /admin/v1/me`.
- `internal/settings/service.go` + `handler.go` — repurposed to global; handler moves to `/admin/v1/settings` behind the admin guard; gains prompt fields.
- `internal/rfq/requirements.go` / `extractor.go` / `claude.go` / `openai.go` / `intake.go` / `intake_llm.go` — export default prompts; `ProviderConfig` +2 fields; thread prompts.
- `internal/config/config.go` — `ADMIN_JWT_SECRET`, `PLATFORM_ADMIN_EMAIL`, `PLATFORM_ADMIN_PASSWORD`.
- `internal/httpapi/router.go` — mount admin routes, swap rfq resolver, drop company settings route.
- `cmd/api/main.go` — pass new config; call `admin.Bootstrap` at boot.
- `test/integration/api_admin_test.go` — admin auth + settings + resolver tests. `api_settings_test.go` — updated to the admin endpoint.

**Frontend:**
- `web/lib/admin.ts` — admin token store + `adminLogin`/`adminMe`/`getAdminSettings`/`updateAdminSettings` + types.
- `web/components/AdminShell.tsx` — admin-token guard + minimal chrome.
- `web/app/admin/login/page.tsx` — admin login (standalone).
- `web/app/admin/page.tsx` — the settings portal (Model settings + System prompts).
- Removed: `web/app/(app)/admin/models|usage|fraud/page.tsx`; `getExtraction`/`updateExtraction`/`ExtractionSettings` in `web/lib/api.ts`; the Admin nav group in `web/components/AppShell.tsx`; the admin link + unused `isAdmin` in `web/components/ChatShell.tsx`.

---

## Task 1: Migration 010 + platform queries + regenerate

**Files:**
- Create: `pkg/db/migrations/010_platform_admin.up.sql`, `.down.sql`, `pkg/db/query/platform.sql`
- Regenerate: `pkg/db/sqlc/*`
- Test: `test/integration/api_admin_test.go` (new; `TestPlatform_StoreRoundTrip`)

**Interfaces:**
- Produces sqlc: `PlatformAdmin`, `PlatformSetting` models; `GetPlatformAdminByEmail(ctx, email) (PlatformAdmin, error)`, `CreatePlatformAdmin(ctx, CreatePlatformAdminParams{Email, PasswordHash}) (PlatformAdmin, error)`, `GetPlatformSettings(ctx) (PlatformSetting, error)`, `UpsertPlatformSettings(ctx, UpsertPlatformSettingsParams{...}) (PlatformSetting, error)`.

- [ ] **Step 1: Write the failing test**

Create `test/integration/api_admin_test.go`:

```go
package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestPlatform_StoreRoundTrip(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	admin, err := q.CreatePlatformAdmin(ctx, sqlc.CreatePlatformAdminParams{
		Email: uniqueEmail("padmin-"), PasswordHash: "bcrypt$hash",
	})
	require.NoError(t, err)
	require.Len(t, admin.ID, 26)

	got, err := q.GetPlatformAdminByEmail(ctx, admin.Email)
	require.NoError(t, err)
	require.Equal(t, "bcrypt$hash", got.PasswordHash)

	row, err := q.UpsertPlatformSettings(ctx, sqlc.UpsertPlatformSettingsParams{
		ExtractionProvider:     "anthropic",
		ExtractionModel:        "claude-haiku-4-5",
		ExtractionSystemPrompt: "custom extract",
		IntakeSystemPrompt:     "custom intake",
	})
	require.NoError(t, err)
	require.Equal(t, "global", row.ID)

	// Second upsert updates the same singleton row.
	row2, err := q.UpsertPlatformSettings(ctx, sqlc.UpsertPlatformSettingsParams{
		ExtractionProvider: "openai_compatible", ExtractionModel: "gpt-4o-mini",
	})
	require.NoError(t, err)
	require.Equal(t, "global", row2.ID)

	settings, err := q.GetPlatformSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "openai_compatible", settings.ExtractionProvider)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestPlatform_StoreRoundTrip -v`
Expected: FAIL — `CreatePlatformAdmin`/`sqlc.PlatformAdmin` undefined (compile error).

- [ ] **Step 3: Add the migration**

`pkg/db/migrations/010_platform_admin.up.sql`:

```sql
-- Platform superadmin realm: a separate credential store (not tied to a
-- company) and a single global settings row controlling the LLM config +
-- system prompts used across all companies.

CREATE TABLE platform_admins (
    id            TEXT PRIMARY KEY DEFAULT gen_ulid(),
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE platform_settings (
    id                       TEXT PRIMARY KEY DEFAULT 'global' CHECK (id = 'global'),
    extraction_provider      TEXT NOT NULL DEFAULT '',
    extraction_model         TEXT NOT NULL DEFAULT '',
    extraction_base_url      TEXT NOT NULL DEFAULT '',
    extraction_api_key_enc   BYTEA,
    extraction_api_key_last4 TEXT NOT NULL DEFAULT '',
    extraction_system_prompt TEXT NOT NULL DEFAULT '',
    intake_system_prompt     TEXT NOT NULL DEFAULT '',
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

`pkg/db/migrations/010_platform_admin.down.sql`:

```sql
DROP TABLE platform_settings;
DROP TABLE platform_admins;
```

- [ ] **Step 4: Add the queries**

`pkg/db/query/platform.sql`:

```sql
-- name: GetPlatformAdminByEmail :one
SELECT * FROM platform_admins WHERE email = $1;

-- name: CreatePlatformAdmin :one
INSERT INTO platform_admins (email, password_hash)
VALUES ($1, $2)
RETURNING *;

-- name: GetPlatformSettings :one
SELECT * FROM platform_settings WHERE id = 'global';

-- name: UpsertPlatformSettings :one
INSERT INTO platform_settings (
    id, extraction_provider, extraction_model, extraction_base_url,
    extraction_api_key_enc, extraction_api_key_last4,
    extraction_system_prompt, intake_system_prompt, updated_at
) VALUES ('global', $1, $2, $3, $4, $5, $6, $7, NOW())
ON CONFLICT (id) DO UPDATE
SET extraction_provider      = EXCLUDED.extraction_provider,
    extraction_model         = EXCLUDED.extraction_model,
    extraction_base_url      = EXCLUDED.extraction_base_url,
    extraction_api_key_enc   = EXCLUDED.extraction_api_key_enc,
    extraction_api_key_last4 = EXCLUDED.extraction_api_key_last4,
    extraction_system_prompt = EXCLUDED.extraction_system_prompt,
    intake_system_prompt     = EXCLUDED.intake_system_prompt,
    updated_at               = NOW()
RETURNING *;
```

- [ ] **Step 5: Regenerate + migrate the test DB**

Run:
```bash
sqlc generate
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
```
Expected: `pkg/db/sqlc` gains `PlatformAdmin`, `PlatformSetting`, and the four query methods. Migration applies (v9 → v10).

- [ ] **Step 6: Run to verify it passes**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestPlatform_StoreRoundTrip -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 7: Commit**

```bash
git add pkg/db/migrations/010_platform_admin.*.sql pkg/db/query/platform.sql pkg/db/sqlc test/integration/api_admin_test.go
git commit -m "feat(admin): platform_admins + platform_settings tables + queries"
```

---

## Task 2: Admin JWT claims + `PlatformAdminGuard` middleware

**Files:**
- Create: `pkg/middleware/admin.go`
- Create: `internal/admin/token.go`
- Test: `pkg/middleware/admin_test.go`

**Interfaces:**
- Produces:
  - `const middleware.ScopePlatformAdmin = "platform_admin"`
  - `type middleware.AdminClaims struct { AdminID string `json:"admin_id"`; Email string `json:"email"`; Scope string `json:"scope"`; jwt.RegisteredClaims }`
  - `func middleware.PlatformAdminGuard(secret string) fiber.Handler` — sets locals `admin_id`, `admin_email`.
  - `func middleware.AdminID(c) string`, `func middleware.AdminEmail(c) string`.
  - `type admin.TokenService`; `func admin.NewTokenService(secret string, ttl time.Duration) *TokenService`; `func (s *TokenService) Issue(adminID, email string) (string, error)`.

- [ ] **Step 1: Write the failing test**

Create `pkg/middleware/admin_test.go`:

```go
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/middleware"
)

func adminToken(t *testing.T, secret, adminID, email, scope string) string {
	t.Helper()
	claims := &middleware.AdminClaims{
		AdminID: adminID, Email: email, Scope: scope,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return s
}

func adminApp(secret string) *fiber.App {
	f := fiber.New()
	f.Get("/admin/v1/me", middleware.PlatformAdminGuard(secret), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"admin_id": middleware.AdminID(c), "email": middleware.AdminEmail(c)})
	})
	return f
}

func doGet(t *testing.T, app *fiber.App, bearer string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/v1/me", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	return resp.StatusCode
}

func TestPlatformAdminGuard(t *testing.T) {
	const secret = "admin-secret"
	app := adminApp(secret)

	// Valid admin token → 200.
	require.Equal(t, http.StatusOK, doGet(t, app, adminToken(t, secret, "a1", "a@x.com", middleware.ScopePlatformAdmin)))
	// No token → 401.
	require.Equal(t, http.StatusUnauthorized, doGet(t, app, ""))
	// Wrong secret → 401.
	require.Equal(t, http.StatusUnauthorized, doGet(t, app, adminToken(t, "other-secret", "a1", "a@x.com", middleware.ScopePlatformAdmin)))
	// Missing scope (e.g. a token without platform_admin scope) → 401.
	require.Equal(t, http.StatusUnauthorized, doGet(t, app, adminToken(t, secret, "a1", "a@x.com", "")))

	// A company token (different claims/secret) → 401.
	companyTok := adminToken(t, "company-secret", "", "", "") // signed with a different key; won't verify
	require.Equal(t, http.StatusUnauthorized, doGet(t, app, companyTok))
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./pkg/middleware/ -run TestPlatformAdminGuard -v`
Expected: FAIL — `AdminClaims`/`PlatformAdminGuard` undefined.

- [ ] **Step 3: Implement `pkg/middleware/admin.go`**

```go
package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// ScopePlatformAdmin is the required scope claim for the platform-admin realm.
const ScopePlatformAdmin = "platform_admin"

// Locals keys for the admin realm (kept separate from the tenant locals).
const (
	LocalAdminID    = "admin_id"
	LocalAdminEmail = "admin_email"
)

// AdminClaims is the JWT payload for the platform superadmin. It is signed with
// ADMIN_JWT_SECRET (distinct from the company JWT_SECRET) and carries a scope,
// so a company token can never satisfy PlatformAdminGuard and vice-versa.
type AdminClaims struct {
	AdminID string `json:"admin_id"`
	Email   string `json:"email"`
	Scope   string `json:"scope"`
	jwt.RegisteredClaims
}

// PlatformAdminGuard validates a Bearer admin token signed with secret and
// requires scope == platform_admin. It stashes admin_id/admin_email in locals.
func PlatformAdminGuard(secret string) fiber.Handler {
	key := []byte(secret)
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		raw := strings.TrimPrefix(auth, "Bearer ")

		claims := &AdminClaims{}
		token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return key, nil
		})
		if err != nil || !token.Valid || claims.Scope != ScopePlatformAdmin {
			return fiber.NewError(fiber.StatusUnauthorized, "invalid admin token")
		}
		c.Locals(LocalAdminID, claims.AdminID)
		c.Locals(LocalAdminEmail, claims.Email)
		return c.Next()
	}
}

// AdminID returns the platform admin id for the current request.
func AdminID(c *fiber.Ctx) string {
	if v, ok := c.Locals(LocalAdminID).(string); ok {
		return v
	}
	return ""
}

// AdminEmail returns the platform admin email for the current request.
func AdminEmail(c *fiber.Ctx) string {
	if v, ok := c.Locals(LocalAdminEmail).(string); ok {
		return v
	}
	return ""
}
```

- [ ] **Step 4: Implement `internal/admin/token.go`**

```go
// Package admin owns the platform superadmin realm: a credential store separate
// from companies, bootstrap-from-env, login, and an isolated JWT.
package admin

import (
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tradebridge/core/pkg/middleware"
)

// TokenService issues platform-admin JWTs signed with a dedicated secret.
type TokenService struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenService returns an admin token issuer.
func NewTokenService(secret string, ttl time.Duration) *TokenService {
	return &TokenService{secret: []byte(secret), ttl: ttl}
}

// Issue returns a signed HS256 admin token carrying the platform_admin scope.
func (s *TokenService) Issue(adminID, email string) (string, error) {
	claims := &middleware.AdminClaims{
		AdminID: adminID,
		Email:   email,
		Scope:   middleware.ScopePlatformAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./pkg/middleware/ -run TestPlatformAdminGuard -v && go build ./...`
Expected: PASS; build clean.

- [ ] **Step 6: Commit**

```bash
git add pkg/middleware/admin.go pkg/middleware/admin_test.go internal/admin/token.go
git commit -m "feat(admin): AdminClaims + PlatformAdminGuard + admin token issuer"
```

---

## Task 3: Admin service (bootstrap + authenticate) + login/me handlers + wiring

**Files:**
- Create: `internal/admin/service.go`, `internal/admin/handler.go`
- Modify: `internal/config/config.go`, `internal/httpapi/router.go`, `cmd/api/main.go`
- Test: `test/integration/api_admin_test.go` (add endpoint tests)

**Interfaces:**
- Consumes: `sqlc` platform admin queries (Task 1), `admin.TokenService`, `middleware.PlatformAdminGuard`, `identity.HashPassword`/`CheckPassword`.
- Produces:
  - `type admin.Service`; `func admin.NewService(q *sqlc.Queries) *Service`; `func (s *Service) Bootstrap(ctx, email, password string) error`; `func (s *Service) Authenticate(ctx, email, password string) (sqlc.PlatformAdmin, error)`.
  - `type admin.Handler`; `func admin.NewHandler(svc *Service, tokens *TokenService) *Handler`; `func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler)`.
  - Config fields `AdminJWTSecret`, `PlatformAdminEmail`, `PlatformAdminPassword`; `httpapi.Config` fields of the same names.

- [ ] **Step 1: Write the failing endpoint tests**

Append to `test/integration/api_admin_test.go` (add imports `net/http`, and reuse `buildAPI`/`doJSON`/`uniqueEmail`):

```go
// buildAdminAPI builds the router with a known admin seeded, returning the api
// plus the admin's login credentials.
func buildAdminAPI(t *testing.T) (fiberApp, string, string) {
	t.Helper()
	email := uniqueEmail("padmin-")
	const pass = "admin-super-secret"
	api := buildAPIWithAdmin(t, email, pass) // defined in api_a1_test-style helper below
	return api, email, pass
}

func TestAdmin_LoginAndMe(t *testing.T) {
	api, email, pass := buildAdminAPI(t)

	// Bad password → 401.
	st, _ := doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": "wrong"})
	require.Equal(t, http.StatusUnauthorized, st)

	// Unknown email → 401.
	st, _ = doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{"email": "nobody@x.com", "password": pass})
	require.Equal(t, http.StatusUnauthorized, st)

	// Good creds → token.
	st, body := doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": pass})
	require.Equal(t, http.StatusOK, st)
	tok := body["token"].(string)
	require.NotEmpty(t, tok)

	// /me with the admin token → 200 with the email.
	st, me := doJSON(t, api, "GET", "/admin/v1/me", tok, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, email, me["email"])

	// /me with no token → 401.
	st, _ = doJSON(t, api, "GET", "/admin/v1/me", "", nil)
	require.Equal(t, http.StatusUnauthorized, st)
}

func TestAdmin_CompanyTokenRejected(t *testing.T) {
	api, _, _ := buildAdminAPI(t)
	// Register a normal company user and use its token against the admin realm.
	st, s := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "Acme", "company_type": "buyer",
		"email": uniqueEmail("ct-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	companyTok := s["token"].(string)
	st, _ = doJSON(t, api, "GET", "/admin/v1/me", companyTok, nil)
	require.Equal(t, http.StatusUnauthorized, st, "a company token must not reach the admin realm")
}
```

Add a shared builder near `buildAPI` in `test/integration/api_a1_test.go` (or in `api_admin_test.go`), plus a `fiberApp` interface alias if not present:

```go
// fiberApp is the subset of *fiber.App the JSON helpers use.
type fiberApp = *fiber.App

// buildAPIWithAdmin builds the router and seeds a platform admin.
func buildAPIWithAdmin(t *testing.T, email, password string) *fiber.App {
	t.Helper()
	pool := poolOrSkip(t)
	return httpapi.New(sqlc.New(pool), httpapi.Config{
		JWTSecret:             "test-secret",
		AdminJWTSecret:        "admin-test-secret",
		PlatformAdminEmail:    email,
		PlatformAdminPassword: password,
		Logger:                zerolog.Nop(),
		Pool:                  pool,
	})
}
```

> NOTE: `httpapi.New` does not seed the admin today; Step 5 makes the router (or a helper the router calls) run `admin.Bootstrap` when `PlatformAdminEmail`/`PlatformAdminPassword` are set, so `buildAPIWithAdmin` yields a usable login. If you prefer bootstrap in `main` only, have `buildAPIWithAdmin` call `admin.NewService(q).Bootstrap(...)` explicitly before returning — either is fine, but the router path must also work for production.

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestAdmin_ -v`
Expected: FAIL — `AdminJWTSecret` field / admin routes absent (compile error or 404).

- [ ] **Step 3: Implement `internal/admin/service.go`**

```go
package admin

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// ErrInvalidCredentials is returned for both unknown-email and bad-password so
// callers cannot distinguish the two (no user enumeration).
var ErrInvalidCredentials = errors.New("invalid credentials")

// Service owns the platform-admin credential store.
type Service struct {
	q *sqlc.Queries
}

// NewService builds the admin service.
func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

// Bootstrap creates the platform admin from env config if it does not already
// exist. Idempotent: an existing admin (by email) is left untouched — this is a
// one-time seed, never a password reset. A blank email or password is a no-op.
func (s *Service) Bootstrap(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	if _, err := s.q.GetPlatformAdminByEmail(ctx, email); err == nil {
		return nil // already seeded
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	hash, err := identity.HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.q.CreatePlatformAdmin(ctx, sqlc.CreatePlatformAdminParams{Email: email, PasswordHash: hash})
	return err
}

// Authenticate verifies credentials and returns the admin, or ErrInvalidCredentials.
func (s *Service) Authenticate(ctx context.Context, email, password string) (sqlc.PlatformAdmin, error) {
	admin, err := s.q.GetPlatformAdminByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.PlatformAdmin{}, ErrInvalidCredentials
	} else if err != nil {
		return sqlc.PlatformAdmin{}, err
	}
	if !identity.CheckPassword(admin.PasswordHash, password) {
		return sqlc.PlatformAdmin{}, ErrInvalidCredentials
	}
	return admin, nil
}
```

- [ ] **Step 4: Implement `internal/admin/handler.go`**

```go
package admin

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves the admin auth routes.
type Handler struct {
	svc    *Service
	tokens *TokenService
}

// NewHandler builds the admin handler.
func NewHandler(svc *Service, tokens *TokenService) *Handler {
	return &Handler{svc: svc, tokens: tokens}
}

// Register mounts /admin/v1/login (public) and /admin/v1/me (guarded).
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Post("/admin/v1/login", h.login)
	app.Get("/admin/v1/me", append(append([]fiber.Handler{}, guard...), h.me)...)
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(c *fiber.Ctx) error {
	var req loginReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	admin, err := h.svc.Authenticate(c.Context(), req.Email, req.Password)
	if errors.Is(err, ErrInvalidCredentials) {
		return fiber.NewError(fiber.StatusUnauthorized, "invalid credentials")
	} else if err != nil {
		return err
	}
	tok, err := h.tokens.Issue(admin.ID, admin.Email)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"token": tok})
}

func (h *Handler) me(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"admin_id": middleware.AdminID(c), "email": middleware.AdminEmail(c)})
}
```

- [ ] **Step 5: Wire config + router + bootstrap**

In `internal/config/config.go`, add fields and defaults:

```go
	// Platform superadmin realm (separate from company auth).
	AdminJWTSecret        string `mapstructure:"ADMIN_JWT_SECRET"`
	PlatformAdminEmail    string `mapstructure:"PLATFORM_ADMIN_EMAIL"`
	PlatformAdminPassword string `mapstructure:"PLATFORM_ADMIN_PASSWORD"`
```
and in `Load()`:
```go
	v.SetDefault("ADMIN_JWT_SECRET", "")
	v.SetDefault("PLATFORM_ADMIN_EMAIL", "")
	v.SetDefault("PLATFORM_ADMIN_PASSWORD", "")
```

In `internal/httpapi/router.go`, add to `Config`:
```go
	AdminJWTSecret        string
	PlatformAdminEmail    string
	PlatformAdminPassword string
```
and mount the admin realm inside `New(...)` (after the existing handlers), fail-closed when the secret is empty:

```go
	if cfg.AdminJWTSecret != "" {
		adminSvc := admin.NewService(q)
		// Seed the platform admin from env (idempotent). Errors are logged, not fatal.
		if err := adminSvc.Bootstrap(context.Background(), cfg.PlatformAdminEmail, cfg.PlatformAdminPassword); err != nil {
			cfg.Logger.Error().Err(err).Msg("platform admin bootstrap failed")
		}
		adminTokens := admin.NewTokenService(cfg.AdminJWTSecret, 24*time.Hour)
		adminGuard := middleware.PlatformAdminGuard(cfg.AdminJWTSecret)
		admin.NewHandler(adminSvc, adminTokens).Register(f, adminGuard)
	} else {
		cfg.Logger.Warn().Msg("ADMIN_JWT_SECRET unset — platform admin routes disabled")
	}
```
Add imports `"context"` and `"github.com/tradebridge/core/internal/admin"` to router.go.

In `cmd/api/main.go`, pass the three new fields into `httpapi.Config`:
```go
		AdminJWTSecret:        app.Config.AdminJWTSecret,
		PlatformAdminEmail:    app.Config.PlatformAdminEmail,
		PlatformAdminPassword: app.Config.PlatformAdminPassword,
```

- [ ] **Step 6: Run to verify it passes**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestAdmin_ -v && go build ./... && go vet ./...`
Expected: PASS; build + vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/admin/service.go internal/admin/handler.go internal/config/config.go internal/httpapi/router.go cmd/api/main.go test/integration/api_admin_test.go test/integration/api_a1_test.go
git commit -m "feat(admin): admin login + /admin/v1/me + env bootstrap + router wiring"
```

---

## Task 4: Editable prompts threaded into extraction + intake

**Files:**
- Modify: `internal/rfq/requirements.go` (ProviderConfig fields), `internal/rfq/extractor.go` (BuildExtractor + export default), `internal/rfq/claude.go`, `internal/rfq/openai.go` (accept system prompt), `internal/rfq/intake.go` (BuildIntake), `internal/rfq/intake_llm.go` (export default + llmIntake.system + adapters unchanged)
- Test: `internal/rfq/intake_test.go`, `internal/rfq/rfq_test.go` (prompt-selection unit tests)

**Interfaces:**
- Produces: `rfq.DefaultExtractionPrompt`, `rfq.DefaultIntakePrompt` (exported consts, were `extractSystemPrompt`/`intakeSystemPrompt`); `ProviderConfig.ExtractionPrompt`, `ProviderConfig.IntakePrompt`; `newClaudeExtractor(apiKey, model, systemPrompt string, fallback, logger)`; `newOpenAIExtractor(baseURL, model, apiKey, systemPrompt string, fallback, logger)`; `llmIntake.system string`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/rfq/intake_test.go`:

```go
func TestLLMIntake_UsesConfiguredSystemPrompt(t *testing.T) {
	var gotSystem string
	spy := spyChat{onComplete: func(system string) { gotSystem = system }}
	li := llmIntake{chat: spy, fallback: HeuristicIntake{}, logger: zerolog.Nop(), system: "CUSTOM INTAKE PROMPT"}
	_, _ = li.Decide(context.Background(), []ChatMessage{{Role: "user", Content: "cartons"}})
	if gotSystem != "CUSTOM INTAKE PROMPT" {
		t.Fatalf("intake used system %q, want the configured prompt", gotSystem)
	}
}

func TestBuildIntake_FallsBackToDefaultPrompt(t *testing.T) {
	li, ok := BuildIntake(ProviderConfig{Provider: "anthropic", APIKey: "k"}, zerolog.Nop()).(llmIntake)
	if !ok {
		t.Fatal("expected an llmIntake for a keyed anthropic config")
	}
	if li.system != DefaultIntakePrompt {
		t.Fatalf("blank IntakePrompt should fall back to DefaultIntakePrompt")
	}
	li2, _ := BuildIntake(ProviderConfig{Provider: "anthropic", APIKey: "k", IntakePrompt: "X"}, zerolog.Nop()).(llmIntake)
	if li2.system != "X" {
		t.Fatalf("IntakePrompt should be used when set")
	}
}
```

Add the spy helper (returns a create-style JSON so Decide completes):

```go
type spyChat struct{ onComplete func(system string) }

func (s spyChat) complete(_ context.Context, system string, _ []ChatMessage) (string, error) {
	if s.onComplete != nil {
		s.onComplete(system)
	}
	return `{"action":"create","title":"t","rfq_text":"x"}`, nil
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/rfq/ -run 'TestLLMIntake_UsesConfiguredSystemPrompt|TestBuildIntake_FallsBackToDefaultPrompt' -v`
Expected: FAIL — `llmIntake.system` / `DefaultIntakePrompt` undefined.

- [ ] **Step 3: Export the default prompts**

In `internal/rfq/claude.go`, rename the const and update its use:
```go
// DefaultExtractionPrompt is the built-in extraction system prompt, used when
// the platform admin has not set a custom one.
const DefaultExtractionPrompt = `You extract structured procurement requirements from a buyer's plain-text RFQ.
...(unchanged body)...`
```
Replace the two references to `extractSystemPrompt` (in `claude.go` and `openai.go`) — but they now come from a parameter (Step 4), so remove the direct const use there.

In `internal/rfq/intake_llm.go`, rename `intakeSystemPrompt` → `DefaultIntakePrompt` (exported), keeping the body identical, and update `llmIntake.Decide` to use `l.system` (Step 5).

- [ ] **Step 4: Thread the extraction prompt**

In `internal/rfq/requirements.go`, add to `ProviderConfig`:
```go
	ExtractionPrompt string // overrides DefaultExtractionPrompt when non-empty
	IntakePrompt     string // overrides DefaultIntakePrompt when non-empty
```
(Note: `ProviderConfig` currently lives in `extractor.go` — add the fields there where the struct is defined; the file location doesn't matter, keep it with the struct.)

In `internal/rfq/extractor.go`, update `BuildExtractor`:
```go
func BuildExtractor(cfg ProviderConfig, logger zerolog.Logger) Extractor {
	prompt := cfg.ExtractionPrompt
	if prompt == "" {
		prompt = DefaultExtractionPrompt
	}
	switch {
	case cfg.APIKey == "":
		return HeuristicExtractor{}
	case cfg.Provider == "openai_compatible":
		return newOpenAIExtractor(cfg.BaseURL, cfg.Model, cfg.APIKey, prompt, HeuristicExtractor{}, logger)
	default:
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		return newClaudeExtractor(cfg.APIKey, model, prompt, HeuristicExtractor{}, logger)
	}
}
```
In `internal/rfq/claude.go`: add a `system string` field to `claudeExtractor`, set it in `newClaudeExtractor(apiKey, model, system string, fallback Extractor, logger)`, and use `e.system` in the `System:` block instead of `extractSystemPrompt`. Same shape in `internal/rfq/openai.go`: `newOpenAIExtractor(baseURL, model, apiKey, system string, fallback, logger)` storing `system` and using it in the system message.

- [ ] **Step 5: Thread the intake prompt**

In `internal/rfq/intake_llm.go`, add `system string` to `llmIntake` and use it:
```go
type llmIntake struct {
	chat     llmChat
	fallback IntakeAgent
	logger   zerolog.Logger
	system   string
}

func (l llmIntake) Decide(ctx context.Context, msgs []ChatMessage) (IntakeDecision, error) {
	raw, err := l.chat.complete(ctx, l.system, msgs)
	... (unchanged) ...
}
```
In `internal/rfq/intake.go`, update `BuildIntake` to set `system`:
```go
func BuildIntake(cfg ProviderConfig, logger zerolog.Logger) IntakeAgent {
	prompt := cfg.IntakePrompt
	if prompt == "" {
		prompt = DefaultIntakePrompt
	}
	switch {
	case cfg.APIKey == "":
		return HeuristicIntake{}
	case cfg.Provider == "openai_compatible":
		return llmIntake{chat: newOpenAIChat(cfg.BaseURL, cfg.Model, cfg.APIKey, logger), fallback: HeuristicIntake{}, logger: logger, system: prompt}
	default:
		model := cfg.Model
		if model == "" {
			model = "claude-haiku-4-5"
		}
		return llmIntake{chat: newAnthropicChat(cfg.APIKey, model, logger), fallback: HeuristicIntake{}, logger: logger, system: prompt}
	}
}
```

- [ ] **Step 6: Run to verify pass (incl. no regression)**

Run: `go test ./internal/rfq/ -v && go build ./...`
Expected: the two new tests PASS; all existing rfq tests still PASS; build clean.

- [ ] **Step 7: Commit**

```bash
git add internal/rfq/requirements.go internal/rfq/extractor.go internal/rfq/claude.go internal/rfq/openai.go internal/rfq/intake.go internal/rfq/intake_llm.go internal/rfq/intake_test.go internal/rfq/rfq_test.go
git commit -m "feat(rfq): editable extraction/intake system prompts with const fallbacks"
```

---

## Task 5: Global settings service + `/admin/v1/settings` + resolver repoint

**Files:**
- Modify: `internal/settings/service.go` (read `platform_settings`, add prompts, implement resolver globally), `internal/settings/handler.go` (mount under `/admin/v1/settings` w/ admin guard, drop CompanyID), `internal/httpapi/router.go` (use settings as rfq resolver via admin guard; drop company settings route), `internal/settings/service_test.go` (update)
- Modify/replace: `test/integration/api_settings_test.go` → hit the admin endpoint
- Add: settings integration tests in `test/integration/api_admin_test.go`

**Interfaces:**
- Consumes: `sqlc.GetPlatformSettings`/`UpsertPlatformSettings` (Task 1), `rfq.DefaultExtractionPrompt`/`DefaultIntakePrompt` + `ProviderConfig.{ExtractionPrompt,IntakePrompt}` (Task 4), `middleware.PlatformAdminGuard`.
- Produces: `settings.Service.ExtractionConfig(ctx, _ string)` reads the global row; `ExtractionView` + `ExtractionInput` gain `ExtractionSystemPrompt`, `IntakeSystemPrompt`; view also carries `DefaultExtractionPrompt`, `DefaultIntakePrompt`; `settings.Handler.Register(app, guard...)` mounts `GET/PUT /admin/v1/settings`.

- [ ] **Step 1: Write the failing tests**

Add to `test/integration/api_admin_test.go`:

```go
func TestAdmin_SettingsRoundTrip(t *testing.T) {
	api, email, pass := buildAdminAPI(t)
	st, body := doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": pass})
	require.Equal(t, http.StatusOK, st)
	tok := body["token"].(string)

	// Set model config + both prompts + a key.
	st, _ = doJSON(t, api, "PUT", "/admin/v1/settings", tok, map[string]any{
		"provider": "anthropic", "model": "claude-haiku-4-5",
		"api_key": "sk-secret-1234",
		"extraction_system_prompt": "EX", "intake_system_prompt": "IN",
	})
	require.Equal(t, http.StatusOK, st)

	// GET returns masked key + prompts + defaults, never the plaintext key.
	st, got := doJSON(t, api, "GET", "/admin/v1/settings", tok, nil)
	require.Equal(t, http.StatusOK, st)
	ex := got["extraction"].(map[string]any)
	require.Equal(t, true, ex["has_key"])
	require.Equal(t, "1234", ex["key_last4"])
	require.NotContains(t, got, "api_key")
	require.Equal(t, "EX", got["extraction_system_prompt"])
	require.Equal(t, "IN", got["intake_system_prompt"])
	require.NotEmpty(t, got["default_extraction_prompt"])

	// Company token cannot reach settings.
	st, _ = doJSON(t, api, "GET", "/admin/v1/settings", "", nil)
	require.Equal(t, http.StatusUnauthorized, st)

	// Legacy per-company route is gone.
	st, _ = doJSON(t, api, "GET", "/v1/settings/extraction", tok, nil)
	require.Equal(t, http.StatusNotFound, st)
}
```

Update `test/integration/api_settings_test.go`: its previous per-company assertions move to the admin endpoint (or delete the file if fully superseded by `TestAdmin_SettingsRoundTrip` — keep only tests that still apply).

- [ ] **Step 2: Run to verify it fails**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./test/integration/ -run TestAdmin_SettingsRoundTrip -v`
Expected: FAIL — `/admin/v1/settings` 404.

- [ ] **Step 3: Repoint `internal/settings/service.go` at the global store**

Change the resolver + view/input. Key edits:
- `GetCompanySettings(ctx, companyID)` → `GetPlatformSettings(ctx)`; `UpsertCompanySettings(...)` → `UpsertPlatformSettings(...)`.
- `ExtractionConfig(ctx, _ string)` ignores the company id, returns the global row layered over `s.env`, and additionally sets `cfg.ExtractionPrompt = row.ExtractionSystemPrompt` and `cfg.IntakePrompt = row.IntakeSystemPrompt`.
- Extend `ExtractionView`:
```go
type ExtractionView struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	HasKey   bool   `json:"has_key"`
	KeyLast4 string `json:"key_last4"`
}
type SettingsView struct {
	Extraction              ExtractionView `json:"extraction"`
	ExtractionSystemPrompt  string         `json:"extraction_system_prompt"`
	IntakeSystemPrompt      string         `json:"intake_system_prompt"`
	DefaultExtractionPrompt string         `json:"default_extraction_prompt"`
	DefaultIntakePrompt     string         `json:"default_intake_prompt"`
}
```
- `Get(ctx)` returns `SettingsView` (defaults from `rfq.DefaultExtractionPrompt`/`rfq.DefaultIntakePrompt`).
- `ExtractionInput` gains `ExtractionSystemPrompt string json:"extraction_system_prompt"` and `IntakeSystemPrompt string json:"intake_system_prompt"`; `Update` writes them through `UpsertPlatformSettings`. The key encrypt/clear/preserve logic is unchanged (operate on the global row's existing key). Import `rfq` for the default consts.

- [ ] **Step 4: Move the handler to the admin realm**

In `internal/settings/handler.go`: `Register(app, guard ...fiber.Handler)` mounts:
```go
	g := app.Group("/admin/v1/settings", guard...)
	g.Get("/", h.get)
	g.Put("/", h.update)
```
`get` returns `svc.Get(c.Context())` (no CompanyID); `update` parses `ExtractionInput`, validates provider, calls `svc.Update(c.Context(), in)`. Remove `middleware.CompanyID`/`RequireRole` usage.

- [ ] **Step 5: Rewire the router**

In `internal/httpapi/router.go`:
- Build `settingsSvc := settings.NewService(q, box, envExtraction)` as today, but mount it under the admin guard instead of the company guard: inside the `cfg.AdminJWTSecret != ""` block, after building `adminGuard`, add `settings.NewHandler(settingsSvc).Register(f, adminGuard)`.
- Remove the old `settings.NewHandler(settingsSvc).Register(f, guard...)` company registration.
- `rfq.NewService(q, pub, settingsSvc, cfg.Logger)` stays — `settingsSvc` is still the `rfq.ExtractorResolver`, now global.

> Cross-check: `settingsSvc` must be constructed OUTSIDE the `if cfg.AdminJWTSecret != ""` block (rfq needs it as resolver even when the admin realm is disabled), but its HTTP handler is registered INSIDE the block. Keep the construction above the block.

- [ ] **Step 6: Run to verify pass + full suite**

Run: `TEST_DATABASE_URL=$TEST_DATABASE_URL go test ./... && go build ./... && go vet ./...`
Expected: all pass (update/remove any now-obsolete `settings/service_test.go` expectations that assumed per-company reads).

- [ ] **Step 7: Commit**

```bash
git add internal/settings/ internal/httpapi/router.go test/integration/
git commit -m "feat(admin): global settings + prompts via /admin/v1/settings; rfq resolver reads global"
```

---

## Task 6: Web admin client + `AdminShell` + login page

**Files:**
- Create: `web/lib/admin.ts`, `web/components/AdminShell.tsx`, `web/app/admin/login/page.tsx`

**Interfaces:**
- Produces: `web/lib/admin.ts` exports `adminLogin(email,password)`, `adminMe(token)`, `getAdminSettings(token)`, `updateAdminSettings(token, body)`, token helpers (`adminTokenSnapshot`/`setAdminToken`/`clearAdminToken`/`subscribeAdminToken` over key `tb_admin_token`), and types `AdminSettings`, `AdminSettingsInput`. `AdminShell` (default export) guards on the admin token.

- [ ] **Step 1: Implement `web/lib/admin.ts`**

Mirror `web/lib/api.ts`'s `req()` + token-store pattern, but with key `tb_admin_token` and paths under `/admin/v1/*`:

```ts
"use client";

import { CORE_URL } from "./api";
import { useSyncExternalStore } from "react";

const ADMIN_TOKEN_KEY = "tb_admin_token";
const listeners = new Set<() => void>();
function emit() { listeners.forEach((l) => l()); }
export function subscribeAdminToken(cb: () => void) { listeners.add(cb); return () => listeners.delete(cb); }
export function adminTokenSnapshot(): string | null {
  return typeof window !== "undefined" ? localStorage.getItem(ADMIN_TOKEN_KEY) : null;
}
export function setAdminToken(t: string) { localStorage.setItem(ADMIN_TOKEN_KEY, t); emit(); }
export function clearAdminToken() { localStorage.removeItem(ADMIN_TOKEN_KEY); emit(); }
export function useAdminToken(): string | null {
  return useSyncExternalStore(subscribeAdminToken, adminTokenSnapshot, () => null);
}

async function req<T>(method: string, path: string, opts: { token?: string | null; body?: unknown } = {}): Promise<T> {
  const res = await fetch(`${CORE_URL}${path}`, {
    method,
    headers: {
      ...(opts.body ? { "content-type": "application/json" } : {}),
      ...(opts.token ? { authorization: `Bearer ${opts.token}` } : {}),
    },
    body: opts.body ? JSON.stringify(opts.body) : undefined,
    cache: "no-store",
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text || `HTTP ${res.status}`);
  return (text ? JSON.parse(text) : null) as T;
}

export type AdminSettings = {
  extraction: { provider: string; model: string; base_url: string; has_key: boolean; key_last4: string };
  extraction_system_prompt: string;
  intake_system_prompt: string;
  default_extraction_prompt: string;
  default_intake_prompt: string;
};
export type AdminSettingsInput = {
  provider: string; model: string; base_url: string;
  api_key?: string; clear_api_key?: boolean;
  extraction_system_prompt: string; intake_system_prompt: string;
};

export const adminLogin = (email: string, password: string) =>
  req<{ token: string }>("POST", "/admin/v1/login", { body: { email, password } });
export const adminMe = (token: string) =>
  req<{ admin_id: string; email: string }>("GET", "/admin/v1/me", { token });
export const getAdminSettings = (token: string) =>
  req<AdminSettings>("GET", "/admin/v1/settings/", { token });
export const updateAdminSettings = (token: string, body: AdminSettingsInput) =>
  req<AdminSettings>("PUT", "/admin/v1/settings/", { token, body });
```

> NOTE the trailing slash on `/admin/v1/settings/` — Fiber's `app.Group("/admin/v1/settings")` + `g.Get("/")` registers the path WITH a trailing slash. Match it here (or, in Task 5, register `g.Get("")`/`g.Put("")` to serve the no-slash path and drop the slash here — pick one and keep the client and route consistent).

- [ ] **Step 2: Implement `web/components/AdminShell.tsx`**

A minimal chrome + admin-token guard (mirrors `ChatShell`'s hydration guard):

```tsx
"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { adminMe, clearAdminToken, useAdminToken } from "@/lib/admin";

export default function AdminShell({ children }: { children: React.ReactNode }) {
  const token = useAdminToken();
  const router = useRouter();
  const [hydrated, setHydrated] = useState(false);
  const [ok, setOk] = useState(false);

  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => setHydrated(true), []);

  useEffect(() => {
    if (!hydrated) return;
    if (!token) { router.replace("/admin/login"); return; }
    adminMe(token).then(() => setOk(true)).catch(() => { clearAdminToken(); router.replace("/admin/login"); });
  }, [hydrated, token, router]);

  if (!hydrated || !ok) {
    return <div style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center" }} className="muted">Loading…</div>;
  }

  return (
    <div style={{ minHeight: "100vh" }}>
      <header style={{ height: 56, borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "space-between", padding: "0 1.5rem" }}>
        <strong>TradeBridge · Platform Admin</strong>
        <button className="btn btn-sm btn-ghost" onClick={() => { clearAdminToken(); router.replace("/admin/login"); }}>Sign out</button>
      </header>
      <main style={{ padding: "1.75rem", maxWidth: 760, width: "100%" }}>{children}</main>
    </div>
  );
}
```

- [ ] **Step 3: Implement `web/app/admin/login/page.tsx`**

Standalone (NOT guarded), reusing the `AuthWrap` visual from `web/app/login/page.tsx`:

```tsx
"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { adminLogin, setAdminToken } from "@/lib/admin";

export default function AdminLogin() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const { token } = await adminLogin(email, password);
      setAdminToken(token);
      router.replace("/admin");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "login failed");
      setBusy(false);
    }
  };

  return (
    <main style={{ minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center", padding: "2rem" }}>
      <div className="card" style={{ width: "100%", maxWidth: 400 }}>
        <strong style={{ fontSize: "1.15rem" }}>TradeBridge · Platform Admin</strong>
        <h1 style={{ fontSize: "1.3rem", margin: "1rem 0 1.25rem" }}>Admin sign in</h1>
        <form className="col" onSubmit={submit}>
          <div className="field"><label>Email</label><input className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required /></div>
          <div className="field"><label>Password</label><input className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required /></div>
          {err && <p className="err">{err}</p>}
          <button className="btn btn-primary" disabled={busy}>{busy ? "Signing in…" : "Sign in"}</button>
        </form>
      </div>
    </main>
  );
}
```

- [ ] **Step 4: Gate**

Run: `cd web && npm run lint && npm run build`
Expected: only the (still-present until Task 8) pre-existing lint state; the new files contribute ZERO lint errors; build clean. (`/admin/login` appears in the route list.)

- [ ] **Step 5: Commit**

```bash
git add web/lib/admin.ts web/components/AdminShell.tsx web/app/admin/login/page.tsx
git commit -m "feat(web): admin API client, AdminShell guard, admin login page"
```

---

## Task 7: Web admin settings portal

**Files:**
- Create: `web/app/admin/page.tsx`

**Interfaces:**
- Consumes: `AdminShell` (Task 6), `getAdminSettings`/`updateAdminSettings`/`useAdminToken`/`AdminSettings` (Task 6).

- [ ] **Step 1: Implement `web/app/admin/page.tsx`**

Wrap the content in `AdminShell`; two panels (Model settings + System prompts). Load with `getAdminSettings`, save with `updateAdminSettings`:

```tsx
"use client";

import { useEffect, useState } from "react";
import AdminShell from "@/components/AdminShell";
import { getAdminSettings, updateAdminSettings, useAdminToken, type AdminSettings } from "@/lib/admin";

export default function AdminSettingsPage() {
  return (
    <AdminShell>
      <SettingsForm />
    </AdminShell>
  );
}

function SettingsForm() {
  const token = useAdminToken();
  const [s, setS] = useState<AdminSettings | null>(null);
  const [provider, setProvider] = useState("anthropic");
  const [model, setModel] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [clearKey, setClearKey] = useState(false);
  const [exPrompt, setExPrompt] = useState("");
  const [inPrompt, setInPrompt] = useState("");
  const [msg, setMsg] = useState<string | null>(null);
  const [msgKind, setMsgKind] = useState<"ok" | "err">("ok");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!token) return;
    getAdminSettings(token).then((v) => {
      setS(v);
      setProvider(v.extraction.provider || "anthropic");
      setModel(v.extraction.model);
      setBaseUrl(v.extraction.base_url);
      setExPrompt(v.extraction_system_prompt);
      setInPrompt(v.intake_system_prompt);
    }).catch((e) => { setMsg(e.message); setMsgKind("err"); });
  }, [token]);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true); setMsg(null);
    try {
      const v = await updateAdminSettings(token, {
        provider, model, base_url: baseUrl,
        api_key: apiKey || undefined, clear_api_key: clearKey || undefined,
        extraction_system_prompt: exPrompt, intake_system_prompt: inPrompt,
      });
      setS(v); setApiKey(""); setClearKey(false);
      setMsg("Saved."); setMsgKind("ok");
    } catch (err) {
      setMsg(err instanceof Error ? err.message : "save failed"); setMsgKind("err");
    } finally { setBusy(false); }
  };

  if (!s) return <p className="muted">Loading…</p>;
  const keyPlaceholder = s.extraction.has_key ? `key set (…${s.extraction.key_last4}) — blank keeps it` : "paste API key";

  return (
    <form className="col" onSubmit={save} style={{ gap: "1.5rem" }}>
      <div className="card">
        <h2>Model settings (global)</h2>
        <p className="muted small">Provider + model that power RFQ extraction and chat intake for every company.</p>
        <div className="field"><label>Provider</label>
          <select className="select" value={provider} onChange={(e) => setProvider(e.target.value)}>
            <option value="anthropic">Anthropic (Claude)</option>
            <option value="openai_compatible">OpenAI-compatible</option>
          </select>
        </div>
        <div className="field"><label>Model</label>
          <input className="input" value={model} onChange={(e) => setModel(e.target.value)} placeholder={provider === "anthropic" ? "claude-haiku-4-5" : "gpt-4o-mini"} />
        </div>
        {provider === "openai_compatible" && (
          <div className="field"><label>Base URL</label>
            <input className="input" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.openai.com/v1" />
          </div>
        )}
        <div className="field"><label>API key</label>
          <input className="input" type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={keyPlaceholder} />
        </div>
        {s.extraction.has_key && (
          <label className="row small muted" style={{ gap: "0.5rem" }}>
            <input type="checkbox" checked={clearKey} onChange={(e) => setClearKey(e.target.checked)} /> Remove the stored key
          </label>
        )}
      </div>

      <div className="card">
        <h2>System prompts</h2>
        <div className="field">
          <label>Extraction prompt <button type="button" className="btn btn-sm btn-ghost" onClick={() => setExPrompt("")}>Reset to default</button></label>
          <textarea className="input" rows={6} value={exPrompt} onChange={(e) => setExPrompt(e.target.value)} placeholder={s.default_extraction_prompt} />
          <p className="muted small">Blank = use the built-in default.</p>
        </div>
        <div className="field">
          <label>Chat intake prompt <button type="button" className="btn btn-sm btn-ghost" onClick={() => setInPrompt("")}>Reset to default</button></label>
          <textarea className="input" rows={6} value={inPrompt} onChange={(e) => setInPrompt(e.target.value)} placeholder={s.default_intake_prompt} />
          <p className="muted small">Blank = use the built-in default.</p>
        </div>
      </div>

      {msg && <p className={msgKind}>{msg}</p>}
      <div><button className="btn btn-primary" disabled={busy}>{busy ? "Saving…" : "Save"}</button></div>
    </form>
  );
}
```

- [ ] **Step 2: Gate**

Run: `cd web && npm run lint && npm run build`
Expected: new file contributes ZERO lint errors; build clean; `/admin` in the route list.

- [ ] **Step 3: Commit**

```bash
git add web/app/admin/page.tsx
git commit -m "feat(web): platform admin settings portal (model + system prompts)"
```

---

## Task 8: Remove the company admin section + nav cleanup

**Files:**
- Delete: `web/app/(app)/admin/models/page.tsx`, `web/app/(app)/admin/usage/page.tsx`, `web/app/(app)/admin/fraud/page.tsx` (and the now-empty `admin/` dirs)
- Modify: `web/lib/api.ts` (remove `getExtraction`, `updateExtraction`, `ExtractionSettings`), `web/components/AppShell.tsx` (remove Admin nav group + unused `isAdmin`), `web/components/ChatShell.tsx` (remove Admin link + unused `isAdmin`)

**Interfaces:**
- Consumes: nothing new. This task removes superseded code; after it, `npm run lint` is FULLY clean.

- [ ] **Step 1: Delete the company admin pages**

```bash
git rm "web/app/(app)/admin/models/page.tsx" "web/app/(app)/admin/usage/page.tsx" "web/app/(app)/admin/fraud/page.tsx"
```

- [ ] **Step 2: Remove the superseded API client bits**

In `web/lib/api.ts`, delete the `ExtractionSettings` type and the `getExtraction`/`updateExtraction` exports (they targeted the removed `/v1/settings/extraction`). Confirm nothing else imports them: `grep -rn "getExtraction\|updateExtraction\|ExtractionSettings" web/` returns only the deletions.

- [ ] **Step 3: Remove the Admin nav from `AppShell.tsx`**

Delete the entire `Admin` group object from the `NAV` array. Then remove the now-unused `isAdmin` const (line ~13) if nothing else references it (`grep -n isAdmin web/components/AppShell.tsx`). Leave `isSeller`/`isBuyer` intact.

- [ ] **Step 4: Remove the Admin link from `ChatShell.tsx`**

Delete the `{isAdmin(me) && <Link href="/admin/models" ...>Admin settings</Link>}` line from the bottom user menu, and remove the now-unused `isAdmin` const. (The platform admin portal is reached at `/admin/login`, not from a company user's menu.)

- [ ] **Step 5: Gate (fully clean)**

Run: `cd web && npm run lint && npm run build && npx vitest run`
Expected: `npm run lint` FULLY clean (zero errors); build clean; existing vitest still 4/4. No route references a deleted page.

- [ ] **Step 6: Commit**

```bash
git add -A web/
git commit -m "refactor(web): remove superseded company admin section + dead settings client"
```

---

## Self-Review

**Spec coverage:**
- §3 separate auth realm (table, bootstrap-from-env, login, admin JWT w/ separate secret, guard, /me) → Tasks 1–3. ✓
- §4 global `platform_settings` + resolver repoint + prompt threading + admin settings endpoints + superseded per-company routes removed → Tasks 1, 4, 5. ✓
- §5 `/admin` portal (login + AdminShell + settings page, separate `tb_admin_token`) + namespace cleanup (remove company `/admin/*`, nav) → Tasks 6–8. ✓
- §6 security: realm isolation (guard rejects no-token/company-token/missing-scope; TenantGuard rejects admin token — covered by Task 2 unit + Task 3/5 integration), fail-closed (no secret → routes off; no env → no seed), key never plaintext (Task 5 test), generic login error (Task 3). ✓
- §7 testing: admin bootstrap idempotent (Task 3 — add an explicit second-Bootstrap assertion), login good/bad, guard matrix, settings round-trip masked, resolver reads global, prompt threading unit tests, migration up/down. ✓ Frontend gate = build + fully-clean lint. ✓

**Gap fix (add to Task 3):** the spec calls for an explicit "bootstrap is idempotent on a second boot" test. Add to `TestAdmin_LoginAndMe` (or a new `TestAdmin_BootstrapIdempotent`): call `admin.NewService(q).Bootstrap(ctx, email, pass)` twice against the same pool and assert only one row exists (`GetPlatformAdminByEmail` still succeeds, no error, and a second `CreatePlatformAdmin` was avoided). Since `buildAPIWithAdmin` already bootstraps once via the router, a second direct `Bootstrap` call that returns nil and doesn't error covers it.

**Placeholder scan:** no TBD/TODO; every code step is complete. The two ADR-style NOTES (trailing-slash on the settings route; bootstrap-in-router-vs-main) are explicit decisions with a concrete resolution, not placeholders. ✓

**Type consistency:** `AdminClaims`/`ScopePlatformAdmin`/`PlatformAdminGuard`/`AdminID`/`AdminEmail` (Task 2) are used identically in Tasks 3 & 5; `admin.Service`/`TokenService`/`Handler` signatures match between producer (Tasks 2–3) and the router (Task 3); `settings.SettingsView`/`ExtractionInput` fields (Task 5) match the web `AdminSettings`/`AdminSettingsInput` shapes (Task 6) — `extraction.{provider,model,base_url,has_key,key_last4}`, `extraction_system_prompt`, `intake_system_prompt`, `default_extraction_prompt`, `default_intake_prompt`. `rfq.DefaultExtractionPrompt`/`DefaultIntakePrompt` + `ProviderConfig.{ExtractionPrompt,IntakePrompt}` (Task 4) are consumed by Task 5. ✓

**Route-slash consistency:** Task 5 registers `/admin/v1/settings` via `Group(...).Get("/")` (trailing slash) and Task 6's client calls `/admin/v1/settings/` — kept consistent, with the note offering the no-slash alternative if the implementer prefers.

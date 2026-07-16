# Sprint 3 — Plan 1: Verification Spine — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A seller proves domain control via DNS-TXT and their company flips to `verified`; the verified state is the source of truth for the badge and can gate the RFQ match pool.

**Architecture:** New `internal/trust` package holds a `Service` with an injectable `TXTResolver` (real `net.Resolver` in prod, fake in tests). Verification state lives on `companies`; a `domain_verifications` table holds the active challenge. An admin override/revoke path reuses the existing platform-admin realm. A config flag gates the match pool.

**Tech Stack:** Go 1.23 / Fiber v2 / pgx v5 / sqlc / golang-migrate / zerolog. DB tests use `TEST_DATABASE_URL` (skip when unset).

## Global Constraints

- Source of truth for the badge is `companies.verification_status`; keep `supplier_profiles.verified` in sync so the existing `SearchSuppliers ORDER BY sp.verified DESC` and `MatchSuppliers` keep working.
- **DNS-TXT only.** No HTTP fetch of a user-supplied domain anywhere (SSRF). The resolver is an interface; no real DNS in tests.
- Verification states are exactly `unverified | pending | verified | rejected`. Domain-challenge states are exactly `pending | verified | failed`.
- A domain can be `verified` by only one company at a time (enforced by a partial unique index — handle the unique-violation as a clean 409).
- Match-pool gate defaults **OFF** (`VERIFY_REQUIRED_FOR_MATCH=false`).
- Follow existing patterns: handlers expose `NewHandler(...).Register(app, guard...)`; nullable text columns are `*string` in sqlc params; tenant identity via `middleware.CompanyID(c)` / `Role(c)`.
- Every DB-backed test uses the pattern in `internal/settings/service_test.go` (`os.Getenv("TEST_DATABASE_URL")`, skip if empty, `pgxpool.New`, `sqlc.New`).

---

### Task 1: Migration 017 — verification schema

**Files:**
- Create: `pkg/db/migrations/017_trust_verification.up.sql`
- Create: `pkg/db/migrations/017_trust_verification.down.sql`

**Interfaces:**
- Produces: `companies.verification_status/verified_domain/verified_at`; table `domain_verifications`.

- [ ] **Step 1: Write the up migration**

`pkg/db/migrations/017_trust_verification.up.sql`:
```sql
-- Sprint 3 — Trust & Safety I: DNS-TXT domain verification.
-- Verification state lives on the company (both-role: one identity gates the
-- selling side's badge and match eligibility). domain_verifications holds the
-- active ownership challenge.

ALTER TABLE companies
    ADD COLUMN verification_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (verification_status IN ('unverified','pending','verified','rejected')),
    ADD COLUMN verified_domain TEXT,
    ADD COLUMN verified_at     TIMESTAMPTZ;

-- A domain can be *verified* by at most one company at a time. Pending claims
-- for the same domain by different companies are allowed (only one will win the
-- race to verified).
CREATE UNIQUE INDEX idx_companies_verified_domain
    ON companies (verified_domain) WHERE verification_status = 'verified';

CREATE TABLE domain_verifications (
    id              TEXT PRIMARY KEY DEFAULT gen_ulid(),
    company_id      TEXT NOT NULL REFERENCES companies(id),
    domain          TEXT NOT NULL,
    token           TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','verified','failed')),
    attempts        INT  NOT NULL DEFAULT 0,
    last_checked_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    verified_at     TIMESTAMPTZ
);
CREATE UNIQUE INDEX idx_domain_verifications_company_domain
    ON domain_verifications (company_id, domain);
CREATE INDEX idx_domain_verifications_company
    ON domain_verifications (company_id, created_at DESC);
```

- [ ] **Step 2: Write the down migration**

`pkg/db/migrations/017_trust_verification.down.sql`:
```sql
DROP TABLE IF EXISTS domain_verifications;
DROP INDEX IF EXISTS idx_companies_verified_domain;
ALTER TABLE companies
    DROP COLUMN IF EXISTS verified_at,
    DROP COLUMN IF EXISTS verified_domain,
    DROP COLUMN IF EXISTS verification_status;
```

- [ ] **Step 3: Apply up, then down, then up on the test DB**

Run (TEST_DATABASE_URL is exported in the execution environment):
```bash
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" down 1
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" up
migrate -path pkg/db/migrations -database "$TEST_DATABASE_URL" version
```
Expected: ends at version `17`, no `dirty` flag, no error (proves up+down are reversible).

- [ ] **Step 4: Commit**

```bash
git add pkg/db/migrations/017_trust_verification.up.sql pkg/db/migrations/017_trust_verification.down.sql
git commit -m "feat(trust): migration 017 — company verification state + domain_verifications"
```

---

### Task 2: sqlc queries for verification

**Files:**
- Create: `pkg/db/query/verification.sql`
- Modify (generated): `pkg/db/sqlc/*` via `sqlc generate`
- Test: `internal/trust/queries_test.go`

**Interfaces:**
- Produces (sqlc methods): `UpsertDomainVerification`, `GetDomainVerification`, `TouchDomainVerificationAttempt`, `MarkDomainVerificationVerified`, `SetCompanyVerified`, `SetCompanyVerificationStatus`, `SyncSupplierVerified`, `GetCompanyVerification`, `ListPendingVerificationByCompany`, `ListCompaniesByVerificationStatus`.

- [ ] **Step 1: Write the queries**

`pkg/db/query/verification.sql`:
```sql
-- name: UpsertDomainVerification :one
-- Claim (or re-claim) a domain: regenerates the token and resets the challenge.
INSERT INTO domain_verifications (company_id, domain, token, status, attempts)
VALUES (@company_id, @domain, @token, 'pending', 0)
ON CONFLICT (company_id, domain) DO UPDATE
SET token = EXCLUDED.token,
    status = 'pending',
    attempts = 0,
    last_checked_at = NULL,
    verified_at = NULL
RETURNING *;

-- name: GetDomainVerification :one
SELECT * FROM domain_verifications WHERE company_id = @company_id AND domain = @domain;

-- name: TouchDomainVerificationAttempt :exec
UPDATE domain_verifications
SET attempts = attempts + 1, last_checked_at = NOW()
WHERE id = @id;

-- name: MarkDomainVerificationVerified :exec
UPDATE domain_verifications SET status = 'verified', verified_at = NOW() WHERE id = @id;

-- name: SetCompanyVerified :exec
UPDATE companies
SET verification_status = 'verified', verified_domain = @verified_domain, verified_at = NOW()
WHERE id = @id;

-- name: SetCompanyVerificationStatus :exec
-- Admin override. On non-verified status, clears the verified domain so the
-- partial unique index frees it for re-verification elsewhere.
UPDATE companies
SET verification_status = @status,
    verified_domain = CASE WHEN @status::text = 'verified' THEN verified_domain ELSE NULL END,
    verified_at     = CASE WHEN @status::text = 'verified' THEN COALESCE(verified_at, NOW()) ELSE NULL END
WHERE id = @company_id;

-- name: SyncSupplierVerified :exec
UPDATE supplier_profiles SET verified = @verified, updated_at = NOW() WHERE company_id = @company_id;

-- name: GetCompanyVerification :one
SELECT id, verification_status, verified_domain, verified_at FROM companies WHERE id = @id;

-- name: ListPendingVerificationByCompany :many
SELECT * FROM domain_verifications
WHERE company_id = @company_id AND status = 'pending'
ORDER BY created_at DESC;

-- name: ListCompaniesByVerificationStatus :many
SELECT id, name, type, verification_status, verified_domain, verified_at
FROM companies
WHERE (sqlc.narg(status)::text IS NULL OR verification_status = sqlc.narg(status)::text)
ORDER BY verified_at DESC NULLS LAST, name
LIMIT sqlc.arg(lim) OFFSET sqlc.arg(off);
```

- [ ] **Step 2: Generate sqlc**

Run: `sqlc generate`
Expected: no error; new methods appear in `pkg/db/sqlc/`.

- [ ] **Step 3: Write the round-trip test**

`internal/trust/queries_test.go`:
```go
package trust

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func testQueriesOrSkip(t *testing.T) (*sqlc.Queries, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool), pool
}

// makeCompany inserts a bare company and returns its id.
func makeCompany(t *testing.T, q *sqlc.Queries, name string) string {
	t.Helper()
	co, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: name, Type: "seller"})
	require.NoError(t, err)
	return co.ID
}

func TestDomainVerificationRoundTrip(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	ctx := context.Background()
	cid := makeCompany(t, q, "verif-rt-co")

	dv, err := q.UpsertDomainVerification(ctx, sqlc.UpsertDomainVerificationParams{
		CompanyID: cid, Domain: "acme-rt.example", Token: "tok1",
	})
	require.NoError(t, err)
	require.Equal(t, "pending", dv.Status)

	// Re-claim regenerates token and resets.
	dv2, err := q.UpsertDomainVerification(ctx, sqlc.UpsertDomainVerificationParams{
		CompanyID: cid, Domain: "acme-rt.example", Token: "tok2",
	})
	require.NoError(t, err)
	require.Equal(t, "tok2", dv2.Token)
	require.Equal(t, dv.ID, dv2.ID, "same (company,domain) row")

	require.NoError(t, q.MarkDomainVerificationVerified(ctx, dv2.ID))
	require.NoError(t, q.SetCompanyVerified(ctx, sqlc.SetCompanyVerifiedParams{ID: cid, VerifiedDomain: strptr("acme-rt.example")}))

	got, err := q.GetCompanyVerification(ctx, cid)
	require.NoError(t, err)
	require.Equal(t, "verified", got.VerificationStatus)
	require.Equal(t, "acme-rt.example", *got.VerifiedDomain)
}

func strptr(s string) *string { return &s }
```

Note: confirm the exact `CreateCompany` param/return names from `pkg/db/sqlc` (use `git grep "func (q \*Queries) CreateCompany"`); adjust `makeCompany` to match. If `CreateCompany` isn't present, insert via `pool.Exec` with `INSERT INTO companies (name, type) VALUES ($1,$2) RETURNING id`.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/trust/ -run TestDomainVerificationRoundTrip -v`
Expected: PASS (or SKIP if `TEST_DATABASE_URL` unset — the execution env sets it, so expect PASS).

- [ ] **Step 5: Commit**

```bash
git add pkg/db/query/verification.sql pkg/db/sqlc internal/trust/queries_test.go
git commit -m "feat(trust): sqlc queries for domain verification + round-trip test"
```

---

### Task 3: Verification service (DNS-TXT, injectable resolver)

**Files:**
- Create: `internal/trust/verification.go`
- Create: `internal/trust/verification_test.go`

**Interfaces:**
- Consumes: sqlc methods from Task 2; `*pgxpool.Pool` (for the verify transaction).
- Produces:
  - `type TXTResolver interface { LookupTXT(ctx context.Context, name string) ([]string, error) }`
  - `func NewService(q *sqlc.Queries, pool *pgxpool.Pool, resolver TXTResolver, logger zerolog.Logger) *Service`
  - `func (s *Service) Claim(ctx, companyID, rawDomain string) (Challenge, error)`
  - `func (s *Service) Check(ctx, companyID, rawDomain string) (Result, error)`
  - `func (s *Service) SetStatus(ctx, companyID, status, reason string) error`
  - `Challenge{Domain, RecordName, RecordType, RecordValue string}`, `Result{Status string, Verified bool}`
  - sentinel errors: `ErrInvalidDomain`, `ErrNoClaim`, `ErrDomainTaken`
  - `func NewNetResolver() TXTResolver` (wraps `net.DefaultResolver`)

- [ ] **Step 1: Write the failing test**

`internal/trust/verification_test.go`:
```go
package trust

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

type fakeResolver struct{ records map[string][]string }

func (f *fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if r, ok := f.records[name]; ok {
		return r, nil
	}
	return nil, errors.New("no such host")
}

func newSvc(t *testing.T, res TXTResolver) *Service {
	q, pool := testQueriesOrSkip(t)
	return NewService(q, pool, res, zerolog.Nop())
}

func TestClaimReturnsChallenge(t *testing.T) {
	svc := newSvc(t, &fakeResolver{})
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "claim-co")

	ch, err := svc.Claim(context.Background(), cid, "  HTTPS://Acme-Claim.Example/path ")
	require.NoError(t, err)
	require.Equal(t, "acme-claim.example", ch.Domain, "normalized: lowercased, scheme/path stripped")
	require.Equal(t, "_tradebridge-verify.acme-claim.example", ch.RecordName)
	require.Equal(t, "TXT", ch.RecordType)
	require.Contains(t, ch.RecordValue, "tradebridge-verify=")

	got, err := q.GetCompanyVerification(context.Background(), cid)
	require.NoError(t, err)
	require.Equal(t, "pending", got.VerificationStatus)
}

func TestClaimRejectsBadDomain(t *testing.T) {
	svc := newSvc(t, &fakeResolver{})
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "claim-bad-co")
	for _, bad := range []string{"", "nodot", "com", "has space.com", "a..b.com"} {
		_, err := svc.Claim(context.Background(), cid, bad)
		require.ErrorIs(t, err, ErrInvalidDomain, "domain %q must be rejected", bad)
	}
}

func TestCheckMissThenMatch(t *testing.T) {
	res := &fakeResolver{records: map[string][]string{}}
	svc := newSvc(t, res)
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "check-co")

	ch, err := svc.Claim(context.Background(), cid, "check-co.example")
	require.NoError(t, err)

	// Miss: no TXT record yet → stays pending, no error.
	r, err := svc.Check(context.Background(), cid, "check-co.example")
	require.NoError(t, err)
	require.False(t, r.Verified)
	require.Equal(t, "pending", r.Status)

	// Publish the record, then match.
	res.records[ch.RecordName] = []string{ch.RecordValue}
	r, err = svc.Check(context.Background(), cid, "check-co.example")
	require.NoError(t, err)
	require.True(t, r.Verified)
	require.Equal(t, "verified", r.Status)

	got, err := q.GetCompanyVerification(context.Background(), cid)
	require.NoError(t, err)
	require.Equal(t, "verified", got.VerificationStatus)
	require.Equal(t, "check-co.example", *got.VerifiedDomain)
}

func TestCheckWithoutClaim(t *testing.T) {
	svc := newSvc(t, &fakeResolver{})
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "noclaim-co")
	_, err := svc.Check(context.Background(), cid, "noclaim.example")
	require.ErrorIs(t, err, ErrNoClaim)
}

func TestSetStatusRevokeClearsDomain(t *testing.T) {
	res := &fakeResolver{records: map[string][]string{}}
	svc := newSvc(t, res)
	q, _ := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "revoke-co")
	ch, _ := svc.Claim(context.Background(), cid, "revoke.example")
	res.records[ch.RecordName] = []string{ch.RecordValue}
	_, err := svc.Check(context.Background(), cid, "revoke.example")
	require.NoError(t, err)

	require.NoError(t, svc.SetStatus(context.Background(), cid, "rejected", "abuse"))
	got, err := q.GetCompanyVerification(context.Background(), cid)
	require.NoError(t, err)
	require.Equal(t, "rejected", got.VerificationStatus)
	require.Nil(t, got.VerifiedDomain, "revoke frees the domain")
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/trust/ -run TestClaim -v`
Expected: FAIL (compile error — `Service`/`NewService` not defined).

- [ ] **Step 3: Write the implementation**

`internal/trust/verification.go`:
```go
// Package trust provides seller verification (DNS-TXT domain ownership),
// abuse reports, and blocks. This file is the verification service.
package trust

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

const txtPrefix = "tradebridge-verify="
const recordHost = "_tradebridge-verify."

var (
	ErrInvalidDomain = errors.New("invalid domain")
	ErrNoClaim       = errors.New("no pending verification for this domain")
	ErrDomainTaken   = errors.New("domain already verified by another company")
)

// TXTResolver looks up TXT records. Interface so tests inject a fake (no real DNS).
type TXTResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

type netResolver struct {
	r       *net.Resolver
	timeout time.Duration
}

func (n netResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	return n.r.LookupTXT(ctx, name)
}

// NewNetResolver returns a real DNS resolver with the given per-lookup timeout.
func NewNetResolver(timeout time.Duration) TXTResolver {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return netResolver{r: net.DefaultResolver, timeout: timeout}
}

// Service performs domain verification.
type Service struct {
	q      *sqlc.Queries
	pool   *pgxpool.Pool
	res    TXTResolver
	logger zerolog.Logger
}

func NewService(q *sqlc.Queries, pool *pgxpool.Pool, resolver TXTResolver, logger zerolog.Logger) *Service {
	return &Service{q: q, pool: pool, res: resolver, logger: logger}
}

// Challenge is the DNS record the seller must publish.
type Challenge struct {
	Domain      string `json:"domain"`
	RecordName  string `json:"record_name"`
	RecordType  string `json:"record_type"`
	RecordValue string `json:"record_value"`
}

// Result is the outcome of a Check.
type Result struct {
	Status   string `json:"status"`
	Verified bool   `json:"verified"`
}

var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// validateDomain normalizes and validates a claimed domain. Strips scheme,
// path, port, and userinfo; lowercases; requires >= 2 valid labels.
func validateDomain(raw string) (string, error) {
	d := strings.TrimSpace(strings.ToLower(raw))
	if d == "" {
		return "", ErrInvalidDomain
	}
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	if i := strings.LastIndex(d, "@"); i >= 0 {
		d = d[i+1:]
	}
	if i := strings.Index(d, ":"); i >= 0 {
		d = d[:i]
	}
	d = strings.TrimSuffix(d, ".")
	if d == "" || len(d) > 253 || strings.Contains(d, " ") {
		return "", ErrInvalidDomain
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return "", ErrInvalidDomain
	}
	for _, l := range labels {
		if !labelRe.MatchString(l) {
			return "", ErrInvalidDomain
		}
	}
	return d, nil
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return txtPrefix + hex.EncodeToString(b)
}

// Claim starts (or restarts) a domain-ownership challenge.
func (s *Service) Claim(ctx context.Context, companyID, rawDomain string) (Challenge, error) {
	domain, err := validateDomain(rawDomain)
	if err != nil {
		return Challenge{}, err
	}
	token := newToken()
	if _, err := s.q.UpsertDomainVerification(ctx, sqlc.UpsertDomainVerificationParams{
		CompanyID: companyID, Domain: domain, Token: token,
	}); err != nil {
		return Challenge{}, err
	}
	// Move the company to pending unless it is already verified (a verified
	// company adding a second domain keeps its badge until the new one lands).
	cur, err := s.q.GetCompanyVerification(ctx, companyID)
	if err != nil {
		return Challenge{}, err
	}
	if cur.VerificationStatus != "verified" {
		if err := s.q.SetCompanyVerificationStatus(ctx, sqlc.SetCompanyVerificationStatusParams{
			CompanyID: companyID, Status: "pending",
		}); err != nil {
			return Challenge{}, err
		}
	}
	return Challenge{
		Domain:      domain,
		RecordName:  recordHost + domain,
		RecordType:  "TXT",
		RecordValue: token,
	}, nil
}

// Check resolves the TXT record and, on match, verifies the company atomically.
func (s *Service) Check(ctx context.Context, companyID, rawDomain string) (Result, error) {
	domain, err := validateDomain(rawDomain)
	if err != nil {
		return Result{}, err
	}
	dv, err := s.q.GetDomainVerification(ctx, sqlc.GetDomainVerificationParams{CompanyID: companyID, Domain: domain})
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, ErrNoClaim
	} else if err != nil {
		return Result{}, err
	}
	_ = s.q.TouchDomainVerificationAttempt(ctx, dv.ID)

	if dv.Status == "verified" {
		return Result{Status: "verified", Verified: true}, nil
	}

	records, lookupErr := s.res.LookupTXT(ctx, recordHost+domain)
	if lookupErr != nil {
		s.logger.Debug().Err(lookupErr).Str("domain", domain).Msg("txt lookup failed (treated as not-yet)")
	}
	matched := false
	for _, r := range records {
		if strings.TrimSpace(r) == dv.Token {
			matched = true
			break
		}
	}
	if !matched {
		return Result{Status: "pending", Verified: false}, nil
	}

	// Match → verify atomically: company + challenge + supplier sync.
	if err := s.verifyTx(ctx, companyID, domain, dv.ID); err != nil {
		return Result{}, err
	}
	s.logger.Info().Str("company_id", companyID).Str("domain", domain).Msg("company verified via DNS-TXT")
	return Result{Status: "verified", Verified: true}, nil
}

func (s *Service) verifyTx(ctx context.Context, companyID, domain, dvID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	if err := qtx.SetCompanyVerified(ctx, sqlc.SetCompanyVerifiedParams{ID: companyID, VerifiedDomain: &domain}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDomainTaken
		}
		return err
	}
	if err := qtx.MarkDomainVerificationVerified(ctx, dvID); err != nil {
		return err
	}
	verified := true
	if err := qtx.SyncSupplierVerified(ctx, sqlc.SyncSupplierVerifiedParams{CompanyID: companyID, Verified: verified}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SetStatus is the admin override/revoke. status ∈ verified|rejected|unverified.
func (s *Service) SetStatus(ctx context.Context, companyID, status, reason string) error {
	switch status {
	case "verified", "rejected", "unverified":
	default:
		return fmt.Errorf("invalid status %q", status)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if err := qtx.SetCompanyVerificationStatus(ctx, sqlc.SetCompanyVerificationStatusParams{
		CompanyID: companyID, Status: status,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDomainTaken
		}
		return err
	}
	if err := qtx.SyncSupplierVerified(ctx, sqlc.SyncSupplierVerifiedParams{
		CompanyID: companyID, Verified: status == "verified",
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.logger.Info().Str("company_id", companyID).Str("status", status).Str("reason", reason).Msg("admin set verification status")
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/trust/ -v`
Expected: PASS (all verification + query tests).

- [ ] **Step 5: Commit**

```bash
git add internal/trust/verification.go internal/trust/verification_test.go
git commit -m "feat(trust): DNS-TXT verification service with injectable resolver"
```

---

### Task 4: Verification HTTP handler + router wiring

**Files:**
- Create: `internal/trust/handler.go`
- Create: `internal/trust/handler_test.go`
- Modify: `internal/httpapi/router.go` (construct + register the trust handler)

**Interfaces:**
- Consumes: `*Service` (Task 3), `*sqlc.Queries`, `middleware.RequireVerified`, guard chain.
- Produces routes: `POST /v1/verification/domain`, `POST /v1/verification/domain/check`, `GET /v1/verification`.
- Produces: `func NewHandler(svc *Service, q *sqlc.Queries) *Handler`, `func (h *Handler) Register(app *fiber.App, verified fiber.Handler, guard ...fiber.Handler)`.

- [ ] **Step 1: Write the handler**

`internal/trust/handler.go`:
```go
package trust

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves seller verification routes.
type Handler struct {
	svc *Service
	q   *sqlc.Queries
}

func NewHandler(svc *Service, q *sqlc.Queries) *Handler {
	return &Handler{svc: svc, q: q}
}

// Register mounts verification routes. Mutating routes require owner/admin role
// and a verified email; check is additionally intended to be rate-limited
// (wired in Plan 3).
func (h *Handler) Register(app *fiber.App, verified fiber.Handler, guard ...fiber.Handler) {
	writeGuard := append(append([]fiber.Handler{}, guard...), verified, middleware.RequireRole("owner", "admin"))
	app.Post("/v1/verification/domain", append(append([]fiber.Handler{}, writeGuard...), h.claim)...)
	app.Post("/v1/verification/domain/check", append(append([]fiber.Handler{}, writeGuard...), h.check)...)
	app.Get("/v1/verification", append(append([]fiber.Handler{}, guard...), h.status)...)
}

type domainReq struct {
	Domain string `json:"domain"`
}

func (h *Handler) claim(c *fiber.Ctx) error {
	var req domainReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	ch, err := h.svc.Claim(c.Context(), middleware.CompanyID(c), req.Domain)
	if errors.Is(err, ErrInvalidDomain) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid domain")
	} else if err != nil {
		return err
	}
	return c.JSON(ch)
}

func (h *Handler) check(c *fiber.Ctx) error {
	var req domainReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	res, err := h.svc.Check(c.Context(), middleware.CompanyID(c), req.Domain)
	switch {
	case errors.Is(err, ErrInvalidDomain):
		return fiber.NewError(fiber.StatusBadRequest, "invalid domain")
	case errors.Is(err, ErrNoClaim):
		return fiber.NewError(fiber.StatusNotFound, "no pending verification for this domain")
	case errors.Is(err, ErrDomainTaken):
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"code": "domain_taken", "error": "domain already verified by another company"})
	case err != nil:
		return err
	}
	return c.JSON(res)
}

func (h *Handler) status(c *fiber.Ctx) error {
	cid := middleware.CompanyID(c)
	v, err := h.q.GetCompanyVerification(c.Context(), cid)
	if err != nil {
		return err
	}
	pending, err := h.q.ListPendingVerificationByCompany(c.Context(), cid)
	if err != nil {
		return err
	}
	claims := make([]fiber.Map, 0, len(pending))
	for _, p := range pending {
		claims = append(claims, fiber.Map{
			"domain":       p.Domain,
			"record_name":  recordHost + p.Domain,
			"record_type":  "TXT",
			"record_value": p.Token,
		})
	}
	return c.JSON(fiber.Map{
		"verification_status": v.VerificationStatus,
		"verified_domain":     v.VerifiedDomain,
		"verified_at":         v.VerifiedAt,
		"pending":             claims,
	})
}
```

- [ ] **Step 2: Write the handler test**

`internal/trust/handler_test.go`:
```go
package trust

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// stubIdentity injects company/role into ctx locals the way the JWT guard would.
func stubIdentity(companyID, role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("company_id", companyID)
		c.Locals("user_id", "u_"+companyID)
		c.Locals("role", role)
		return c.Next()
	}
}

func newTestApp(t *testing.T, res TXTResolver) (*fiber.App, *sqlc.Queries, string) {
	q, pool := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "handler-co")
	svc := NewService(q, pool, res, zerolog.Nop())
	app := fiber.New()
	// verified passthrough (email-verify is exercised elsewhere).
	verified := func(c *fiber.Ctx) error { return c.Next() }
	NewHandler(svc, q).Register(app, verified, stubIdentity(cid, "owner"), middleware.TenantGuard())
	return app, q, cid
}

func doJSON(t *testing.T, app *fiber.App, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestHandlerClaimThenCheck(t *testing.T) {
	res := &fakeResolver{records: map[string][]string{}}
	app, q, cid := newTestApp(t, res)

	code, out := doJSON(t, app, "POST", "/v1/verification/domain", map[string]string{"domain": "handler.example"})
	require.Equal(t, 200, code)
	recName := out["record_name"].(string)
	recVal := out["record_value"].(string)

	// Not published yet → pending.
	code, out = doJSON(t, app, "POST", "/v1/verification/domain/check", map[string]string{"domain": "handler.example"})
	require.Equal(t, 200, code)
	require.Equal(t, false, out["verified"])

	// Publish + check → verified.
	res.records[recName] = []string{recVal}
	code, out = doJSON(t, app, "POST", "/v1/verification/domain/check", map[string]string{"domain": "handler.example"})
	require.Equal(t, 200, code)
	require.Equal(t, true, out["verified"])

	v, _ := q.GetCompanyVerification(context.Background(), cid)
	require.Equal(t, "verified", v.VerificationStatus)
}

func TestHandlerCheckNoClaim(t *testing.T) {
	app, _, _ := newTestApp(t, &fakeResolver{})
	code, _ := doJSON(t, app, "POST", "/v1/verification/domain/check", map[string]string{"domain": "unclaimed.example"})
	require.Equal(t, 404, code)
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/trust/ -run TestHandler -v`
Expected: FAIL (compile error — handler not defined).

- [ ] **Step 4: Wire into the router**

In `internal/httpapi/router.go`, after the `suppliers.NewHandler(q).Register(f, guard...)` line, add construction and registration. Add the import `"github.com/tradebridge/core/internal/trust"` and a config field `VerifyDNSTimeout time.Duration` (used here). Insert:
```go
	trustSvc := trust.NewService(q, cfg.Pool, trust.NewNetResolver(cfg.VerifyDNSTimeout), cfg.Logger)
	trust.NewHandler(trustSvc, q).Register(f, middleware.RequireVerified(q), guard...)
```
Add to `httpapi.Config`:
```go
	// VerifyDNSTimeout bounds each DNS-TXT lookup during domain verification.
	VerifyDNSTimeout time.Duration
```

- [ ] **Step 5: Run the handler tests + build**

Run: `go build ./... && go test ./internal/trust/ -v`
Expected: build clean; PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/trust/handler.go internal/trust/handler_test.go internal/httpapi/router.go
git commit -m "feat(trust): verification HTTP handler (claim/check/status) + router wiring"
```

---

### Task 5: Admin override/revoke

**Files:**
- Create: `internal/trust/admin.go`
- Create: `internal/trust/admin_test.go`
- Modify: `internal/httpapi/router.go` (register inside the admin block)

**Interfaces:**
- Consumes: `*Service` (SetStatus), `*sqlc.Queries` (ListCompaniesByVerificationStatus).
- Produces routes: `GET /admin/v1/verifications?status=&limit=&offset=`, `POST /admin/v1/companies/:id/verification`.
- Produces: `func NewAdminHandler(svc *Service, q *sqlc.Queries) *AdminHandler`, `func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler)`.

- [ ] **Step 1: Write the admin handler**

`internal/trust/admin.go`:
```go
package trust

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// AdminHandler is the platform-admin override/revoke surface for verification.
type AdminHandler struct {
	svc *Service
	q   *sqlc.Queries
}

func NewAdminHandler(svc *Service, q *sqlc.Queries) *AdminHandler {
	return &AdminHandler{svc: svc, q: q}
}

func (h *AdminHandler) Register(app *fiber.App, adminGuard fiber.Handler) {
	g := app.Group("/admin/v1", adminGuard)
	g.Get("/verifications", h.list)
	g.Post("/companies/:id/verification", h.setStatus)
}

func (h *AdminHandler) list(c *fiber.Ctx) error {
	var status *string
	if s := c.Query("status"); s != "" {
		status = &s
	}
	limit := parseIntDefault(c.Query("limit"), 50, 500)
	offset := parseIntDefault(c.Query("offset"), 0, 1_000_000)
	rows, err := h.q.ListCompaniesByVerificationStatus(c.Context(), sqlc.ListCompaniesByVerificationStatusParams{
		Status: status, Lim: int32(limit), Off: int32(offset),
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"companies": rows, "count": len(rows)})
}

type setStatusReq struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (h *AdminHandler) setStatus(c *fiber.Ctx) error {
	companyID := c.Params("id")
	var req setStatusReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if err := h.svc.SetStatus(c.Context(), companyID, req.Status, req.Reason); err != nil {
		if err == ErrDomainTaken {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"code": "domain_taken"})
		}
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	v, err := h.q.GetCompanyVerification(c.Context(), companyID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"company_id": companyID, "verification_status": v.VerificationStatus})
}

// parseIntDefault clamps to [0,maxVal], falling back to def on empty/invalid.
func parseIntDefault(s string, def, maxVal int) int {
	if s == "" {
		return def
	}
	n := def
	if _, err := fmtSscan(s, &n); err != nil || n < 0 {
		return def
	}
	if n > maxVal {
		return maxVal
	}
	return n
}
```
Note: implement `parseIntDefault` using `strconv.Atoi` (import `strconv`) rather than the `fmtSscan` placeholder above — mirror `internal/suppliers/handler.go`'s exact `parseIntDefault`. (The suppliers package already has one; this package needs its own copy since Go has no cross-package private sharing.) Replace the body with the strconv version.

- [ ] **Step 2: Write the admin test**

`internal/trust/admin_test.go`:
```go
package trust

import (
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestAdminSetStatusVerifies(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	cid := makeCompany(t, q, "admin-verif-co")
	svc := NewService(q, pool, &fakeResolver{}, zerolog.Nop())
	app := fiber.New()
	pass := func(c *fiber.Ctx) error { return c.Next() }
	NewAdminHandler(svc, q).Register(app, pass)

	code, out := doJSON(t, app, "POST", "/admin/v1/companies/"+cid+"/verification", map[string]string{"status": "verified", "reason": "manual"})
	require.Equal(t, 200, code)
	require.Equal(t, "verified", out["verification_status"])
}

func TestAdminListFiltersByStatus(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	svc := NewService(q, pool, &fakeResolver{}, zerolog.Nop())
	app := fiber.New()
	pass := func(c *fiber.Ctx) error { return c.Next() }
	NewAdminHandler(svc, q).Register(app, pass)

	code, out := doJSON(t, app, "GET", "/admin/v1/verifications?status=verified&limit=5", nil)
	require.Equal(t, 200, code)
	require.Contains(t, out, "companies")
}
```
Note: `doJSON` with a `nil` body — adjust `doJSON` to send an empty body when `body == nil` (use `http.NoBody`). Update the Task 4 helper accordingly (make it tolerate nil), or add a `doGET` helper here.

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/trust/ -run TestAdmin -v`
Expected: FAIL (admin handler not defined).

- [ ] **Step 4: Register in the router admin block**

In `internal/httpapi/router.go`, inside `if cfg.AdminJWTSecret != ""` (after `entitlements.NewAdminHandler(...).Register(f, adminGuard)`), add:
```go
		trust.NewAdminHandler(trustSvc, q).Register(f, adminGuard)
```
(`trustSvc` is already constructed above in Task 4.)

- [ ] **Step 5: Run tests + build**

Run: `go build ./... && go test ./internal/trust/ -v`
Expected: build clean; all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/trust/admin.go internal/trust/admin_test.go internal/httpapi/router.go
git commit -m "feat(trust): admin verification override/revoke + list"
```

---

### Task 6: Match-pool gate (config + query + wiring)

**Files:**
- Modify: `internal/config/config.go` (add `VerifyRequiredForMatch`, `VerifyDNSTimeout`)
- Modify: `pkg/db/query/rfq.sql` (`MatchSuppliers` gains a `verified_only` arg)
- Modify (generated): `pkg/db/sqlc/*` via `sqlc generate`
- Modify: `internal/rfq/service.go` (`findMatches` passes the flag) and its constructor/config
- Modify: `internal/httpapi/router.go` and `cmd/api/main.go` + `cmd/gateway/main.go` (map the new config)
- Test: `internal/rfq/match_gate_test.go`

**Interfaces:**
- Consumes: `companies.verification_status`.
- Produces: `MatchSuppliers` filters unverified sellers when `verified_only` is true.

- [ ] **Step 1: Write the failing test**

`internal/rfq/match_gate_test.go`:
```go
package rfq

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func rfqQueriesOrSkip(t *testing.T) *sqlc.Queries {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool)
}

func TestMatchSuppliersVerifiedGate(t *testing.T) {
	q := rfqQueriesOrSkip(t)
	ctx := context.Background()

	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "gate-seller", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: seller.ID, Description: strPtr("widgets and gadgets"), Categories: []string{"widgets"}, Location: strPtr("NYC"),
	})
	require.NoError(t, err)

	// Unverified seller: appears when gate is OFF, hidden when ON.
	off, err := q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{ExcludeCompany: "buyer-x", Needle: "widgets", VerifiedOnly: false, Lim: 10})
	require.NoError(t, err)
	require.True(t, containsCompany(off, seller.ID), "gate off → unverified included")

	on, err := q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{ExcludeCompany: "buyer-x", Needle: "widgets", VerifiedOnly: true, Lim: 10})
	require.NoError(t, err)
	require.False(t, containsCompany(on, seller.ID), "gate on → unverified excluded")

	// Verify the seller, then it appears with the gate ON.
	require.NoError(t, q.SetCompanyVerified(ctx, sqlc.SetCompanyVerifiedParams{ID: seller.ID, VerifiedDomain: strPtr("gate-seller.example")}))
	on2, err := q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{ExcludeCompany: "buyer-x", Needle: "widgets", VerifiedOnly: true, Lim: 10})
	require.NoError(t, err)
	require.True(t, containsCompany(on2, seller.ID), "gate on + verified → included")
}

func strPtr(s string) *string { return &s }

func containsCompany(rows []sqlc.MatchSuppliersRow, id string) bool {
	for _, r := range rows {
		if r.CompanyID == id {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/rfq/ -run TestMatchSuppliersVerifiedGate -v`
Expected: FAIL (compile error — `VerifiedOnly` field does not exist).

- [ ] **Step 3: Update the query**

In `pkg/db/query/rfq.sql`, replace the `MatchSuppliers` query with:
```sql
-- name: MatchSuppliers :many
SELECT sp.company_id, c.name AS company_name, sp.categories, sp.description, sp.location
FROM supplier_profiles sp
JOIN companies c ON c.id = sp.company_id
WHERE sp.company_id <> sqlc.arg(exclude_company)
  AND (NOT sqlc.arg(verified_only)::boolean OR c.verification_status = 'verified')
  AND (
    EXISTS (
        SELECT 1 FROM unnest(sp.categories) cat
        WHERE cat ILIKE '%' || sqlc.arg(needle)::text || '%'
    )
    OR sp.description ILIKE '%' || sqlc.arg(needle)::text || '%'
  )
LIMIT sqlc.arg(lim);
```
Then run `sqlc generate`.

- [ ] **Step 4: Thread the flag through the service**

In `internal/rfq/service.go`:
- Add a field `verifyRequiredForMatch bool` to `Service`.
- Add a param to `NewService` (append `verifyRequiredForMatch bool` as the final param) OR add a setter; the plan uses a constructor param. Update the single call site in `internal/httpapi/router.go`.
- In `findMatches`, pass `VerifiedOnly: s.verifyRequiredForMatch` in the `MatchSuppliersParams`.

`findMatches` MatchSuppliers call becomes:
```go
		rows, err := s.q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{
			ExcludeCompany: buyerCompanyID,
			Needle:         needle,
			VerifiedOnly:   s.verifyRequiredForMatch,
			Lim:            maxMatches,
		})
```

- [ ] **Step 5: Add config**

In `internal/config/config.go`, add struct fields and defaults:
```go
	// VerifyRequiredForMatch gates the RFQ match pool to verified sellers only.
	VerifyRequiredForMatch bool `mapstructure:"VERIFY_REQUIRED_FOR_MATCH"`
	// VerifyDNSTimeout bounds each DNS-TXT lookup during domain verification.
	VerifyDNSTimeout time.Duration `mapstructure:"VERIFY_DNS_TIMEOUT"`
```
Add imports for `time` if missing, and defaults in `Load`:
```go
	v.SetDefault("VERIFY_REQUIRED_FOR_MATCH", false)
	v.SetDefault("VERIFY_DNS_TIMEOUT", "5s")
```
Viper decodes the `5s` string into `time.Duration` via its default string→Duration decode hook (Unmarshal already supports it). Confirm with a config test if one exists; otherwise trust viper's `StringToTimeDurationHookFunc` (default in `viper.Unmarshal`).

- [ ] **Step 6: Map config into the router in both mains**

In `cmd/api/main.go` and `cmd/gateway/main.go`, where `httpapi.Config{...}` (api) / gateway config is built from `*config.Config`, set `VerifyDNSTimeout: cfg.VerifyDNSTimeout`. In `internal/httpapi/router.go`, pass `cfg.VerifyRequiredForMatch` into `rfq.NewService(...)` and set the `httpapi.Config.VerifyRequiredForMatch bool` + `VerifyDNSTimeout` fields (add `VerifyRequiredForMatch bool` to `httpapi.Config`). Confirm the gateway also constructs an rfq service; if it does, thread the flag there too (grep `rfq.NewService`).

- [ ] **Step 7: Run tests + build**

Run: `go build ./... && go test ./internal/rfq/ -run TestMatchSuppliersVerifiedGate -v && go test ./...`
Expected: build clean; gate test PASS; full suite green (DB tests PASS with TEST_DATABASE_URL set, others unaffected).

- [ ] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(trust): config-gated verified-only match pool (default off)"
```

---

## Deferred / follow-ups (record in the ledger, not built here)

- HTTP-file verification method (needs SSRF guards) — deferred to a later sprint.
- Background re-check sweeper for pending verifications — pull-based only for now.
- Rate-limiting the check endpoint — lands in Plan 3.
- Verified badge surfaced in the UI + on quote/search read APIs — Plans 2 and 4.

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

	"github.com/tradebridge/core/internal/activation"
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
	if dv.Status == "verified" {
		return Result{Status: "verified", Verified: true}, nil
	}
	// Count the attempt only when we actually perform a lookup.
	_ = s.q.TouchDomainVerificationAttempt(ctx, dv.ID)

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
	activation.Emit(ctx, s.q, companyID, "", activation.EventVerified, nil)
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
	// On revoke/unverify, demote any verified challenge so a stale
	// domain_verifications row can't let Check report verified for a domain the
	// company no longer holds (e.g. after the domain is reassigned).
	if status != "verified" {
		if err := qtx.FailCompanyVerifiedDomains(ctx, companyID); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.logger.Info().Str("company_id", companyID).Str("status", status).Str("reason", reason).Msg("admin set verification status")
	if status == "verified" {
		activation.Emit(ctx, s.q, companyID, "", activation.EventVerified, nil)
	}
	return nil
}

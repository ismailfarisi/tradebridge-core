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

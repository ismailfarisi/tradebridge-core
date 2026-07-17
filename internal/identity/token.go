// Package identity owns authentication: password hashing, JWT issuance, company
// registration, login, and the teammate invite flow.
package identity

import (
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tradebridge/core/pkg/middleware"
)

// ImpersonationTTL bounds a support impersonation session.
const ImpersonationTTL = 30 * time.Minute

// TokenService issues signed JWTs carrying tenant + user + role claims.
type TokenService struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenService returns a token issuer signing with secret and the given TTL.
func NewTokenService(secret string, ttl time.Duration) *TokenService {
	return &TokenService{secret: []byte(secret), ttl: ttl}
}

// Issue returns a signed HS256 token for the given tenant/user/role.
func (s *TokenService) Issue(companyID, userID, role string) (string, error) {
	claims := &middleware.Claims{
		CompanyID: companyID,
		UserID:    userID,
		Role:      role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// IssueImpersonation mints a short-TTL, read-only company token for support. The
// token carries ReadOnly=true (EnforceReadOnly blocks writes) and records the
// acting admin. TTL is fixed at ImpersonationTTL, independent of login TTL.
func (s *TokenService) IssueImpersonation(companyID, userID, role, adminEmail string) (string, error) {
	claims := &middleware.Claims{
		CompanyID:   companyID,
		UserID:      userID,
		Role:        role,
		ReadOnly:    true,
		ActingAdmin: adminEmail,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ImpersonationTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

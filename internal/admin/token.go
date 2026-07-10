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

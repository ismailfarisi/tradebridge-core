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

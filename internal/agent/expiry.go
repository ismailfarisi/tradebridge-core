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

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

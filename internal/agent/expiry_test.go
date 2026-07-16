package agent

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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

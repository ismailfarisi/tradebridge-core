package agent

import (
	"testing"
	"time"
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

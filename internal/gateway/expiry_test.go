package gateway

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestCredExpired(t *testing.T) {
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	past := pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true}
	future := pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true}
	never := pgtype.Timestamptz{} // NULL
	if !credExpired(past, now) {
		t.Fatal("past expiry should be expired")
	}
	if credExpired(future, now) {
		t.Fatal("future expiry should not be expired")
	}
	if credExpired(never, now) {
		t.Fatal("NULL expiry never expires")
	}
}

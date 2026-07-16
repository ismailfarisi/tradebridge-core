// Package integration holds tests that require a live Postgres. They skip
// unless TEST_DATABASE_URL is set (CI sets it to the postgres service).
package integration

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func poolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// verifyUser marks userID as verified directly in the DB, bypassing the
// verify-email token flow. Tests that exercise other features (attachments,
// quotes, chat, ...) use this so a freshly registered user isn't blocked by
// the email-verification soft gate on RFQ create/chat.
func verifyUser(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE users SET email_verified_at = NOW() WHERE id = $1`, userID)
	require.NoError(t, err)
}

func TestGenULID(t *testing.T) {
	pool := poolOrSkip(t)
	var id string
	err := pool.QueryRow(context.Background(), "SELECT gen_ulid()").Scan(&id)
	require.NoError(t, err)
	require.Len(t, id, 26, "gen_ulid should return a 26-char ULID")
}

func TestCreateCompanyRoundTrip(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	created, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{
		Name: "Acme Trading",
		Type: "buyer",
	})
	require.NoError(t, err)
	require.Len(t, created.ID, 26)

	got, err := q.GetCompanyByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "Acme Trading", got.Name)
}

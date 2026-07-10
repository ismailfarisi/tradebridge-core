package rfq_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func poolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func uniqueEmail(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:]) + "@test.local"
}

func seedCompany(t *testing.T, q *sqlc.Queries, companyType string) string {
	t.Helper()
	co, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{
		Name: "sweeper-" + companyType, Type: companyType,
	})
	require.NoError(t, err)
	return co.ID
}

func seedUser(t *testing.T, q *sqlc.Queries, companyID string) string {
	t.Helper()
	u, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
		CompanyID: companyID, Email: uniqueEmail("sweeper-"), Role: "owner",
	})
	require.NoError(t, err)
	return u.ID
}

func TestSweeper_ExpiresOverdueOpenRFQ(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// Seed a matched company + an overdue open RFQ + a match row so the sweeper
	// has a seller to notify.
	buyer := seedCompany(t, q, "buyer")
	seller := seedCompany(t, q, "seller")
	user := seedUser(t, q, buyer)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer, CreatedBy: user, Title: "t", RawText: "x",
	})
	require.NoError(t, err)
	_, err = q.MarkRFQMatched(ctx, sqlc.MarkRFQMatchedParams{
		Requirements: []byte("{}"), Category: "", MatchCount: 1, ID: r.ID,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	require.NoError(t, err)
	_, err = q.CreateRFQMatch(ctx, sqlc.CreateRFQMatchParams{
		RfqID: r.ID, SellerCompanyID: seller, Score: 1, Reason: "x",
	})
	require.NoError(t, err)

	swp := rfq.NewSweeper(q, pool, eventbus.New(q, nil, zerolog.Nop()), zerolog.Nop(), time.Minute)
	n, err := swp.SweepOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got, err := q.GetRFQ(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, rfq.StatusExpired, got.Status)

	// Second sweep is a no-op (already expired).
	n2, err := swp.SweepOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n2)

	// Seller received a durable rfq.expired.
	evs, err := q.ReplayAgentEventsSince(ctx, sqlc.ReplayAgentEventsSinceParams{
		CompanyID: seller, AfterSeq: 0, Lim: 100,
	})
	require.NoError(t, err)
	var found bool
	for _, e := range evs {
		if e.Type == "rfq.expired" {
			found = true
		}
	}
	require.True(t, found, "matched seller must receive rfq.expired")
}

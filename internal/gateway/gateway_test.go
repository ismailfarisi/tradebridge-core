package gateway

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func gwQueriesOrSkip(t *testing.T) *sqlc.Queries {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return sqlc.New(pool)
}

// mkAgentCredential creates an active, never-expiring agent credential for
// companyID and returns the "<id>.<raw>" bearer value the gateway expects.
func mkAgentCredential(t *testing.T, q *sqlc.Queries, companyID string) string {
	t.Helper()
	rawSecret := "test-secret-" + companyID
	hash, err := identity.HashPassword(rawSecret)
	require.NoError(t, err)
	cred, err := q.CreateAgentCredential(context.Background(), sqlc.CreateAgentCredentialParams{
		CompanyID:  companyID,
		Label:      "test",
		SecretHash: hash,
	})
	require.NoError(t, err)
	return cred.ID + "." + rawSecret
}

// A suspended (or soft-deleted) company's agent credential must be rejected
// at the gateway even though the credential row itself is perfectly valid —
// otherwise a suspended seller's agent keeps submitting quotes/commands,
// defeating the suspension. An active company's agent must keep working.
func TestAuthenticateRejectsSuspendedOrDeletedCompany(t *testing.T) {
	q := gwQueriesOrSkip(t)
	ctx := context.Background()

	active, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "gw-active-co", Type: "seller"})
	require.NoError(t, err)
	activeToken := mkAgentCredential(t, q, active.ID)

	suspended, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "gw-suspended-co", Type: "seller"})
	require.NoError(t, err)
	suspendedToken := mkAgentCredential(t, q, suspended.ID)
	require.NoError(t, q.SetCompanyStatus(ctx, sqlc.SetCompanyStatusParams{ID: suspended.ID, Status: "suspended"}))

	deleted, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "gw-deleted-co", Type: "seller"})
	require.NoError(t, err)
	deletedToken := mkAgentCredential(t, q, deleted.ID)
	require.NoError(t, q.SoftDeleteAndAnonymizeCompany(ctx, deleted.ID))

	h := &Handler{q: q, logger: zerolog.Nop(), rl: &rateLimiter{logger: zerolog.Nop()}}
	app := fiber.New()
	app.Get("/agent/v1/heartbeat-probe", func(c *fiber.Ctx) error {
		if _, err := h.authenticate(c); err != nil {
			return err
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	doReq := func(token string) int {
		req := httptest.NewRequest("GET", "/agent/v1/heartbeat-probe", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := app.Test(req, -1)
		require.NoError(t, err)
		return resp.StatusCode
	}

	require.Equal(t, 200, doReq(activeToken), "active company's agent must authenticate")
	require.NotEqual(t, 200, doReq(suspendedToken), "suspended company's agent must be rejected")
	require.NotEqual(t, 200, doReq(deletedToken), "deleted company's agent must be rejected")
}

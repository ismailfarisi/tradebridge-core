package waitlist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func doJSON(t *testing.T, app *fiber.App, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, http.NoBody)
	} else {
		var buf bytes.Buffer
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
		req = httptest.NewRequest(method, path, &buf)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func testQueriesOrSkip(t *testing.T) *sqlc.Queries {
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

// stubAdminGuard mimics the platform admin guard middleware without requiring
// a real JWT, mirroring internal/adminconsole/handler_test.go's harness.
func stubAdminGuard() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.Next()
	}
}

// seedSignup inserts a pending signup with a unique email (so reruns against
// a persistent test DB never collide with a prior run's already-invited row
// via the join handler's ON CONFLICT DO UPDATE ... RETURNING).
func seedSignup(t *testing.T, q *sqlc.Queries, prefix string) sqlc.WaitlistSignup {
	t.Helper()
	email := fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
	row, err := q.CreateWaitlistSignup(context.Background(), sqlc.CreateWaitlistSignupParams{Email: email})
	require.NoError(t, err)
	require.False(t, row.InvitedAt.Valid, "freshly seeded signup must start pending")
	return row
}

func TestInviteSendsToAllPendingAndMarksInvited(t *testing.T) {
	q := testQueriesOrSkip(t)
	s1 := seedSignup(t, q, "invite-a")
	s2 := seedSignup(t, q, "invite-b")

	app := fiber.New()
	NewAdminHandler(q, notify.LogSender{}, "https://switeaz.com", zerolog.Nop()).Register(app, stubAdminGuard())

	code, out := doJSON(t, app, "POST", "/admin/v1/waitlist/invite", nil)
	require.Equal(t, 200, code, "%v", out)

	sent, ok := out["sent"].(float64)
	require.True(t, ok, "%v", out)
	require.GreaterOrEqual(t, sent, float64(2))

	attempted, ok := out["attempted"].(float64)
	require.True(t, ok, "%v", out)
	require.Equal(t, sent, attempted)

	// Both seeded rows should now show invited_at set.
	invitedRows, err := q.ListWaitlist(context.Background(), sqlc.ListWaitlistParams{StatusFilter: strPtr("invited"), Lim: 500})
	require.NoError(t, err)
	invitedIDs := map[string]bool{}
	for _, r := range invitedRows {
		invitedIDs[r.ID] = true
	}
	require.True(t, invitedIDs[s1.ID], "signup 1 should be invited")
	require.True(t, invitedIDs[s2.ID], "signup 2 should be invited")

	pendingRows, err := q.ListWaitlist(context.Background(), sqlc.ListWaitlistParams{StatusFilter: strPtr("pending"), Lim: 500})
	require.NoError(t, err)
	for _, r := range pendingRows {
		require.NotEqual(t, s1.ID, r.ID)
		require.NotEqual(t, s2.ID, r.ID)
	}
}

func TestListWaitlistFiltersByStatus(t *testing.T) {
	q := testQueriesOrSkip(t)
	pending := seedSignup(t, q, "list-pending")

	app := fiber.New()
	NewAdminHandler(q, notify.LogSender{}, "https://switeaz.com", zerolog.Nop()).Register(app, stubAdminGuard())

	code, out := doJSON(t, app, "GET", "/admin/v1/waitlist?status=pending", nil)
	require.Equal(t, 200, code, "%v", out)
	rows, ok := out["signups"].([]any)
	require.True(t, ok)
	found := false
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok && m["id"] == pending.ID {
			found = true
		}
	}
	require.True(t, found, "pending signup should appear in pending filter")

	code, out = doJSON(t, app, "GET", "/admin/v1/waitlist?status=invited", nil)
	require.Equal(t, 200, code, "%v", out)
	rows, ok = out["signups"].([]any)
	require.True(t, ok)
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			require.NotEqual(t, pending.ID, m["id"], "pending signup must not appear in invited filter")
		}
	}
}

func strPtr(s string) *string { return &s }

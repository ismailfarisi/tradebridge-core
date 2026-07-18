package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

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

// stubIdentity injects the locals TenantGuard() would derive from real JWT
// claims, so it fully replaces the JWT+TenantGuard(+RequireActiveCompany+
// EnforceReadOnly) chain in these tests.
func stubIdentity(companyID, userID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals("company_id", companyID)
		c.Locals("user_id", userID)
		c.Locals("role", "owner")
		return c.Next()
	}
}

func seedCompanyAndUser(t *testing.T, q *sqlc.Queries, name string) (companyID, userID string) {
	t.Helper()
	co, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: name, Type: "buyer"})
	require.NoError(t, err)
	u, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
		CompanyID: co.ID,
		Email:     "u-" + co.ID + "@feedback.test",
		Role:      "owner",
	})
	require.NoError(t, err)
	return co.ID, u.ID
}

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

func TestCreateFeedbackNPSWithScore(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-nps-ok")

	app := fiber.New()
	NewHandler(q).Register(app, stubIdentity(cid, uid))

	code, out := doJSON(t, app, "POST", "/v1/feedback", map[string]any{"kind": "nps", "score": 9})
	require.Equal(t, 201, code, "%v", out)
	require.Equal(t, "nps", out["kind"])
	require.EqualValues(t, 9, out["score"])
}

func TestCreateFeedbackNPSMissingScore(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-nps-noscore")

	app := fiber.New()
	NewHandler(q).Register(app, stubIdentity(cid, uid))

	code, _ := doJSON(t, app, "POST", "/v1/feedback", map[string]any{"kind": "nps"})
	require.Equal(t, 400, code)
}

func TestCreateFeedbackNPSScoreOutOfRange(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-nps-outofrange")

	app := fiber.New()
	NewHandler(q).Register(app, stubIdentity(cid, uid))

	code, _ := doJSON(t, app, "POST", "/v1/feedback", map[string]any{"kind": "nps", "score": 11})
	require.Equal(t, 400, code)
}

func TestCreateFeedbackMessageEmpty(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-msg-empty")

	app := fiber.New()
	NewHandler(q).Register(app, stubIdentity(cid, uid))

	code, _ := doJSON(t, app, "POST", "/v1/feedback", map[string]any{"kind": "feedback", "message": ""})
	require.Equal(t, 400, code)
}

func TestCreateFeedbackMessageOK(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-msg-ok")

	app := fiber.New()
	NewHandler(q).Register(app, stubIdentity(cid, uid))

	code, out := doJSON(t, app, "POST", "/v1/feedback", map[string]any{"kind": "feedback", "message": "hi"})
	require.Equal(t, 201, code, "%v", out)
	require.Equal(t, "feedback", out["kind"])
	require.Equal(t, "hi", out["message"])
}

func TestCreateFeedbackMessageWhitespaceOnlyRejected(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-msg-whitespace")

	app := fiber.New()
	NewHandler(q).Register(app, stubIdentity(cid, uid))

	code, _ := doJSON(t, app, "POST", "/v1/feedback", map[string]any{"kind": "feedback", "message": "   "})
	require.Equal(t, 400, code)
}

// A stray out-of-range score on a "feedback" kind must not reach the DB's
// CHECK (score BETWEEN 0 AND 10) constraint — that would surface as an
// unwrapped *pgconn.PgError and a 500 from Fiber's default error handler.
// The handler clears score for "feedback" kind, so this must 201 with a null
// score rather than 500.
func TestCreateFeedbackWithStrayOutOfRangeScoreIgnoresScore(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-stray-score")

	app := fiber.New()
	NewHandler(q).Register(app, stubIdentity(cid, uid))

	code, out := doJSON(t, app, "POST", "/v1/feedback", map[string]any{"kind": "feedback", "message": "hi", "score": 99})
	require.Equal(t, 201, code, "%v", out)
	require.Equal(t, "feedback", out["kind"])
	require.Nil(t, out["score"])
}

func TestCreateFeedbackBogusKind(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-bogus")

	app := fiber.New()
	NewHandler(q).Register(app, stubIdentity(cid, uid))

	code, _ := doJSON(t, app, "POST", "/v1/feedback", map[string]any{"kind": "bogus"})
	require.Equal(t, 400, code)
}

func TestAdminListFeedbackNewestFirst(t *testing.T) {
	q := testQueriesOrSkip(t)
	cid, uid := seedCompanyAndUser(t, q, "fb-admin-list")

	older, err := q.CreateFeedback(context.Background(), sqlc.CreateFeedbackParams{
		CompanyID: cid, UserID: uid, Kind: "nps", Score: int32Ptr(3),
	})
	require.NoError(t, err)
	// A real clock tick between the two inserts, so ordering can't be a
	// same-timestamp coincidence: created_at is TIMESTAMPTZ (microsecond
	// resolution), but two inserts back-to-back in-process can otherwise
	// land in the same tick on a fast machine.
	time.Sleep(2 * time.Millisecond)
	newer, err := q.CreateFeedback(context.Background(), sqlc.CreateFeedbackParams{
		CompanyID: cid, UserID: uid, Kind: "nps", Score: int32Ptr(8),
	})
	require.NoError(t, err)

	app := fiber.New()
	NewHandler(q).RegisterAdmin(app, func(c *fiber.Ctx) error { return c.Next() })

	code, out := doJSON(t, app, "GET", "/admin/v1/feedback", nil)
	require.Equal(t, 200, code, "%v", out)
	rows, ok := out["feedback"].([]any)
	require.True(t, ok)

	// The list spans every tenant's feedback (admin-wide), not just this
	// test's rows, so locate our two rows by id and assert their relative
	// order — dropping ORDER BY created_at DESC would put `older` before
	// `newer` (or make the order unstable) and fail this.
	indexOf := func(id string) int {
		for i, r := range rows {
			if m, ok := r.(map[string]any); ok && m["id"] == id {
				return i
			}
		}
		return -1
	}
	newerIdx, olderIdx := indexOf(newer.ID), indexOf(older.ID)
	require.GreaterOrEqual(t, newerIdx, 0, "newer row missing from list")
	require.GreaterOrEqual(t, olderIdx, 0, "older row missing from list")
	require.Less(t, newerIdx, olderIdx, "newer row should be listed before older row (newest-first)")
}

func int32Ptr(v int32) *int32 { return &v }

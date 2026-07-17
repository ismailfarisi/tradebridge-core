package assist

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
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

// stubGuard injects a company id into locals the way TenantGuard would.
func stubGuard(companyID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalCompanyID, companyID)
		return c.Next()
	}
}

// TestImproveRFQEndpoint_FreeCompanyUnderCap verifies a Free-plan company
// (no active subscription) can call /v1/assist/rfq while under its monthly
// cap and gets back a 200 with a non-empty suggestion, using the keyless
// deterministic stub (no network).
func TestImproveRFQEndpoint_FreeCompanyUnderCap(t *testing.T) {
	q := testQueriesOrSkip(t)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "assist-h-free-co", Type: "buyer"})
	require.NoError(t, err)

	entSvc := entitlements.NewService(q, zerolog.Nop())
	svc := NewService("", zerolog.Nop())

	app := fiber.New()
	NewHandler(svc, entSvc, q).Register(app, stubGuard(co.ID))

	body := strings.NewReader(`{"draft":"need bolts"}`)
	req := httptest.NewRequest("POST", "/v1/assist/rfq", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var out struct {
		Suggestion string `json:"suggestion"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Contains(t, out.Suggestion, "bolts")
}

// TestImproveRFQEndpoint_FreeCompanyOverCapGetsPaywall verifies that once a
// Free company has used its 20 assist calls this period, the endpoint
// returns the paywall status entitlements.Fail maps a *PaywallError to
// (402 Payment Required — see entitlements.Fail).
func TestImproveRFQEndpoint_FreeCompanyOverCapGetsPaywall(t *testing.T) {
	q := testQueriesOrSkip(t)
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "assist-h-capped-co", Type: "buyer"})
	require.NoError(t, err)

	entSvc := entitlements.NewService(q, zerolog.Nop())
	svc := NewService("", zerolog.Nop())

	// Drive the company to its cap via the entitlements service directly,
	// against the same DB the handler test uses.
	for i := 0; i < 20; i++ {
		entSvc.IncrementAssistUsage(ctx, co.ID)
	}

	app := fiber.New()
	NewHandler(svc, entSvc, q).Register(app, stubGuard(co.ID))

	body := strings.NewReader(`{"draft":"need bolts"}`)
	req := httptest.NewRequest("POST", "/v1/assist/rfq", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusPaymentRequired, resp.StatusCode)

	var out struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, "upgrade_required", out.Code)
}

// TestCompareEndpoint_NonBuyerGets404 verifies /v1/assist/compare doesn't
// leak the RFQ's existence to a company that isn't its buyer.
func TestCompareEndpoint_NonBuyerGets404(t *testing.T) {
	q := testQueriesOrSkip(t)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "assist-h-buyer", Type: "buyer"})
	require.NoError(t, err)
	stranger, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "assist-h-stranger", Type: "buyer"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@assisth.test", Role: "owner"})
	require.NoError(t, err)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need bolts", RawText: "bolts"})
	require.NoError(t, err)

	entSvc := entitlements.NewService(q, zerolog.Nop())
	svc := NewService("", zerolog.Nop())

	app := fiber.New()
	NewHandler(svc, entSvc, q).Register(app, stubGuard(stranger.ID))

	body := strings.NewReader(`{"rfq_id":"` + r.ID + `"}`)
	req := httptest.NewRequest("POST", "/v1/assist/compare", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 404, resp.StatusCode)
}

// TestCompareEndpoint_BuyerWithQuotesGetsExplanation verifies the happy path:
// the buyer gets a 200 with a non-empty explanation naming a seller, and the
// call counts against their assist quota.
func TestCompareEndpoint_BuyerWithQuotesGetsExplanation(t *testing.T) {
	q := testQueriesOrSkip(t)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "assist-h-cmp-buyer", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "assist-h-cmp-seller", Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@assisth.test", Role: "owner"})
	require.NoError(t, err)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need bolts", RawText: "bolts"})
	require.NoError(t, err)
	_, err = q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: r.ID, SellerCompanyID: seller.ID, PriceCents: 1000, Currency: "USD", Notes: "", Completeness: 1,
	})
	require.NoError(t, err)

	entSvc := entitlements.NewService(q, zerolog.Nop())
	svc := NewService("", zerolog.Nop())

	app := fiber.New()
	NewHandler(svc, entSvc, q).Register(app, stubGuard(buyer.ID))

	before := assistUsageCounter(t, q, buyer.ID)

	body := strings.NewReader(`{"rfq_id":"` + r.ID + `"}`)
	req := httptest.NewRequest("POST", "/v1/assist/compare", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)

	var out struct {
		Explanation string `json:"explanation"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.NotEmpty(t, out.Explanation)

	after := assistUsageCounter(t, q, buyer.ID)
	require.Equal(t, before+1, after, "a served compare with quotes counts against the assist quota")
}

// assistUsageCounter reads the current-period "assist" usage_counters row
// directly (0 when absent), mirroring entitlements' unexported period()/
// metricAssist without depending on them.
func assistUsageCounter(t *testing.T, q *sqlc.Queries, companyID string) int32 {
	t.Helper()
	n, err := q.GetUsageCounter(context.Background(), sqlc.GetUsageCounterParams{
		CompanyID: companyID, Metric: "assist", Period: time.Now().UTC().Format("2006-01"),
	})
	if err != nil {
		return 0
	}
	return n
}

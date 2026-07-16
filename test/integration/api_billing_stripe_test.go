package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/billing"
	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/internal/httpapi"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestBillingService_ApplyEventDrivesPlanAndIsIdempotent(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	svc := billing.NewService(q, zerolog.Nop())
	ent := entitlements.NewService(q, zerolog.Nop())
	ctx := context.Background()

	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "PayCo", Type: "both"})
	require.NoError(t, err)
	require.Equal(t, "free", ent.PlanCode(ctx, co.ID))

	// checkout.session.completed → active Pro.
	activate := billing.WebhookEvent{
		ID: "evt_activate_" + co.ID, CompanyID: co.ID, CustomerID: "cus_" + co.ID,
		SubscriptionID: "sub_" + co.ID, Plan: "pro", Interval: "month", Status: "active",
	}
	require.NoError(t, svc.ApplyWebhookEvent(ctx, activate))
	require.Equal(t, "pro", ent.PlanCode(ctx, co.ID))
	require.True(t, ent.For(ctx, co.ID).AgentAutonomy)

	// Re-delivery of the SAME event id is a no-op (idempotent).
	require.NoError(t, svc.ApplyWebhookEvent(ctx, activate))
	require.Equal(t, "pro", ent.PlanCode(ctx, co.ID))

	// past_due keeps the plan (grace).
	require.NoError(t, svc.ApplyWebhookEvent(ctx, billing.WebhookEvent{
		ID: "evt_pastdue_" + co.ID, CompanyID: co.ID, CustomerID: "cus_" + co.ID,
		SubscriptionID: "sub_" + co.ID, Plan: "pro", Interval: "month", Status: "past_due",
	}))
	require.Equal(t, "pro", ent.PlanCode(ctx, co.ID))

	// canceled → Free.
	require.NoError(t, svc.ApplyWebhookEvent(ctx, billing.WebhookEvent{
		ID: "evt_cancel_" + co.ID, CompanyID: co.ID, CustomerID: "cus_" + co.ID,
		SubscriptionID: "sub_" + co.ID, Plan: "pro", Interval: "month", Status: "canceled",
	}))
	require.Equal(t, "free", ent.PlanCode(ctx, co.ID))
	require.False(t, ent.For(ctx, co.ID).AgentAutonomy)
}

func TestBillingHTTP_CheckoutWebhookUnlocksPro(t *testing.T) {
	app := buildAPI(t)

	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "HttpPayCo", "company_type": "both",
		"email": uniqueEmail("pay-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := reg["token"].(string)
	companyID := reg["company_id"].(string)

	// Free before.
	st, sum := doJSON(t, app, "GET", "/v1/billing/summary", token, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "free", sum["plan"])

	// Checkout returns a URL (stub) and stores the Stripe customer id.
	st, co := doJSON(t, app, "POST", "/v1/billing/checkout", token, map[string]any{"plan": "pro", "interval": "month"})
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, co["url"].(string), "stub")

	// Stripe confirms via webhook (stub parses the normalized envelope directly).
	st, _ = doJSON(t, app, "POST", "/v1/billing/webhook", "", map[string]any{
		"id": "evt_http_" + companyID, "type": "checkout.session.completed",
		"company_id": companyID, "customer_id": "cus_stub_" + companyID,
		"subscription_id": "sub_http", "plan": "pro", "interval": "month", "status": "active",
	})
	require.Equal(t, http.StatusOK, st)

	// Now Pro: autonomy unlocked, rfq limit 100.
	st, sum = doJSON(t, app, "GET", "/v1/billing/summary", token, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "pro", sum["plan"])
	ent := sum["entitlements"].(map[string]any)
	require.Equal(t, true, ent["agent_autonomy"])
	require.Equal(t, float64(100), ent["rfq_monthly_limit"])

	// Portal deep-link works once a customer exists.
	st, portal := doJSON(t, app, "POST", "/v1/billing/portal", token, nil)
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, portal["url"].(string), "stub")

	// Cancellation downgrades to Free.
	st, _ = doJSON(t, app, "POST", "/v1/billing/webhook", "", map[string]any{
		"id": "evt_cancel_http_" + companyID, "type": "customer.subscription.deleted",
		"company_id": companyID, "customer_id": "cus_stub_" + companyID, "status": "canceled",
	})
	require.Equal(t, http.StatusOK, st)
	st, sum = doJSON(t, app, "GET", "/v1/billing/summary", token, nil)
	require.Equal(t, "free", sum["plan"])
}

// TestBillingWebhook_RefusedWhenUnconfigured proves the fail-closed interlock:
// on a keyless deploy with BILLING_DEV_MODE unset (the default), the public
// webhook must NOT parse an unsigned forged event — it should refuse before
// ever reaching StubBilling.ParseWebhook.
func TestBillingWebhook_RefusedWhenUnconfigured(t *testing.T) {
	pool := poolOrSkip(t)
	// Build an app WITHOUT a Stripe key and WITHOUT dev mode → billing inactive.
	app := httpapi.New(sqlc.New(pool), httpapi.Config{
		JWTSecret: "test-secret", Logger: zerolog.Nop(), Pool: pool, AttachmentsDir: t.TempDir(),
		// BillingDevMode defaults false; no Stripe key → provider is stub, Enabled()==false.
	})
	st, _ := doJSON(t, app, "POST", "/v1/billing/webhook", "", map[string]any{
		"id": "evt_forged", "company_id": "co_x", "plan": "pro", "status": "active",
	})
	require.Equal(t, http.StatusServiceUnavailable, st) // fails closed — no unsigned parse, no state change
}

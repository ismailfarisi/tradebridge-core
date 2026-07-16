package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/billing"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// TestAgentEntitlements_ReflectsPlan is E2E #2's core: the entitlement the agent
// reads flips from Free (autonomy off) to Pro (autonomy on) once a billing
// webhook activates the company's subscription.
func TestAgentEntitlements_ReflectsPlan(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	_, companyID, secret := registerSeller(t, api, "AutonomyCo")

	// Free by default → autonomy off.
	st, ent := doJSON(t, gw, "GET", "/agent/v1/entitlements", secret, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, false, ent["agent_autonomy"])
	require.Equal(t, "free", ent["plan"])

	// A billing webhook activates Pro.
	require.NoError(t, billing.NewService(q, zerolog.Nop()).ApplyWebhookEvent(ctx, billing.WebhookEvent{
		ID: "evt_autonomy_" + companyID, CompanyID: companyID, CustomerID: "cus_" + companyID,
		SubscriptionID: "sub_" + companyID, Plan: "pro", Interval: "month", Status: "active",
	}))

	// Now autonomy is unlocked.
	st, ent = doJSON(t, gw, "GET", "/agent/v1/entitlements", secret, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, ent["agent_autonomy"])
	require.Equal(t, "pro", ent["plan"])

	// An unauthenticated request is rejected (credential required).
	st, _ = doJSON(t, gw, "GET", "/agent/v1/entitlements", "", nil)
	require.Equal(t, http.StatusUnauthorized, st)
}

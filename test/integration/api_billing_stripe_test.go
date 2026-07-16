package integration

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/billing"
	"github.com/tradebridge/core/internal/entitlements"
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

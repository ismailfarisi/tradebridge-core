package billing

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStub_RoundTripsWebhookEnvelope(t *testing.T) {
	s := NewStub()
	require.False(t, s.Enabled())

	ev := WebhookEvent{
		ID: "evt_1", CompanyID: "co_1", CustomerID: "cus_1", SubscriptionID: "sub_1",
		Plan: "pro", Interval: "month", Status: "active",
	}
	payload, err := json.Marshal(ev)
	require.NoError(t, err)

	got, err := s.ParseWebhook(payload, "")
	require.NoError(t, err)
	require.Equal(t, ev.ID, got.ID)
	require.Equal(t, "pro", got.Plan)
	require.Equal(t, "active", got.Status)
}

func TestStub_CheckoutAndCustomerAreDeterministic(t *testing.T) {
	s := NewStub()
	cust, err := s.EnsureCustomer(context.Background(), "co_42", "a@b.com", "")
	require.NoError(t, err)
	require.Equal(t, "cus_stub_co_42", cust)

	url, err := s.CreateCheckoutSession(context.Background(), CheckoutParams{
		CompanyID: "co_42", CustomerID: cust, Plan: "pro", Interval: "month",
		SuccessURL: "s", CancelURL: "c",
	})
	require.NoError(t, err)
	require.Contains(t, url, "stub")
}

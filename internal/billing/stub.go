package billing

import (
	"context"
	"encoding/json"
	"fmt"
)

// StubBilling is a deterministic, network-free Billing used in tests and when
// STRIPE_SECRET_KEY is unset. Its ParseWebhook reads a normalized WebhookEvent
// straight from the JSON body (no signature), so tests can drive the full
// checkout→webhook→entitlement flow without Stripe.
type StubBilling struct{}

// NewStub builds the stub provider.
func NewStub() *StubBilling { return &StubBilling{} }

func (s *StubBilling) Enabled() bool { return false }

func (s *StubBilling) EnsureCustomer(_ context.Context, companyID, _, existing string) (string, error) {
	if existing != "" {
		return existing, nil
	}
	return "cus_stub_" + companyID, nil
}

func (s *StubBilling) CreateCheckoutSession(_ context.Context, p CheckoutParams) (string, error) {
	return fmt.Sprintf("https://stub.local/checkout?company=%s&plan=%s&interval=%s", p.CompanyID, p.Plan, p.Interval), nil
}

func (s *StubBilling) CreatePortalSession(_ context.Context, customerID, returnURL string) (string, error) {
	return fmt.Sprintf("https://stub.local/portal?customer=%s&return=%s", customerID, returnURL), nil
}

func (s *StubBilling) ParseWebhook(payload []byte, _ string) (WebhookEvent, error) {
	var ev WebhookEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return WebhookEvent{}, err
	}
	return ev, nil
}

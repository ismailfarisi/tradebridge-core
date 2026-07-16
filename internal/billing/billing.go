// Package billing integrates Stripe for subscriptions and drives the
// subscriptions table that internal/entitlements reads. Stripe types are
// confined to stripebilling.go; the rest of the system speaks WebhookEvent.
package billing

import "context"

// CheckoutParams describes a Stripe Checkout session to open.
type CheckoutParams struct {
	CompanyID     string
	CustomerID    string // Stripe customer id (ensure it first)
	CustomerEmail string
	Plan          string // pro | business
	Interval      string // month | year
	SuccessURL    string
	CancelURL     string
}

// WebhookEvent is the normalized, provider-agnostic result of parsing a Stripe
// webhook. Status uses our subscription vocabulary
// (trialing|active|past_due|canceled|unpaid|incomplete). Type is informational.
type WebhookEvent struct {
	ID                string `json:"id"`
	Type              string `json:"type"`
	CompanyID         string `json:"company_id"`
	CustomerID        string `json:"customer_id"`
	SubscriptionID    string `json:"subscription_id"`
	Plan              string `json:"plan"`     // free|pro|business
	Interval          string `json:"interval"` // month|year
	Status            string `json:"status"`
	CurrentPeriodEnd  int64  `json:"current_period_end"` // unix seconds; 0 = unknown
	CancelAtPeriodEnd bool   `json:"cancel_at_period_end"`
	// Ignore marks an event we recognize but don't act on (keeps handler simple).
	Ignore bool `json:"ignore"`
}

// Billing is the provider port. StripeBilling and StubBilling implement it.
type Billing interface {
	// Enabled reports whether a real Stripe provider is configured.
	Enabled() bool
	// EnsureCustomer returns a Stripe customer id for the company, creating one
	// if existingCustomerID is empty.
	EnsureCustomer(ctx context.Context, companyID, email, existingCustomerID string) (string, error)
	// CreateCheckoutSession returns a redirect URL for the given plan/interval.
	CreateCheckoutSession(ctx context.Context, p CheckoutParams) (string, error)
	// CreatePortalSession returns a Stripe Customer Portal deep-link.
	CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, error)
	// ParseWebhook verifies + normalizes a webhook request body.
	ParseWebhook(payload []byte, sigHeader string) (WebhookEvent, error)
}

package billing

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/rs/zerolog"
	stripe "github.com/stripe/stripe-go/v82"
	billingportal "github.com/stripe/stripe-go/v82/billingportal/session"
	checkout "github.com/stripe/stripe-go/v82/checkout/session"
	"github.com/stripe/stripe-go/v82/customer"
	"github.com/stripe/stripe-go/v82/webhook"
)

// StripeBilling is the real provider. Constructed only when a secret key is set.
type StripeBilling struct {
	cfg    StripeConfig
	logger zerolog.Logger
}

func newStripeBilling(cfg StripeConfig, logger zerolog.Logger) *StripeBilling {
	stripe.Key = cfg.SecretKey
	return &StripeBilling{cfg: cfg, logger: logger}
}

func (b *StripeBilling) Enabled() bool { return true }

func (b *StripeBilling) EnsureCustomer(ctx context.Context, companyID, email, existing string) (string, error) {
	if existing != "" {
		return existing, nil
	}
	params := &stripe.CustomerParams{Email: stripe.String(email)}
	params.AddMetadata("company_id", companyID)
	c, err := customer.New(params)
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

func (b *StripeBilling) CreateCheckoutSession(ctx context.Context, p CheckoutParams) (string, error) {
	price := b.cfg.Prices.priceFor(p.Plan, p.Interval)
	if price == "" {
		return "", errors.New("no Stripe price configured for " + p.Plan + "/" + p.Interval)
	}
	params := &stripe.CheckoutSessionParams{
		Mode:              stripe.String(stripe.CheckoutSessionModeSubscription),
		Customer:          stripe.String(p.CustomerID),
		ClientReferenceID: stripe.String(p.CompanyID),
		SuccessURL:        stripe.String(p.SuccessURL),
		CancelURL:         stripe.String(p.CancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(price), Quantity: stripe.Int64(1)},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{},
	}
	params.SubscriptionData.AddMetadata("company_id", p.CompanyID)
	params.SubscriptionData.AddMetadata("plan", p.Plan)
	// Enable automatic tax (real Tax needs the live account; safe to request).
	params.AutomaticTax = &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)}
	sess, err := checkout.New(params)
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}

func (b *StripeBilling) CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	sess, err := billingportal.New(params)
	if err != nil {
		return "", err
	}
	return sess.URL, nil
}

// ParseWebhook verifies the Stripe signature and normalizes the event. Only the
// subscription-lifecycle events we act on are mapped; others are marked Ignore.
func (b *StripeBilling) ParseWebhook(payload []byte, sigHeader string) (WebhookEvent, error) {
	event, err := webhook.ConstructEvent(payload, sigHeader, b.cfg.WebhookSecret)
	if err != nil {
		return WebhookEvent{}, err
	}
	out := WebhookEvent{ID: event.ID, Type: string(event.Type)}

	switch event.Type {
	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return WebhookEvent{}, err
		}
		if sub.Customer != nil {
			out.CustomerID = sub.Customer.ID
		}
		out.SubscriptionID = sub.ID
		out.CompanyID = sub.Metadata["company_id"]
		out.Plan = sub.Metadata["plan"]
		out.Status = normalizeStatus(string(sub.Status))
		out.CancelAtPeriodEnd = sub.CancelAtPeriodEnd
		out.CurrentPeriodEnd = subCurrentPeriodEnd(&sub)
		out.Interval = subInterval(&sub)
		if event.Type == "customer.subscription.deleted" {
			out.Status = "canceled"
		}
	case "checkout.session.completed":
		var cs stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &cs); err != nil {
			return WebhookEvent{}, err
		}
		out.CompanyID = cs.ClientReferenceID
		if cs.Customer != nil {
			out.CustomerID = cs.Customer.ID
		}
		if cs.Subscription != nil {
			out.SubscriptionID = cs.Subscription.ID
		}
		out.Status = "active"
	case "invoice.payment_failed":
		var inv stripe.Invoice
		if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
			return WebhookEvent{}, err
		}
		if inv.Customer != nil {
			out.CustomerID = inv.Customer.ID
		}
		out.Status = "past_due"
	default:
		out.Ignore = true
	}
	return out, nil
}

// normalizeStatus maps Stripe subscription statuses to our vocabulary.
func normalizeStatus(s string) string {
	switch s {
	case "trialing", "active", "past_due", "canceled", "unpaid", "incomplete":
		return s
	case "incomplete_expired":
		return "canceled"
	default:
		return "incomplete"
	}
}

// subCurrentPeriodEnd extracts current_period_end from the subscription's first
// item. In stripe-go v82, current_period_end moved from Subscription onto the
// SubscriptionItem; 0 means unknown (the state machine treats 0 as NULL).
func subCurrentPeriodEnd(sub *stripe.Subscription) int64 {
	if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0] != nil {
		return sub.Items.Data[0].CurrentPeriodEnd
	}
	return 0
}

// subInterval extracts month/year from the subscription's first price recurrence.
func subInterval(sub *stripe.Subscription) string {
	if sub.Items != nil && len(sub.Items.Data) > 0 && sub.Items.Data[0].Price != nil && sub.Items.Data[0].Price.Recurring != nil {
		if string(sub.Items.Data[0].Price.Recurring.Interval) == "year" {
			return "year"
		}
	}
	return "month"
}

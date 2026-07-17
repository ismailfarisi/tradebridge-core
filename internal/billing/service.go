package billing

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/activation"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Service applies normalized webhook events to the subscriptions table. It is
// idempotent per Stripe event id and upserts the single row per company.
type Service struct {
	q      *sqlc.Queries
	logger zerolog.Logger
}

// NewService builds the billing service.
func NewService(q *sqlc.Queries, logger zerolog.Logger) *Service {
	return &Service{q: q, logger: logger}
}

// ApplyWebhookEvent records the event (idempotent) and updates the company's
// subscription row. Unknown/ignored events are recorded and skipped.
func (s *Service) ApplyWebhookEvent(ctx context.Context, ev WebhookEvent) error {
	if ev.ID != "" {
		already, err := s.q.IsStripeEventProcessed(ctx, ev.ID)
		if err != nil {
			return err
		}
		if already {
			return nil // idempotent no-op
		}
	}

	if !ev.Ignore {
		if err := s.applyState(ctx, ev); err != nil {
			return err
		}
	}

	if ev.ID != "" {
		if err := s.q.MarkStripeEventProcessed(ctx, sqlc.MarkStripeEventProcessedParams{
			EventID: ev.ID, Type: ev.Type,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyState(ctx context.Context, ev WebhookEvent) error {
	companyID, err := s.resolveCompany(ctx, ev)
	if err != nil {
		return err
	}
	if companyID == "" {
		s.logger.Warn().Str("event", ev.ID).Str("customer", ev.CustomerID).Msg("billing: event with no resolvable company, skipping")
		return nil
	}

	// Keep companies.stripe_customer_id in sync (first time we learn it).
	if ev.CustomerID != "" {
		if err := s.q.SetCompanyStripeCustomer(ctx, sqlc.SetCompanyStripeCustomerParams{
			ID: companyID, StripeCustomerID: ptr(ev.CustomerID),
		}); err != nil {
			return err
		}
	}

	status := ev.Status
	if status == "" {
		status = "incomplete"
	}
	var periodEnd pgtype.Timestamptz
	if ev.CurrentPeriodEnd > 0 {
		periodEnd = pgtype.Timestamptz{Time: time.Unix(ev.CurrentPeriodEnd, 0).UTC(), Valid: true}
	}
	interval := ev.Interval
	if interval != "month" && interval != "year" {
		interval = "month"
	}
	plan := ev.Plan
	if plan == "" {
		plan = "pro"
	}

	_, err = s.q.UpsertSubscription(ctx, sqlc.UpsertSubscriptionParams{
		CompanyID:            companyID,
		StripeCustomerID:     ptrOrNil(ev.CustomerID),
		StripeSubscriptionID: ptrOrNil(ev.SubscriptionID),
		Plan:                 plan,
		Interval:             interval,
		Status:               status,
		CurrentPeriodEnd:     periodEnd,
		CancelAtPeriodEnd:    ev.CancelAtPeriodEnd,
	})
	if err != nil {
		return err
	}
	// Only a paid plan on an active-ish subscription is an "upgrade" — never a
	// cancellation/past_due event (which also flows through applyState with the
	// plan still set). Idempotent per (company, upgrade), so the legitimate
	// first paid activation records once.
	if (plan == "pro" || plan == "business") && (status == "active" || status == "trialing") {
		activation.Emit(ctx, s.q, companyID, "", activation.EventUpgrade, map[string]any{"plan": plan})
	}
	return nil
}

// resolveCompany prefers the event's explicit company id, falling back to a
// lookup by Stripe customer id.
func (s *Service) resolveCompany(ctx context.Context, ev WebhookEvent) (string, error) {
	if ev.CompanyID != "" {
		return ev.CompanyID, nil
	}
	if ev.CustomerID == "" {
		return "", nil
	}
	co, err := s.q.GetCompanyByStripeCustomer(ctx, ptr(ev.CustomerID))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return co.ID, nil
}

func ptr(s string) *string { return &s }

// ptrOrNil returns nil for an empty string so the column stores NULL.
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

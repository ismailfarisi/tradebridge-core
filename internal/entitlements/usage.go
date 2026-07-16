package entitlements

import (
	"context"
	"errors"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

const metricRFQ = "rfq"

// PaywallError signals a request blocked by an entitlement limit. Fail renders
// it as HTTP 402.
type PaywallError struct {
	Feature      string
	PlanRequired string
}

func (e *PaywallError) Error() string {
	return "upgrade required: " + e.Feature + " (needs " + e.PlanRequired + ")"
}

// Fail renders a *PaywallError as a 402 with the paywall contract body; any
// other error is returned unchanged for the default error handler.
func Fail(c *fiber.Ctx, err error) error {
	var pw *PaywallError
	if errors.As(err, &pw) {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"code":          "upgrade_required",
			"feature":       pw.Feature,
			"plan_required": pw.PlanRequired,
		})
	}
	return err
}

// Usage is the current usage-vs-limit snapshot for a company.
type Usage struct {
	RFQUsed      int `json:"rfq_used"`
	RFQLimit     int `json:"rfq_limit"`
	CatalogUsed  int `json:"catalog_used"`
	CatalogLimit int `json:"catalog_limit"`
	SeatsUsed    int `json:"seats_used"`
	SeatsLimit   int `json:"seats_limit"`
}

// period is the current 'YYYY-MM' usage bucket in UTC.
func period() string { return time.Now().UTC().Format("2006-01") }

// planRequiredFor returns the cheapest plan whose limit for `feature` clears
// `used`. Boolean features (autonomy) always need Pro. For numeric limits, Pro
// is tried first, then Business.
func (s *Service) planRequiredFor(ctx context.Context, feature string, used int) string {
	for _, code := range []string{"pro", "business"} {
		p, err := s.plan(ctx, code)
		if err != nil {
			continue
		}
		var lim int32
		switch feature {
		case "catalog_max_items":
			lim = p.CatalogMaxItems
		case metricRFQ, "rfq_monthly_limit":
			lim = p.RfqMonthlyLimit
		case "seats":
			lim = p.Seats
		case "agent_autonomy":
			if p.AgentAutonomy {
				return code
			}
			continue
		}
		if lim == -1 || int(lim) > used {
			return code
		}
	}
	return "business"
}

// rfqUsed reads the current-period RFQ counter (0 when absent).
func (s *Service) rfqUsed(ctx context.Context, companyID string) int {
	n, err := s.q.GetUsageCounter(ctx, sqlc.GetUsageCounterParams{
		CompanyID: companyID, Metric: metricRFQ, Period: period(),
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			s.logger.Warn().Err(err).Str("company", companyID).Msg("entitlements: rfq usage read failed, treating as 0")
		}
		return 0
	}
	return int(n)
}

// CheckRFQQuota returns a *PaywallError when the company is at/over its monthly
// RFQ cap. Unlimited (-1) never blocks.
func (s *Service) CheckRFQQuota(ctx context.Context, companyID string) error {
	limit := s.For(ctx, companyID).RFQMonthlyLimit
	if limit == -1 {
		return nil
	}
	used := s.rfqUsed(ctx, companyID)
	if used >= limit {
		return &PaywallError{Feature: "rfq_monthly_limit", PlanRequired: s.planRequiredFor(ctx, "rfq_monthly_limit", used)}
	}
	return nil
}

// IncrementRFQUsage bumps the current-period RFQ counter. Best-effort: a
// failure is logged, not surfaced (the RFQ was already created).
func (s *Service) IncrementRFQUsage(ctx context.Context, companyID string) {
	if _, err := s.q.IncrementUsageCounter(ctx, sqlc.IncrementUsageCounterParams{
		CompanyID: companyID, Metric: metricRFQ, Period: period(),
	}); err != nil {
		s.logger.Warn().Err(err).Str("company", companyID).Msg("entitlements: rfq usage increment failed")
	}
}

// CheckCatalogQuota returns a *PaywallError when adding one more catalog item
// would exceed the plan's cap.
func (s *Service) CheckCatalogQuota(ctx context.Context, companyID string) error {
	limit := s.For(ctx, companyID).CatalogMaxItems
	if limit == -1 {
		return nil
	}
	n, err := s.q.CountCatalogByCompany(ctx, companyID)
	if err != nil {
		return err
	}
	if int(n) >= limit {
		return &PaywallError{Feature: "catalog_max_items", PlanRequired: s.planRequiredFor(ctx, "catalog_max_items", int(n))}
	}
	return nil
}

// CheckSeatQuota returns a *PaywallError when the company has no free seat.
// A seat is consumed by an existing user OR a pending (unaccepted) invite.
func (s *Service) CheckSeatQuota(ctx context.Context, companyID string) error {
	limit := s.For(ctx, companyID).Seats
	if limit == -1 {
		return nil
	}
	used, err := s.seatsUsed(ctx, companyID)
	if err != nil {
		return err
	}
	if used >= limit {
		return &PaywallError{Feature: "seats", PlanRequired: s.planRequiredFor(ctx, "seats", used)}
	}
	return nil
}

func (s *Service) seatsUsed(ctx context.Context, companyID string) (int, error) {
	users, err := s.q.CountUsersByCompany(ctx, companyID)
	if err != nil {
		return 0, err
	}
	pending, err := s.q.CountPendingInvitesByCompany(ctx, companyID)
	if err != nil {
		return 0, err
	}
	return int(users) + int(pending), nil
}

// Usage returns the current usage-vs-limit snapshot. Counts that fail resolve
// to 0 used (fail-open on the read, not the gate).
func (s *Service) Usage(ctx context.Context, companyID string) Usage {
	ent := s.For(ctx, companyID)
	cat, _ := s.q.CountCatalogByCompany(ctx, companyID)
	seats, _ := s.seatsUsed(ctx, companyID)
	return Usage{
		RFQUsed:      s.rfqUsed(ctx, companyID),
		RFQLimit:     ent.RFQMonthlyLimit,
		CatalogUsed:  int(cat),
		CatalogLimit: ent.CatalogMaxItems,
		SeatsUsed:    seats,
		SeatsLimit:   ent.Seats,
	}
}

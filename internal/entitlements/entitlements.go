// Package entitlements resolves a company's plan and the feature limits that
// plan grants, and meters usage against those limits. Plan values live in the
// `plans` table (admin-editable); a company's plan comes from its active
// subscription row, defaulting to Free. Everything fails closed to Free.
package entitlements

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Entitlements is the set of feature limits a plan grants. A limit of -1 means
// unlimited.
type Entitlements struct {
	AgentAutonomy   bool `json:"agent_autonomy"`
	CatalogMaxItems int  `json:"catalog_max_items"`
	RFQMonthlyLimit int  `json:"rfq_monthly_limit"`
	Seats           int  `json:"seats"`
}

// freeFallback is returned whenever resolution fails, so callers never receive
// a paid entitlement on error.
var freeFallback = Entitlements{AgentAutonomy: false, CatalogMaxItems: 25, RFQMonthlyLimit: 10, Seats: 2}

// Service resolves plans and entitlements over the query layer, caching plan
// rows in-process (invalidated when the admin edits a plan).
type Service struct {
	q      *sqlc.Queries
	logger zerolog.Logger

	mu    sync.RWMutex
	cache map[string]sqlc.Plan
	gen   uint64
}

// NewService builds the entitlements service.
func NewService(q *sqlc.Queries, logger zerolog.Logger) *Service {
	return &Service{q: q, logger: logger, cache: map[string]sqlc.Plan{}}
}

// InvalidateCache drops all cached plan rows. Call after a plan edit.
func (s *Service) InvalidateCache() {
	s.mu.Lock()
	s.cache = map[string]sqlc.Plan{}
	s.gen++
	s.mu.Unlock()
}

// PlanCode returns the company's current plan code, or "free" when it has no
// active subscription. Fail-closed to "free" on any error.
func (s *Service) PlanCode(ctx context.Context, companyID string) string {
	sub, err := s.q.GetActiveSubscription(ctx, companyID)
	if err != nil {
		return "free" // includes pgx.ErrNoRows (the common case) — no active sub => Free
	}
	return sub.Plan
}

// For resolves the company's entitlements. Free defaults on any error.
func (s *Service) For(ctx context.Context, companyID string) Entitlements {
	plan, err := s.plan(ctx, s.PlanCode(ctx, companyID))
	if err != nil {
		s.logger.Warn().Err(err).Str("company", companyID).Msg("entitlements: plan lookup failed, defaulting to Free")
		return freeFallback
	}
	return Entitlements{
		AgentAutonomy:   plan.AgentAutonomy,
		CatalogMaxItems: int(plan.CatalogMaxItems),
		RFQMonthlyLimit: int(plan.RfqMonthlyLimit),
		Seats:           int(plan.Seats),
	}
}

// plan returns a plan row, using the cache when warm.
func (s *Service) plan(ctx context.Context, code string) (sqlc.Plan, error) {
	s.mu.RLock()
	p, ok := s.cache[code]
	gen := s.gen
	s.mu.RUnlock()
	if ok {
		return p, nil
	}
	p, err := s.q.GetPlan(ctx, code)
	if err != nil {
		return sqlc.Plan{}, err
	}
	s.mu.Lock()
	if s.gen == gen { // don't cache a row fetched before a concurrent InvalidateCache
		s.cache[code] = p
	}
	s.mu.Unlock()
	return p, nil
}

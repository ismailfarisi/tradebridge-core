// Package dashboard serves tenant-scoped business aggregations for buyer and
// seller home screens.
package dashboard

import (
	"context"
	"time"

	"github.com/tradebridge/core/internal/entitlements"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

const recentRFQLimit = 8

// agentFreshWindow: a credential seen within this window is considered online.
const agentFreshWindow = 5 * time.Minute

// Service runs dashboard aggregation queries.
type Service struct {
	q   *sqlc.Queries
	ent *entitlements.Service
}

func NewService(q *sqlc.Queries, ent *entitlements.Service) *Service { return &Service{q: q, ent: ent} }

// RecentRFQ is a compact RFQ row for the dashboard recent list.
type RecentRFQ struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	QuoteCount int64  `json:"quote_count"`
	NewQuotes  int64  `json:"new_quotes"`
	CreatedAt  string `json:"created_at"`
}

// BuyerDashboard is the buyer home summary.
type BuyerDashboard struct {
	ActiveRFQs       int64       `json:"active_rfqs"`
	QuotesReceived   int64       `json:"quotes_received"`
	NewQuotes        int64       `json:"new_quotes"`
	OrdersInProgress int64       `json:"orders_in_progress"`
	RecentRFQs       []RecentRFQ `json:"recent_rfqs"`
}

// BuyerDashboard computes the buyer home summary for a company.
func (s *Service) BuyerDashboard(ctx context.Context, companyID string) (BuyerDashboard, error) {
	counts, err := s.q.BuyerDashboardCounts(ctx, companyID)
	if err != nil {
		return BuyerDashboard{}, err
	}
	recent, err := s.q.ListBuyerRecentRFQs(ctx, sqlc.ListBuyerRecentRFQsParams{
		CompanyID: companyID,
		Lim:       recentRFQLimit,
	})
	if err != nil {
		return BuyerDashboard{}, err
	}
	out := BuyerDashboard{
		ActiveRFQs:       counts.ActiveRfqs,
		QuotesReceived:   counts.QuotesReceived,
		NewQuotes:        counts.NewQuotes,
		OrdersInProgress: counts.OrdersInProgress,
		RecentRFQs:       make([]RecentRFQ, 0, len(recent)),
	}
	for _, r := range recent {
		out.RecentRFQs = append(out.RecentRFQs, RecentRFQ{
			ID:         r.ID,
			Title:      r.Title,
			Status:     r.Status,
			QuoteCount: r.QuoteCount,
			NewQuotes:  r.NewQuotes,
			CreatedAt:  r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return out, nil
}

// AgentStatus is the seller's connected-agent state.
type AgentStatus struct {
	Online   bool    `json:"online"`
	LastSeen *string `json:"last_seen"`
	Autonomy bool    `json:"autonomy"`
}

// PlanUsage is the seller's plan + usage snapshot.
type PlanUsage struct {
	Plan         string `json:"plan"`
	RFQUsed      int    `json:"rfq_used"`
	RFQLimit     int    `json:"rfq_limit"`
	CatalogUsed  int    `json:"catalog_used"`
	CatalogLimit int    `json:"catalog_limit"`
	SeatsUsed    int    `json:"seats_used"`
	SeatsLimit   int    `json:"seats_limit"`
}

// SellerDashboard is the seller home summary.
type SellerDashboard struct {
	IncomingRFQs int64       `json:"incoming_rfqs"`
	ReviewQueue  int64       `json:"review_queue"`
	ActiveOrders int64       `json:"active_orders"`
	Agent        AgentStatus `json:"agent"`
	PlanUsage    PlanUsage   `json:"plan_usage"`
}

// SellerDashboard computes the seller home summary for a company.
func (s *Service) SellerDashboard(ctx context.Context, companyID string) (SellerDashboard, error) {
	counts, err := s.q.SellerDashboardCounts(ctx, companyID)
	if err != nil {
		return SellerDashboard{}, err
	}
	presence, err := s.q.GetAgentPresence(ctx, companyID)
	if err != nil {
		return SellerDashboard{}, err
	}
	agent := AgentStatus{Autonomy: s.ent.For(ctx, companyID).AgentAutonomy}
	// presence.LastSeen is generated as `interface{}` (sqlc could not infer a
	// concrete nullable type from the scalar-subquery MAX). pgx decodes a
	// non-NULL timestamptz into time.Time when scanning into `any`, and leaves
	// it nil on SQL NULL — so a type assertion covers both cases safely.
	if presence.HasValid {
		if ts, ok := presence.LastSeen.(time.Time); ok {
			iso := ts.UTC().Format("2006-01-02T15:04:05Z07:00")
			agent.LastSeen = &iso
			agent.Online = time.Since(ts) <= agentFreshWindow
		}
	}
	u := s.ent.Usage(ctx, companyID)
	return SellerDashboard{
		IncomingRFQs: counts.IncomingRfqs,
		ReviewQueue:  counts.ReviewQueue,
		ActiveOrders: counts.ActiveOrders,
		Agent:        agent,
		PlanUsage: PlanUsage{
			Plan:         s.ent.PlanCode(ctx, companyID),
			RFQUsed:      u.RFQUsed,
			RFQLimit:     u.RFQLimit,
			CatalogUsed:  u.CatalogUsed,
			CatalogLimit: u.CatalogLimit,
			SeatsUsed:    u.SeatsUsed,
			SeatsLimit:   u.SeatsLimit,
		},
	}, nil
}

// TopBuyer is a buyer ranked by revenue won from this seller.
type TopBuyer struct {
	CompanyID    string `json:"company_id"`
	Name         string `json:"name"`
	Orders       int64  `json:"orders"`
	RevenueCents int64  `json:"revenue_cents"`
}

// SellerAnalytics is the seller's quote/order performance summary.
type SellerAnalytics struct {
	QuotesSubmitted  int64      `json:"quotes_submitted"`
	QuotesWon        int64      `json:"quotes_won"`
	WinRate          float64    `json:"win_rate"` // 0..1; quotes_won / decided
	RevenueWonCents  int64      `json:"revenue_won_cents"`
	AvgResponseHours float64    `json:"avg_response_hours"`
	TopBuyers        []TopBuyer `json:"top_buyers"`
}

// SellerAnalytics computes win rate, revenue, response time, and top buyers.
func (s *Service) SellerAnalytics(ctx context.Context, companyID string) (SellerAnalytics, error) {
	a, err := s.q.SellerAnalytics(ctx, companyID)
	if err != nil {
		return SellerAnalytics{}, err
	}
	buyers, err := s.q.SellerTopBuyers(ctx, companyID)
	if err != nil {
		return SellerAnalytics{}, err
	}
	winRate := 0.0
	if a.Decided > 0 {
		winRate = float64(a.QuotesWon) / float64(a.Decided)
	}
	out := SellerAnalytics{
		QuotesSubmitted:  a.QuotesSubmitted,
		QuotesWon:        a.QuotesWon,
		WinRate:          winRate,
		RevenueWonCents:  a.RevenueWonCents,
		AvgResponseHours: a.AvgResponseHours,
		TopBuyers:        make([]TopBuyer, 0, len(buyers)),
	}
	for _, b := range buyers {
		out.TopBuyers = append(out.TopBuyers, TopBuyer{
			CompanyID:    b.CompanyID,
			Name:         b.Name,
			Orders:       b.Orders,
			RevenueCents: b.RevenueCents,
		})
	}
	return out, nil
}

// Package dashboard serves tenant-scoped business aggregations for buyer and
// seller home screens.
package dashboard

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

const recentRFQLimit = 8

// Service runs dashboard aggregation queries.
type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

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

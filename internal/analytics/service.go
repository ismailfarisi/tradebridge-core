// Package analytics computes platform-wide business metrics on-the-fly (admin realm).
package analytics

import (
	"context"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type Service struct{ q *sqlc.Queries }

func NewService(q *sqlc.Queries) *Service { return &Service{q: q} }

type Summary struct {
	GMVCents      int64        `json:"gmv_cents"`
	Orders        int64        `json:"orders"`
	GMVByWeek     []WeekPoint  `json:"gmv_by_week"`
	ActiveBuyers  int64        `json:"active_buyers"`
	ActiveSellers int64        `json:"active_sellers"`
	Funnel        []FunnelStep `json:"funnel"`
	PlanMix       []PlanMixRow `json:"plan_mix"`
	MRRCents      *int64       `json:"mrr_cents"` // nil when prices unset
	PricesSet     bool         `json:"prices_set"`
	ActiveSubs    int64        `json:"active_subs"`
	CanceledSubs  int64        `json:"canceled_subs"`
	ChurnPct      float64      `json:"churn_pct"`
}

type WeekPoint struct {
	Week     string `json:"week"`
	GMVCents int64  `json:"gmv_cents"`
}

type FunnelStep struct {
	Event     string `json:"event"`
	Companies int64  `json:"companies"`
}

type PlanMixRow struct {
	PlanCode  string `json:"plan"`
	SubStatus string `json:"status"`
	Count     int64  `json:"count"`
}

func (s *Service) Business(ctx context.Context) (Summary, error) {
	var out Summary
	if g, err := s.q.AnalyticsGMV(ctx); err == nil {
		out.GMVCents, out.Orders = g.GmvCents, g.Orders
	}
	if w, err := s.q.AnalyticsGMVByWeek(ctx); err == nil {
		for _, r := range w {
			out.GMVByWeek = append(out.GMVByWeek, WeekPoint{Week: r.Week.Time.Format("2006-01-02"), GMVCents: r.GmvCents})
		}
	}
	if a, err := s.q.AnalyticsActiveCompanies(ctx); err == nil {
		out.ActiveBuyers, out.ActiveSellers = a.ActiveBuyers, a.ActiveSellers
	}
	if f, err := s.q.AnalyticsFunnel(ctx); err == nil {
		for _, r := range f {
			out.Funnel = append(out.Funnel, FunnelStep{Event: r.Event, Companies: r.Companies})
		}
	}
	// Plan mix + MRR + churn.
	mix, err := s.q.AnalyticsSubscriptionMix(ctx)
	if err == nil {
		var mrr int64
		pricesSet := true
		active := func(st string) bool { return st == "active" || st == "trialing" }
		for _, r := range mix {
			out.PlanMix = append(out.PlanMix, PlanMixRow{PlanCode: r.Plan, SubStatus: r.Status, Count: r.N})
			if active(r.Status) {
				out.ActiveSubs += r.N
				switch {
				case r.PriceCentsMonth != nil:
					mrr += int64(*r.PriceCentsMonth) * r.N
				case r.PriceCentsYear != nil:
					mrr += (int64(*r.PriceCentsYear) / 12) * r.N
				default:
					pricesSet = false
				}
			}
			if r.Status == "canceled" {
				out.CanceledSubs += r.N
			}
		}
		if pricesSet && out.ActiveSubs > 0 {
			out.MRRCents = &mrr
		}
		out.PricesSet = pricesSet
		if total := out.ActiveSubs + out.CanceledSubs; total > 0 {
			out.ChurnPct = float64(out.CanceledSubs) / float64(total) * 100.0
		}
	}
	return out, nil
}

package rfq

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// maxMatches caps how many sellers a single RFQ fans out to.
const maxMatches = 25

// Service orchestrates RFQ creation: extract → match → persist → broadcast.
type Service struct {
	q         *sqlc.Queries
	redis     *redis.Client // may be nil; broadcast still persists to agent_events
	extractor Extractor
	logger    zerolog.Logger
}

// NewService builds the RFQ service. redis may be nil (events are still durably
// written to agent_events; only the live publish is skipped).
func NewService(q *sqlc.Queries, rdb *redis.Client, extractor Extractor, logger zerolog.Logger) *Service {
	return &Service{q: q, redis: rdb, extractor: extractor, logger: logger}
}

// MatchResult is one matched seller, returned to the buyer after creation.
type MatchResult struct {
	SellerCompanyID string  `json:"seller_company_id"`
	SellerName      string  `json:"seller_name"`
	Score           float64 `json:"score"`
	Reason          string  `json:"reason"`
}

// matchedEvent is the `rfq.matched` payload delivered to a seller's agent.
type matchedEvent struct {
	Type           string       `json:"type"`
	RFQID          string       `json:"rfq_id"`
	Title          string       `json:"title"`
	BuyerCompanyID string       `json:"buyer_company_id"`
	Requirements   Requirements `json:"requirements"`
	Score          float64      `json:"score"`
	Reason         string       `json:"reason"`
}

// CreateAndMatch runs the full pipeline for a new RFQ and returns the opened
// RFQ plus the sellers it matched.
func (s *Service) CreateAndMatch(ctx context.Context, buyerCompanyID, userID, title, text string) (sqlc.Rfq, []MatchResult, error) {
	rfq, err := s.q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyerCompanyID,
		CreatedBy:      userID,
		Title:          title,
		RawText:        text,
	})
	if err != nil {
		return sqlc.Rfq{}, nil, err
	}

	req, err := s.extractor.Extract(ctx, title, text)
	if err != nil {
		return sqlc.Rfq{}, nil, err
	}

	matches, err := s.findMatches(ctx, buyerCompanyID, req)
	if err != nil {
		return sqlc.Rfq{}, nil, err
	}

	// Persist matches and broadcast one durable event per matched seller.
	for _, m := range matches {
		if _, err := s.q.CreateRFQMatch(ctx, sqlc.CreateRFQMatchParams{
			RfqID:           rfq.ID,
			SellerCompanyID: m.SellerCompanyID,
			Score:           m.Score,
			Reason:          m.Reason,
		}); err != nil {
			return sqlc.Rfq{}, nil, err
		}
		if err := s.broadcast(ctx, rfq, req, m); err != nil {
			return sqlc.Rfq{}, nil, err
		}
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return sqlc.Rfq{}, nil, err
	}
	opened, err := s.q.MarkRFQMatched(ctx, sqlc.MarkRFQMatchedParams{
		Requirements: reqJSON,
		Category:     req.Category,
		MatchCount:   int32(len(matches)),
		ID:           rfq.ID,
	})
	if err != nil {
		return sqlc.Rfq{}, nil, err
	}
	return opened, matches, nil
}

// findMatches scores sellers by how many requirement needles they hit.
func (s *Service) findMatches(ctx context.Context, buyerCompanyID string, req Requirements) ([]MatchResult, error) {
	type agg struct {
		name string
		hits map[string]struct{}
	}
	byCompany := map[string]*agg{}

	for _, needle := range req.needles() {
		rows, err := s.q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{
			ExcludeCompany: buyerCompanyID,
			Needle:         needle,
			Lim:            maxMatches,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			a, ok := byCompany[row.CompanyID]
			if !ok {
				a = &agg{name: row.CompanyName, hits: map[string]struct{}{}}
				byCompany[row.CompanyID] = a
			}
			a.hits[needle] = struct{}{}
		}
	}

	out := make([]MatchResult, 0, len(byCompany))
	for companyID, a := range byCompany {
		needles := make([]string, 0, len(a.hits))
		for n := range a.hits {
			needles = append(needles, n)
		}
		sort.Strings(needles)
		out = append(out, MatchResult{
			SellerCompanyID: companyID,
			SellerName:      a.name,
			Score:           float64(len(needles)),
			Reason:          "matched on: " + strings.Join(needles, ", "),
		})
	}

	// Highest score first; stable tiebreak by company id for determinism.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].SellerCompanyID < out[j].SellerCompanyID
	})
	if len(out) > maxMatches {
		out = out[:maxMatches]
	}
	return out, nil
}

// broadcast writes the event to agent_events (durable) BEFORE publishing it to
// Redis (best-effort) — the write-always pattern the Gateway relies on (A3).
func (s *Service) broadcast(ctx context.Context, rfq sqlc.Rfq, req Requirements, m MatchResult) error {
	payload := matchedEvent{
		Type:           "rfq.matched",
		RFQID:          rfq.ID,
		Title:          rfq.Title,
		BuyerCompanyID: rfq.BuyerCompanyID,
		Requirements:   req,
		Score:          m.Score,
		Reason:         m.Reason,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := s.q.CreateAgentEvent(ctx, sqlc.CreateAgentEventParams{
		CompanyID: m.SellerCompanyID,
		Type:      "rfq.matched",
		Payload:   body,
	}); err != nil {
		return err
	}

	if s.redis != nil {
		channel := fmt.Sprintf("agent:events:%s", m.SellerCompanyID)
		if err := s.redis.Publish(ctx, channel, body).Err(); err != nil {
			// Redis is the live fast-path, not the source of truth — the event
			// is already durably persisted, so a publish failure is non-fatal.
			s.logger.Warn().Err(err).Str("channel", channel).Msg("rfq.matched redis publish failed")
		}
	}
	return nil
}

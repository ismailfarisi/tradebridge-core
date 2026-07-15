package rfq

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// maxMatches caps how many sellers a single RFQ fans out to.
const maxMatches = 25

// Service orchestrates RFQ creation: extract → match → persist → broadcast.
// The extractor is resolved per RFQ from the buyer company's settings, so each
// tenant can configure its own provider/model/key.
type Service struct {
	q        *sqlc.Queries
	pub      *eventbus.Publisher
	resolver ExtractorResolver
	logger   zerolog.Logger
	ttlDays  int
}

// NewService builds the RFQ service. ttlDays is how long a matched RFQ stays
// open before the sweeper (Task C4) expires it; non-positive values fall back
// to a 14-day default.
func NewService(q *sqlc.Queries, pub *eventbus.Publisher, resolver ExtractorResolver, logger zerolog.Logger, ttlDays int) *Service {
	if ttlDays <= 0 {
		ttlDays = 14
	}
	return &Service{q: q, pub: pub, resolver: resolver, logger: logger, ttlDays: ttlDays}
}

// extractorFor resolves the buyer company's extraction config and builds an
// extractor; on resolver error it degrades to the heuristic extractor.
func (s *Service) extractorFor(ctx context.Context, companyID string) Extractor {
	cfg, err := s.resolver.ExtractionConfig(ctx, companyID)
	if err != nil {
		s.logger.Warn().Err(err).Str("company_id", companyID).Msg("extraction config resolve failed; using heuristic")
		return HeuristicExtractor{}
	}
	return BuildExtractor(cfg, s.logger)
}

// intakeFor resolves the buyer's provider config and builds an intake agent,
// degrading to the heuristic on resolver error (same policy as extractorFor).
func (s *Service) intakeFor(ctx context.Context, companyID string) IntakeAgent {
	cfg, err := s.resolver.ExtractionConfig(ctx, companyID)
	if err != nil {
		s.logger.Warn().Err(err).Str("company_id", companyID).Msg("intake config resolve failed; using heuristic")
		return HeuristicIntake{}
	}
	return BuildIntake(cfg, s.logger)
}

// ChatResult is one RFQ-chat step: either a clarifying question (Done=false) or
// the created RFQ (Done=true, RFQ populated).
type ChatResult struct {
	Done       bool
	Assistant  string
	RFQ        *sqlc.Rfq
	MatchCount int
}

// Chat runs one intake step. On "create" it reuses CreateAndMatch and persists
// the full transcript (incl. the confirmation) to rfqs.messages.
func (s *Service) Chat(ctx context.Context, buyerCompanyID, userID string, msgs []ChatMessage) (ChatResult, error) {
	dec, err := s.intakeFor(ctx, buyerCompanyID).Decide(ctx, msgs)
	if err != nil {
		return ChatResult{}, err
	}
	return s.finishChat(ctx, buyerCompanyID, userID, msgs, dec)
}

// ChatStream is Chat with live assistant-text deltas. Streaming-capable intake
// agents stream the model's "message" field; others (heuristic) emit their
// question as a single delta so clients get one uniform shape.
func (s *Service) ChatStream(ctx context.Context, buyerCompanyID, userID string, msgs []ChatMessage, onDelta func(string)) (ChatResult, error) {
	agent := s.intakeFor(ctx, buyerCompanyID)
	var dec IntakeDecision
	var err error
	if streamer, ok := agent.(IntakeStreamer); ok {
		dec, err = streamer.DecideStream(ctx, msgs, onDelta)
	} else {
		dec, err = agent.Decide(ctx, msgs)
		if err == nil && dec.Action != "create" && dec.Message != "" {
			onDelta(dec.Message)
		}
	}
	if err != nil {
		return ChatResult{}, err
	}
	return s.finishChat(ctx, buyerCompanyID, userID, msgs, dec)
}

// finishChat turns an intake decision into a ChatResult, creating the RFQ on
// "create" (shared tail of Chat and ChatStream).
func (s *Service) finishChat(ctx context.Context, buyerCompanyID, userID string, msgs []ChatMessage, dec IntakeDecision) (ChatResult, error) {
	if dec.Action != "create" {
		return ChatResult{Done: false, Assistant: dec.Message}, nil
	}

	opened, matches, err := s.CreateAndMatch(ctx, buyerCompanyID, userID, dec.Title, dec.Text)
	if err != nil {
		return ChatResult{}, err
	}

	confirm := chatConfirmation(opened.Title, len(matches))
	transcript := append(append([]ChatMessage{}, msgs...), ChatMessage{Role: "assistant", Content: confirm})
	raw, err := json.Marshal(transcript)
	if err != nil {
		return ChatResult{}, err
	}
	if err := s.q.SetRFQMessages(ctx, sqlc.SetRFQMessagesParams{Messages: raw, ID: opened.ID}); err != nil {
		return ChatResult{}, err
	}
	opened.Messages = raw
	return ChatResult{Done: true, Assistant: confirm, RFQ: &opened, MatchCount: len(matches)}, nil
}

// chatConfirmation is the assistant's closing line after posting an RFQ.
func chatConfirmation(title string, n int) string {
	plural := "s"
	if n == 1 {
		plural = ""
	}
	return fmt.Sprintf("Done — I've posted your RFQ %q and matched %d seller%s. Quotes will appear here as sellers respond.", title, n, plural)
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

	req, err := s.extractorFor(ctx, buyerCompanyID).Extract(ctx, title, text, nil)
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

	// Default expiry is TTL days out from creation; a concrete future deadline
	// parsed from the buyer's free text overrides it (best-effort).
	expiresAt := rfq.CreatedAt.Add(time.Duration(s.ttlDays) * 24 * time.Hour)
	if d, ok := parseDeadline(req.Deadline); ok && d.After(time.Now()) {
		expiresAt = d
	}

	opened, err := s.q.MarkRFQMatched(ctx, sqlc.MarkRFQMatchedParams{
		Requirements: reqJSON,
		Category:     req.Category,
		MatchCount:   int32(len(matches)),
		ID:           rfq.ID,
		ExpiresAt:    pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return sqlc.Rfq{}, nil, err
	}
	return opened, matches, nil
}

// parseDeadline best-effort-parses the free-text requirements deadline into a
// timestamp. Only a concrete future date overrides the TTL-based expiry.
func parseDeadline(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if d, err := time.Parse(layout, s); err == nil {
			return d, true
		}
	}
	return time.Time{}, false
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
	return s.pub.Publish(ctx, m.SellerCompanyID, "rfq.matched", body)
}

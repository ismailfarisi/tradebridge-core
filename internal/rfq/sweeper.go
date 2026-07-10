package rfq

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Sweeper periodically expires overdue open/quoted RFQs and notifies their
// matched sellers' agents with a durable rfq.expired event.
type Sweeper struct {
	q        *sqlc.Queries
	pool     *pgxpool.Pool
	pub      *eventbus.Publisher
	logger   zerolog.Logger
	interval time.Duration
}

type expiredEvent struct {
	Type            string `json:"type"`
	RFQID           string `json:"rfq_id"`
	SellerCompanyID string `json:"seller_company_id"`
	ExpiresAt       string `json:"expires_at"`
}

// NewSweeper builds a Sweeper. A non-positive interval falls back to 5 minutes.
func NewSweeper(q *sqlc.Queries, pool *pgxpool.Pool, pub *eventbus.Publisher, logger zerolog.Logger, interval time.Duration) *Sweeper {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &Sweeper{q: q, pool: pool, pub: pub, logger: logger, interval: interval}
}

// Run ticks until ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.SweepOnce(ctx); err != nil {
				s.logger.Warn().Err(err).Msg("rfq sweep failed")
			} else if n > 0 {
				s.logger.Info().Int("expired", n).Msg("rfq sweep expired overdue rfqs")
			}
		}
	}
}

// SweepOnce expires all currently-overdue RFQs and returns how many it expired.
// A per-RFQ failure is logged and skipped so one bad row can't stall the rest.
func (s *Sweeper) SweepOnce(ctx context.Context) (int, error) {
	ids, err := s.q.ListExpirableRFQIDs(ctx, 100)
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, id := range ids {
		if err := s.expireOne(ctx, id); err != nil {
			s.logger.Warn().Err(err).Str("rfq_id", id).Msg("expire rfq failed")
			continue
		}
		expired++
	}
	return expired, nil
}

func (s *Sweeper) expireOne(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	r, err := qtx.GetRFQ(ctx, id)
	if err != nil {
		return err
	}
	// Idempotent: another tick (or an accept) may have moved it already.
	if !CanTransition(r.Status, StatusExpired) {
		return nil
	}
	if _, err := qtx.SetRFQStatus(ctx, sqlc.SetRFQStatusParams{ID: id, Status: StatusExpired}); err != nil {
		return err
	}
	matches, err := qtx.ListMatchesByRFQ(ctx, id)
	if err != nil {
		return err
	}
	when := ""
	if r.ExpiresAt.Valid {
		when = r.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	evs := make([]sqlc.AgentEvent, 0, len(matches))
	for _, m := range matches {
		body, err := json.Marshal(expiredEvent{
			Type: "rfq.expired", RFQID: id, SellerCompanyID: m.SellerCompanyID, ExpiresAt: when,
		})
		if err != nil {
			return err
		}
		ev, err := qtx.CreateAgentEvent(ctx, sqlc.CreateAgentEventParams{
			CompanyID: m.SellerCompanyID, Type: "rfq.expired", Payload: body,
		})
		if err != nil {
			return err
		}
		evs = append(evs, ev)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, ev := range evs {
		s.pub.PublishExisting(ctx, ev)
	}
	return nil
}

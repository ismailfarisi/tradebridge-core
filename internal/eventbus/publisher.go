// Package eventbus centralizes the write-always agent-event pattern: persist to
// agent_events (durable, the Gateway's replay source) and then best-effort
// publish the same envelope to Redis for live delivery. Both the RFQ engine
// (rfq.matched) and the order flow (order.created) publish through it.
package eventbus

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/protocol"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Publisher writes and broadcasts agent events.
type Publisher struct {
	q      *sqlc.Queries
	redis  *redis.Client // may be nil; durable write still happens, live publish is skipped
	logger zerolog.Logger
}

// New builds a Publisher. redis may be nil.
func New(q *sqlc.Queries, rdb *redis.Client, logger zerolog.Logger) *Publisher {
	return &Publisher{q: q, redis: rdb, logger: logger}
}

// Publish durably records an event, then broadcasts it live.
func (p *Publisher) Publish(ctx context.Context, companyID, eventType string, payload []byte) error {
	ev, err := p.q.CreateAgentEvent(ctx, sqlc.CreateAgentEventParams{
		CompanyID: companyID,
		Type:      eventType,
		Payload:   payload,
	})
	if err != nil {
		return err
	}
	p.PublishExisting(ctx, ev)
	return nil
}

// PublishExisting broadcasts an already-persisted event. Use it when the durable
// row was written inside a transaction (e.g. order acceptance) and only the live
// Redis publish remains — after the commit.
func (p *Publisher) PublishExisting(ctx context.Context, ev sqlc.AgentEvent) {
	if p.redis == nil {
		return
	}
	wire, err := json.Marshal(protocol.Event{Seq: ev.Seq, ID: ev.ID, Type: ev.Type, Payload: ev.Payload})
	if err != nil {
		p.logger.Warn().Err(err).Msg("event envelope marshal failed")
		return
	}
	channel := protocol.Channel(ev.CompanyID)
	if err := p.redis.Publish(ctx, channel, wire).Err(); err != nil {
		// Redis is the live fast-path, not the source of truth — the event is
		// already durably persisted, so a publish failure is non-fatal.
		p.logger.Warn().Err(err).Str("channel", channel).Msg("event redis publish failed")
	}
}

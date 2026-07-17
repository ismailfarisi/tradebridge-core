// Package thread carries live buyer↔seller quote-thread updates: a
// nil-safe Redis publisher and an authed SSE stream that live-tails a
// quote's channel. No durable replay — the REST thread load is the source
// of truth; the stream is a live-tail convenience on top of it.
package thread

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Envelope is the compact live-update pushed to thread:<quoteID>. Kind is
// "message" or "version"; only the matching field is populated.
type Envelope struct {
	Kind    string          `json:"kind"` // "message" | "version"
	QuoteID string          `json:"quote_id"`
	Message json.RawMessage `json:"message,omitempty"`
	Version json.RawMessage `json:"version,omitempty"`
}

// channel is the Redis pub/sub channel a quote's thread lives on.
func channel(quoteID string) string { return "thread:" + quoteID }

// Publisher publishes thread envelopes to Redis. It is nil-safe both as a
// nil *Publisher (a service holding one may not have wired it in tests) and
// with a nil redis client (dev/test without Redis) — either way, publishing
// is a no-op. The DB row is always the source of truth; publish failures are
// logged, never returned.
type Publisher struct {
	redis  *redis.Client // may be nil → publishing is a no-op
	logger zerolog.Logger
}

// NewPublisher builds a thread event publisher. rdb may be nil.
func NewPublisher(rdb *redis.Client, logger zerolog.Logger) *Publisher {
	return &Publisher{redis: rdb, logger: logger}
}

// PublishMessage publishes a new quote_messages row to the quote's thread
// channel. Best-effort: never returns an error.
func (p *Publisher) PublishMessage(ctx context.Context, quoteID string, messageJSON []byte) {
	p.publish(ctx, quoteID, Envelope{Kind: "message", QuoteID: quoteID, Message: messageJSON})
}

// PublishVersion publishes a new quote_versions row to the quote's thread
// channel. Best-effort: never returns an error.
func (p *Publisher) PublishVersion(ctx context.Context, quoteID string, versionJSON []byte) {
	p.publish(ctx, quoteID, Envelope{Kind: "version", QuoteID: quoteID, Version: versionJSON})
}

func (p *Publisher) publish(ctx context.Context, quoteID string, env Envelope) {
	if p == nil || p.redis == nil {
		return
	}
	b, err := json.Marshal(env)
	if err != nil {
		p.logger.Warn().Err(err).Str("quote_id", quoteID).Msg("thread envelope marshal failed")
		return
	}
	if err := p.redis.Publish(ctx, channel(quoteID), b).Err(); err != nil {
		p.logger.Warn().Err(err).Str("quote_id", quoteID).Msg("thread publish failed")
	}
}

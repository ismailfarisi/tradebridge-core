package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"github.com/valyala/fasthttp"

	"github.com/tradebridge/core/internal/protocol"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// replayLimit bounds how many missed events a single reconnect drains at once.
const replayLimit = 500

// keepaliveInterval matches the Phase 0 stub — prevents proxy/idle timeouts.
const keepaliveInterval = 30 * time.Second

// stream is the authenticated SSE endpoint. It subscribes to the agent's Redis
// channel, replays events missed since Last-Event-ID (or all unacked on a fresh
// connect), then live-tails — deduping replayed events by seq.
func (h *Handler) stream(c *fiber.Ctx) error {
	agent, err := h.authenticate(c)
	if err != nil {
		return err
	}
	if err := h.q.TouchAgentCredentialLastSeen(c.Context(), agent.CredentialID); err != nil {
		return err
	}

	lastEventID := parseSeq(c.Get("Last-Event-ID"))

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no") // disable proxy buffering — critical for SSE

	companyID := agent.CompanyID

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Subscribe BEFORE replaying so no event slips through the gap between
		// the replay query and the live subscription; duplicates are deduped by seq.
		var live <-chan *redis.Message
		if h.redis != nil {
			sub := h.redis.Subscribe(ctx, protocol.Channel(companyID))
			defer func() { _ = sub.Close() }()
			live = sub.Channel()
		}

		maxSeq, err := h.replay(ctx, w, companyID, lastEventID)
		if err != nil {
			h.logger.Error().Err(err).Str("company_id", companyID).Msg("gateway replay failed")
			return
		}

		if !writeComment(w, "connected") || w.Flush() != nil {
			return
		}

		ticker := time.NewTicker(keepaliveInterval)
		defer ticker.Stop()

		for {
			select {
			case msg, ok := <-live:
				if !ok {
					return
				}
				var ev protocol.Event
				if json.Unmarshal([]byte(msg.Payload), &ev) != nil {
					continue
				}
				if ev.Seq <= maxSeq {
					continue // already delivered during replay
				}
				if !writeEvent(w, ev) || w.Flush() != nil {
					return
				}
				maxSeq = ev.Seq
			case <-ticker.C:
				if !writeComment(w, "keepalive") || w.Flush() != nil {
					return
				}
			}
		}
	}))
	return nil
}

// replay writes every event the agent has not yet seen and returns the highest
// seq written (the dedup watermark for the live tail).
func (h *Handler) replay(ctx context.Context, w *bufio.Writer, companyID string, lastEventID int64) (int64, error) {
	var rows []sqlc.AgentEvent
	var err error
	if lastEventID > 0 {
		rows, err = h.q.ReplayAgentEventsSince(ctx, sqlc.ReplayAgentEventsSinceParams{
			CompanyID: companyID, AfterSeq: lastEventID, Lim: replayLimit,
		})
	} else {
		rows, err = h.q.ReplayUnackedAgentEvents(ctx, sqlc.ReplayUnackedAgentEventsParams{
			CompanyID: companyID, Lim: replayLimit,
		})
	}
	if err != nil {
		return 0, err
	}

	maxSeq := lastEventID
	for _, r := range rows {
		ev := protocol.Event{Seq: r.Seq, ID: r.ID, Type: r.Type, Payload: r.Payload}
		if !writeEvent(w, ev) {
			break // client gone
		}
		if r.Seq > maxSeq {
			maxSeq = r.Seq
		}
	}
	_ = w.Flush()
	return maxSeq, nil
}

// writeEvent emits one SSE frame. The `id:` (seq) is what the agent echoes back
// as Last-Event-ID; `data:` is the compact payload JSON (no embedded newlines).
func writeEvent(w *bufio.Writer, ev protocol.Event) bool {
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, ev.Payload)
	return err == nil
}

func writeComment(w *bufio.Writer, s string) bool {
	_, err := fmt.Fprintf(w, ": %s\n\n", s)
	return err == nil
}

func parseSeq(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

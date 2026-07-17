package thread

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// keepaliveInterval prevents proxy/idle timeouts on the SSE connection.
const keepaliveInterval = 30 * time.Second

// Handler serves the buyer/seller quote-thread SSE live-tail.
type Handler struct {
	q      *sqlc.Queries
	redis  *redis.Client // may be nil; the stream then just keepalives
	logger zerolog.Logger
}

// NewHandler builds the thread stream handler.
func NewHandler(q *sqlc.Queries, rdb *redis.Client, logger zerolog.Logger) *Handler {
	return &Handler{q: q, redis: rdb, logger: logger}
}

// Register mounts the thread SSE route under the given guard chain (the
// existing tenant guard — Authorization header — the frontend consumes this
// via fetch+ReadableStream, not EventSource, so no query-token is needed).
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	app.Add("GET", "/v1/quotes/:id/thread/stream", append(guard, h.stream)...)
}

// authorizeThreadParty confirms companyID is a party to quoteID — its seller,
// or the buyer on its RFQ — mirroring quotes.mustParty. It returns a 404
// fiber.Error for both a missing quote and a non-party company, so existence
// never leaks across tenants. Extracted from stream() so the 404/party split
// is unit-testable without driving an actual SSE connection.
func authorizeThreadParty(ctx context.Context, q *sqlc.Queries, quoteID, companyID string) error {
	quote, err := q.GetQuote(ctx, quoteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "quote not found")
	} else if err != nil {
		return err
	}
	if quote.SellerCompanyID == companyID {
		return nil
	}
	r, err := q.GetRFQ(ctx, quote.RfqID)
	if err != nil {
		return err
	}
	if r.BuyerCompanyID != companyID {
		return fiber.NewError(fiber.StatusNotFound, "quote not found")
	}
	return nil
}

// stream live-tails thread:<quoteID> for a party to the quote. There is no
// replay: the page loads the thread over REST first (GET
// /v1/quotes/:id/thread) and re-fetches on reconnect, so this only carries
// events from the moment of connection onward.
func (h *Handler) stream(c *fiber.Ctx) error {
	quoteID := c.Params("id")
	if err := authorizeThreadParty(c.Context(), h.q, quoteID, middleware.CompanyID(c)); err != nil {
		return err
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no") // disable proxy buffering — critical for SSE

	c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// A nil live channel (Redis unavailable) blocks forever in the select
		// below, so the loop degrades to keepalive-only — never selected, never
		// panics.
		var live <-chan *redis.Message
		if h.redis != nil {
			sub := h.redis.Subscribe(ctx, channel(quoteID))
			defer func() { _ = sub.Close() }()
			live = sub.Channel()
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
				if !writeEvent(w, msg.Payload) || w.Flush() != nil {
					return
				}
			case <-ticker.C:
				if !writeComment(w, "keepalive") || w.Flush() != nil {
					return
				}
			}
		}
	}))
	return nil
}

// writeEvent emits one SSE frame carrying a thread envelope's JSON payload.
func writeEvent(w *bufio.Writer, payload string) bool {
	_, err := fmt.Fprintf(w, "event: thread\ndata: %s\n\n", payload)
	return err == nil
}

func writeComment(w *bufio.Writer, s string) bool {
	_, err := fmt.Fprintf(w, ": %s\n\n", s)
	return err == nil
}

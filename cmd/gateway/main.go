// Command gateway is the Agent Gateway. At scale it holds 100k SSE connections,
// subscribes to Redis per agent, and replays missed events. Phase 0 ships the
// bare SSE endpoint an agent connects to — an empty stream with keepalives.
// Redis pub/sub, durable replay and acks arrive in Phase 1 (plan §5).
package main

import (
	"bufio"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"

	"github.com/tradebridge/core/internal/di"
)

func main() {
	app, cleanup, err := di.InitializeApp()
	if err != nil {
		panic(err)
	}
	defer cleanup()

	log := app.Logger
	f := fiber.New(fiber.Config{AppName: "tradebridge-gateway"})

	f.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	f.Get("/agent/v1/stream", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("X-Accel-Buffering", "no") // disable proxy buffering — critical for SSE

		c.Context().SetBodyStreamWriter(fasthttp.StreamWriter(func(w *bufio.Writer) {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			// Emit an immediate comment so the client sees an established stream.
			if _, err := w.WriteString(": connected\n\n"); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}

			for range ticker.C {
				// Keepalive comment — prevents ALB/idle timeouts. A broken pipe
				// surfaces as a flush error, ending the stream.
				if _, err := w.WriteString(": keepalive\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}))
		return nil
	})

	addr := ":" + app.Config.GatewayPort
	log.Info().Str("addr", addr).Msg("agent gateway listening")
	if err := f.Listen(addr); err != nil {
		log.Fatal().Err(err).Msg("gateway server stopped")
	}
}

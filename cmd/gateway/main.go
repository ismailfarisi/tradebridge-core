// Command gateway is the Agent Gateway. It holds the agents' SSE connections,
// subscribes to Redis per company for live events, replays events missed since
// Last-Event-ID, and accepts validated, idempotent agent commands. The protocol
// lives in internal/gateway; this is the thin entrypoint. (Plan §5.)
package main

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/di"
	"github.com/tradebridge/core/internal/gateway"
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

	gateway.NewHandler(app.Queries, app.Redis, app.Logger).Register(f)

	addr := ":" + app.Config.GatewayPort
	log.Info().Str("addr", addr).Msg("agent gateway listening")
	if err := f.Listen(addr); err != nil {
		log.Fatal().Err(err).Msg("gateway server stopped")
	}
}

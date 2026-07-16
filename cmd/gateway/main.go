// Command gateway is the Agent Gateway. It holds the agents' SSE connections,
// subscribes to Redis per company for live events, replays events missed since
// Last-Event-ID, and accepts validated, idempotent agent commands. The protocol
// lives in internal/gateway; this is the thin entrypoint. (Plan §5.)
package main

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/internal/di"
	"github.com/tradebridge/core/internal/gateway"
	"github.com/tradebridge/core/internal/notify"
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

	attDir, err := attachments.ResolveDir(app.Config.AttachmentsDir)
	if err != nil {
		panic(err)
	}
	store, err := attachments.NewStore(attDir)
	if err != nil {
		panic(err)
	}

	sender := notify.NewSender(app.Config.ResendAPIKey, app.Config.EmailFrom, app.Logger)
	notifier := notify.New(app.Queries, sender, app.Config.AppBaseURL, app.Logger)

	gateway.NewHandler(app.Queries, app.Redis, store, app.Logger, gateway.RateLimitConfig{
		PerMin:            app.Config.AgentRLPerMin,
		AuthfailMax:       app.Config.AgentAuthfailMax,
		AuthfailWindowSec: app.Config.AgentAuthfailWindowSec,
	}, notifier).Register(f)

	addr := ":" + app.Config.GatewayPort
	log.Info().Str("addr", addr).Msg("agent gateway listening")
	if err := f.Listen(addr); err != nil {
		log.Fatal().Err(err).Msg("gateway server stopped")
	}
}

// Command api is the TradeBridge Core API. It owns companies, users, RFQs,
// quotes and orders. Phase 0 exposes /health and a tenant-guarded /v1/me.
package main

import (
	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/di"
	"github.com/tradebridge/core/pkg/middleware"
)

func main() {
	app, cleanup, err := di.InitializeApp()
	if err != nil {
		panic(err)
	}
	defer cleanup()

	log := app.Logger
	f := fiber.New(fiber.Config{AppName: "tradebridge-core-api"})
	f.Use(middleware.RequestLogger(log))

	// Public liveness probe.
	f.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Everything under /v1 requires a valid JWT + a tenant.
	v1 := f.Group("/v1", middleware.JWT(app.Config.JWTSecret), middleware.TenantGuard())
	v1.Get("/me", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"company_id": middleware.CompanyID(c),
			"user_id":    middleware.UserID(c),
		})
	})

	addr := ":" + app.Config.Port
	log.Info().Str("addr", addr).Msg("core api listening")
	if err := f.Listen(addr); err != nil {
		log.Fatal().Err(err).Msg("api server stopped")
	}
}

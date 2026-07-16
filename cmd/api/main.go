// Command api is the TradeBridge Core API. It owns companies, users, RFQs,
// quotes and orders. Sprint A1 adds auth, the seller directory, and agent
// credential issuance on top of the Phase 0 foundation.
package main

import (
	"context"
	"time"

	"github.com/tradebridge/core/internal/di"
	"github.com/tradebridge/core/internal/eventbus"
	"github.com/tradebridge/core/internal/httpapi"
	"github.com/tradebridge/core/internal/rfq"
)

func main() {
	app, cleanup, err := di.InitializeApp()
	if err != nil {
		panic(err)
	}
	defer cleanup()

	f := httpapi.New(app.Queries, httpapi.Config{
		JWTSecret:       app.Config.JWTSecret,
		TokenTTL:        24 * time.Hour,
		Logger:          app.Logger,
		Redis:           app.Redis,
		Pool:            app.Pool,
		AnthropicAPIKey: app.Config.AnthropicAPIKey,
		AnthropicModel:  app.Config.AnthropicModel,
		SettingsEncKey:  app.Config.SettingsEncKey,
		CORSOrigins:     app.Config.CORSOrigins,

		ResendAPIKey: app.Config.ResendAPIKey,
		EmailFrom:    app.Config.EmailFrom,
		AppBaseURL:   app.Config.AppBaseURL,

		AdminJWTSecret:        app.Config.AdminJWTSecret,
		PlatformAdminEmail:    app.Config.PlatformAdminEmail,
		PlatformAdminPassword: app.Config.PlatformAdminPassword,

		RFQTTLDays: app.Config.RFQTTLDays,

		AttachmentsDir: app.Config.AttachmentsDir,

		AgentCredTTLDays:         app.Config.AgentCredTTLDays,
		AgentCredRotateGraceDays: app.Config.AgentCredRotateGraceDays,

		StripeSecretKey:          app.Config.StripeSecretKey,
		StripeWebhookSecret:      app.Config.StripeWebhookSecret,
		StripePriceProMonth:      app.Config.StripePriceProMonth,
		StripePriceProYear:       app.Config.StripePriceProYear,
		StripePriceBusinessMonth: app.Config.StripePriceBusinessMonth,
		StripePriceBusinessYear:  app.Config.StripePriceBusinessYear,
	})

	sweepCtx, cancelSweep := context.WithCancel(context.Background())
	defer cancelSweep()
	sweeper := rfq.NewSweeper(
		app.Queries, app.Pool,
		eventbus.New(app.Queries, app.Redis, app.Logger),
		app.Logger,
		time.Duration(app.Config.RFQSweepSeconds)*time.Second,
	)
	go sweeper.Run(sweepCtx)

	addr := ":" + app.Config.Port
	app.Logger.Info().Str("addr", addr).Msg("core api listening")
	if err := f.Listen(addr); err != nil {
		app.Logger.Fatal().Err(err).Msg("api server stopped")
	}
}

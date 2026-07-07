// Command api is the TradeBridge Core API. It owns companies, users, RFQs,
// quotes and orders. Sprint A1 adds auth, the seller directory, and agent
// credential issuance on top of the Phase 0 foundation.
package main

import (
	"time"

	"github.com/tradebridge/core/internal/di"
	"github.com/tradebridge/core/internal/httpapi"
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
	})

	addr := ":" + app.Config.Port
	app.Logger.Info().Str("addr", addr).Msg("core api listening")
	if err := f.Listen(addr); err != nil {
		app.Logger.Fatal().Err(err).Msg("api server stopped")
	}
}

//go:build wireinject
// +build wireinject

package di

import (
	"github.com/google/wire"

	"github.com/tradebridge/core/internal/config"
)

// InitializeApp builds the App dependency graph. `wire` generates the concrete
// implementation into wire_gen.go — run `make generate` after editing providers.
func InitializeApp() (*App, func(), error) {
	wire.Build(
		config.Load,
		NewLogger,
		NewPool,
		NewRedis,
		NewQueries,
		wire.Struct(new(App), "*"),
	)
	return nil, nil, nil
}

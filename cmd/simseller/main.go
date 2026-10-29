// Command simseller runs one or more simulated seller agents against the
// Agent Gateway for local demos. Credentials come from -creds or
// SIM_CREDENTIALS (comma-separated "id.secret" values, as issued by
// POST /v1/agent-credentials). Never run against production.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/simseller"
)

func main() {
	gw := flag.String("gateway", envOr("SIM_GATEWAY_URL", "http://localhost:8081"), "Agent Gateway base URL")
	creds := flag.String("creds", os.Getenv("SIM_CREDENTIALS"), "comma-separated agent credentials")
	latency := flag.Duration("latency", 1500*time.Millisecond, "think time before each response")
	flag.Parse()

	log := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).With().Timestamp().Logger()
	list := splitNonEmpty(*creds)
	if len(list) == 0 {
		log.Fatal().Msg("no credentials: pass -creds or SIM_CREDENTIALS (run `make demo-seed` first)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	for i, c := range list {
		a := &simseller.Agent{GatewayURL: *gw, Credential: c, Persona: simseller.Personas[i%len(simseller.Personas)], Latency: *latency, Logger: log}
		wg.Add(1)
		go func() { defer wg.Done(); a.Run(ctx) }()
	}
	wg.Wait()
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

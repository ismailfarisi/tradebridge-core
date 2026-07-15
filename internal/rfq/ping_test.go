package rfq

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestPingProvider_NoKeyReturnsError(t *testing.T) {
	err := PingProvider(context.Background(), ProviderConfig{Provider: "openai_compatible"}, zerolog.Nop())
	if err == nil || !strings.Contains(err.Error(), "no API key") {
		t.Fatalf("expected no-API-key error, got %v", err)
	}
}

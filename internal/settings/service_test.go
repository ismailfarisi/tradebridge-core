package settings

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/cryptobox"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

func serviceOrSkip(t *testing.T) (*Service, *sqlc.Queries) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)

	box, err := cryptobox.New("test-enc-secret")
	require.NoError(t, err)
	env := rfq.ProviderConfig{Provider: "anthropic", Model: "claude-haiku-4-5", APIKey: "env-key"}
	return NewService(q, box, env, nil), q
}

func TestExtractionConfigFallsBackToEnv(t *testing.T) {
	svc, q := serviceOrSkip(t)
	ctx := context.Background()

	// Clear any global row left by a prior test so the env fallback is exercised.
	_, err := q.UpsertPlatformSettings(ctx, sqlc.UpsertPlatformSettingsParams{})
	require.NoError(t, err)

	cfg, err := svc.ExtractionConfig(ctx, "ignored-company-id")
	require.NoError(t, err)
	require.Equal(t, "anthropic", cfg.Provider)
	require.Equal(t, "env-key", cfg.APIKey, "no stored settings → env default")
}

func TestUpdateAndResolve(t *testing.T) {
	svc, q := serviceOrSkip(t)
	ctx := context.Background()

	view, err := svc.Update(ctx, ExtractionInput{
		Provider: "openai_compatible", Model: "gpt-4o-mini",
		BaseURL: "https://api.example.com/v1", APIKey: "sk-live-abcd1234",
	})
	require.NoError(t, err)
	require.True(t, view.Extraction.HasKey)
	require.Equal(t, "1234", view.Extraction.KeyLast4)

	// Resolution decrypts the stored key and layers over env. companyID is
	// ignored — settings are global now.
	cfg, err := svc.ExtractionConfig(ctx, "ignored-company-id")
	require.NoError(t, err)
	require.Equal(t, "openai_compatible", cfg.Provider)
	require.Equal(t, "gpt-4o-mini", cfg.Model)
	require.Equal(t, "sk-live-abcd1234", cfg.APIKey)

	// Stored ciphertext is not the plaintext.
	row, err := q.GetPlatformSettings(ctx)
	require.NoError(t, err)
	require.NotContains(t, string(row.ExtractionApiKeyEnc), "sk-live-abcd1234")

	// An empty APIKey on a later update preserves the stored key.
	view, err = svc.Update(ctx, ExtractionInput{Provider: "openai_compatible", Model: "gpt-4o", BaseURL: "https://api.example.com/v1"})
	require.NoError(t, err)
	require.True(t, view.Extraction.HasKey, "empty api_key must not wipe the stored key")
	require.Equal(t, "gpt-4o", view.Extraction.Model)

	// ClearKey removes it.
	view, err = svc.Update(ctx, ExtractionInput{Provider: "openai_compatible", ClearKey: true})
	require.NoError(t, err)
	require.False(t, view.Extraction.HasKey)
}

func TestGetIncludesPromptsAndDefaults(t *testing.T) {
	svc, _ := serviceOrSkip(t)
	ctx := context.Background()

	view, err := svc.Update(ctx, ExtractionInput{
		Provider:               "anthropic",
		Model:                  "claude-haiku-4-5",
		ExtractionSystemPrompt: "custom extraction prompt",
		IntakeSystemPrompt:     "custom intake prompt",
	})
	require.NoError(t, err)
	require.Equal(t, "custom extraction prompt", view.ExtractionSystemPrompt)
	require.Equal(t, "custom intake prompt", view.IntakeSystemPrompt)
	require.Equal(t, rfq.DefaultExtractionPrompt, view.DefaultExtractionPrompt)
	require.Equal(t, rfq.DefaultIntakePrompt, view.DefaultIntakePrompt)

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "custom extraction prompt", got.ExtractionSystemPrompt)
	require.Equal(t, "custom intake prompt", got.IntakeSystemPrompt)
	require.Equal(t, rfq.DefaultExtractionPrompt, got.DefaultExtractionPrompt)
	require.Equal(t, rfq.DefaultIntakePrompt, got.DefaultIntakePrompt)

	// ExtractionConfig layers the stored prompts onto the resolved config too.
	cfg, err := svc.ExtractionConfig(ctx, "ignored-company-id")
	require.NoError(t, err)
	require.Equal(t, "custom extraction prompt", cfg.ExtractionPrompt)
	require.Equal(t, "custom intake prompt", cfg.IntakePrompt)
}

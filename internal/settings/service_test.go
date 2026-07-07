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

func serviceOrSkip(t *testing.T) (*Service, *sqlc.Queries, string) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)

	company, err := q.CreateCompany(context.Background(), sqlc.CreateCompanyParams{Name: "SetCo", Type: "buyer"})
	require.NoError(t, err)

	box, err := cryptobox.New("test-enc-secret")
	require.NoError(t, err)
	env := rfq.ProviderConfig{Provider: "anthropic", Model: "claude-haiku-4-5", APIKey: "env-key"}
	return NewService(q, box, env), q, company.ID
}

func TestExtractionConfigFallsBackToEnv(t *testing.T) {
	svc, _, companyID := serviceOrSkip(t)
	cfg, err := svc.ExtractionConfig(context.Background(), companyID)
	require.NoError(t, err)
	require.Equal(t, "anthropic", cfg.Provider)
	require.Equal(t, "env-key", cfg.APIKey, "no stored settings → env default")
}

func TestUpdateAndResolve(t *testing.T) {
	svc, q, companyID := serviceOrSkip(t)
	ctx := context.Background()

	view, err := svc.UpdateExtraction(ctx, companyID, ExtractionInput{
		Provider: "openai_compatible", Model: "gpt-4o-mini",
		BaseURL: "https://api.example.com/v1", APIKey: "sk-live-abcd1234",
	})
	require.NoError(t, err)
	require.True(t, view.HasKey)
	require.Equal(t, "1234", view.KeyLast4)

	// Resolution decrypts the stored key and layers over env.
	cfg, err := svc.ExtractionConfig(ctx, companyID)
	require.NoError(t, err)
	require.Equal(t, "openai_compatible", cfg.Provider)
	require.Equal(t, "gpt-4o-mini", cfg.Model)
	require.Equal(t, "sk-live-abcd1234", cfg.APIKey)

	// Stored ciphertext is not the plaintext.
	row, err := q.GetCompanySettings(ctx, companyID)
	require.NoError(t, err)
	require.NotContains(t, string(row.ExtractionApiKeyEnc), "sk-live-abcd1234")

	// An empty APIKey on a later update preserves the stored key.
	view, err = svc.UpdateExtraction(ctx, companyID, ExtractionInput{
		Provider: "openai_compatible", Model: "gpt-4o", BaseURL: "https://api.example.com/v1",
	})
	require.NoError(t, err)
	require.True(t, view.HasKey, "empty api_key must not wipe the stored key")
	require.Equal(t, "gpt-4o", view.Model)

	// ClearKey removes it.
	view, err = svc.UpdateExtraction(ctx, companyID, ExtractionInput{Provider: "openai_compatible", ClearKey: true})
	require.NoError(t, err)
	require.False(t, view.HasKey)
}

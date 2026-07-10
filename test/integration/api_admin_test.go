package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestPlatform_StoreRoundTrip(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	admin, err := q.CreatePlatformAdmin(ctx, sqlc.CreatePlatformAdminParams{
		Email: uniqueEmail("padmin-"), PasswordHash: "bcrypt$hash",
	})
	require.NoError(t, err)
	require.Len(t, admin.ID, 26)

	got, err := q.GetPlatformAdminByEmail(ctx, admin.Email)
	require.NoError(t, err)
	require.Equal(t, "bcrypt$hash", got.PasswordHash)

	row, err := q.UpsertPlatformSettings(ctx, sqlc.UpsertPlatformSettingsParams{
		ExtractionProvider:     "anthropic",
		ExtractionModel:        "claude-haiku-4-5",
		ExtractionSystemPrompt: "custom extract",
		IntakeSystemPrompt:     "custom intake",
	})
	require.NoError(t, err)
	require.Equal(t, "global", row.ID)

	// Second upsert updates the same singleton row.
	row2, err := q.UpsertPlatformSettings(ctx, sqlc.UpsertPlatformSettingsParams{
		ExtractionProvider: "openai_compatible", ExtractionModel: "gpt-4o-mini",
	})
	require.NoError(t, err)
	require.Equal(t, "global", row2.ID)

	settings, err := q.GetPlatformSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, "openai_compatible", settings.ExtractionProvider)
}

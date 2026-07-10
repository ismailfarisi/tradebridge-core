package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/admin"
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

// buildAdminAPI builds the router with a known admin seeded, returning the api
// plus the admin's login credentials.
func buildAdminAPI(t *testing.T) (fiberApp, string, string) {
	t.Helper()
	email := uniqueEmail("padmin-")
	const pass = "admin-super-secret"
	api := buildAPIWithAdmin(t, email, pass)
	return api, email, pass
}

func TestAdmin_LoginAndMe(t *testing.T) {
	api, email, pass := buildAdminAPI(t)

	// Bad password → 401.
	st, _ := doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": "wrong"})
	require.Equal(t, http.StatusUnauthorized, st)

	// Unknown email → 401.
	st, _ = doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{"email": "nobody@x.com", "password": pass})
	require.Equal(t, http.StatusUnauthorized, st)

	// Good creds → token.
	st, body := doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{"email": email, "password": pass})
	require.Equal(t, http.StatusOK, st)
	tok := body["token"].(string)
	require.NotEmpty(t, tok)

	// /me with the admin token → 200 with the email.
	st, me := doJSON(t, api, "GET", "/admin/v1/me", tok, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, email, me["email"])

	// /me with no token → 401.
	st, _ = doJSON(t, api, "GET", "/admin/v1/me", "", nil)
	require.Equal(t, http.StatusUnauthorized, st)
}

func TestAdmin_CompanyTokenRejected(t *testing.T) {
	api, _, _ := buildAdminAPI(t)
	// Register a normal company user and use its token against the admin realm.
	st, s := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "Acme", "company_type": "buyer",
		"email": uniqueEmail("ct-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	companyTok := s["token"].(string)
	st, _ = doJSON(t, api, "GET", "/admin/v1/me", companyTok, nil)
	require.Equal(t, http.StatusUnauthorized, st, "a company token must not reach the admin realm")
}

// TestAdmin_BootstrapIsIdempotent verifies a second Bootstrap call for the same
// email is a no-op: no error and no duplicate admin created.
func TestAdmin_BootstrapIsIdempotent(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()
	email := uniqueEmail("padmin-idem-")

	svc := admin.NewService(q)
	require.NoError(t, svc.Bootstrap(ctx, email, "first-password"))

	first, err := q.GetPlatformAdminByEmail(ctx, email)
	require.NoError(t, err)

	// Second bootstrap with a different password must not error and must not
	// touch the existing admin's credentials.
	require.NoError(t, svc.Bootstrap(ctx, email, "second-password"))

	second, err := q.GetPlatformAdminByEmail(ctx, email)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, first.PasswordHash, second.PasswordHash, "bootstrap must not reset an existing admin's password")
}

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSettings_ExtractionCRUDAndGuards verifies the settings surface: owner can
// read/update, secrets are masked on read and encrypted at rest, and members
// are forbidden.
func TestSettings_ExtractionCRUDAndGuards(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	email := uniqueEmail("setowner-")
	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "SettingsCo", "email": email, "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	ownerToken := reg["token"].(string)
	companyID := reg["company_id"].(string)

	// Defaults: nothing configured.
	st, view := doJSON(t, app, "GET", "/v1/settings/extraction", ownerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "", view["provider"])
	require.Equal(t, false, view["has_key"])

	// Configure any model via an OpenAI-compatible endpoint.
	st, updated := doJSON(t, app, "PUT", "/v1/settings/extraction", ownerToken, map[string]any{
		"provider": "openai_compatible", "model": "gpt-4o-mini",
		"base_url": "https://api.example.com/v1", "api_key": "sk-secret-value-9876",
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, true, updated["has_key"])
	require.Equal(t, "9876", updated["key_last4"])
	require.Equal(t, "gpt-4o-mini", updated["model"])

	// Read never returns the plaintext key.
	st, got := doJSON(t, app, "GET", "/v1/settings/extraction", ownerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.NotContains(t, toString(got), "sk-secret-value-9876")
	require.Equal(t, "9876", got["key_last4"])

	// Stored at rest as ciphertext, not plaintext.
	var enc []byte
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT extraction_api_key_enc FROM company_settings WHERE company_id=$1`, companyID).Scan(&enc))
	require.NotContains(t, string(enc), "sk-secret-value-9876")

	// Invalid provider rejected.
	st, _ = doJSON(t, app, "PUT", "/v1/settings/extraction", ownerToken, map[string]any{"provider": "bogus"})
	require.Equal(t, http.StatusBadRequest, st)

	// A member is forbidden from reading or writing settings.
	st, inv := doJSON(t, app, "POST", "/v1/users/invite", ownerToken, map[string]any{
		"email": uniqueEmail("setmember-"), "role": "member",
	})
	require.Equal(t, http.StatusCreated, st)
	st, acc := doJSON(t, app, "POST", "/v1/auth/accept-invite", "", map[string]any{
		"token": inv["token"], "password": "memberpass",
	})
	require.Equal(t, http.StatusCreated, st)
	memberToken := acc["token"].(string)

	st, _ = doJSON(t, app, "GET", "/v1/settings/extraction", memberToken, nil)
	require.Equal(t, http.StatusForbidden, st)
	st, _ = doJSON(t, app, "PUT", "/v1/settings/extraction", memberToken, map[string]any{"model": "x"})
	require.Equal(t, http.StatusForbidden, st)
}

func toString(m map[string]any) string {
	s := ""
	for k, v := range m {
		s += k
		if vs, ok := v.(string); ok {
			s += vs
		}
	}
	return s
}

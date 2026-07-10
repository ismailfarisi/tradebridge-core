package integration

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/httpapi"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// fiberApp is the subset of *fiber.App the JSON helpers use.
type fiberApp = *fiber.App

// buildAPI wires the real router against the test database.
func buildAPI(t *testing.T) *fiber.App {
	t.Helper()
	pool := poolOrSkip(t)
	return httpapi.New(sqlc.New(pool), httpapi.Config{
		JWTSecret: "test-secret",
		Logger:    zerolog.Nop(),
		Pool:      pool,
	})
}

// buildAPIWithAdmin builds the router and seeds a platform admin.
func buildAPIWithAdmin(t *testing.T, email, password string) *fiber.App {
	t.Helper()
	pool := poolOrSkip(t)
	return httpapi.New(sqlc.New(pool), httpapi.Config{
		JWTSecret:             "test-secret",
		AdminJWTSecret:        "admin-test-secret",
		PlatformAdminEmail:    email,
		PlatformAdminPassword: password,
		Logger:                zerolog.Nop(),
		Pool:                  pool,
	})
}

// doJSON performs a JSON request against the fiber app and decodes the response.
func doJSON(t *testing.T, app *fiber.App, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

func uniqueEmail(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:]) + "@test.local"
}

func TestA1_RegisterMeAndTenantIsolation(t *testing.T) {
	app := buildAPI(t)

	// Register company A (owner).
	st, regA := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AlphaCo", "company_type": "seller",
		"email": uniqueEmail("a-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	tokenA := regA["token"].(string)
	require.Equal(t, "owner", regA["role"])

	// /v1/me reflects the tenant.
	st, me := doJSON(t, app, "GET", "/v1/me", tokenA, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, regA["company_id"], me["company_id"])

	// A creates a catalog item.
	st, item := doJSON(t, app, "POST", "/v1/catalog", tokenA, map[string]any{
		"name": "Widget A", "category": "widgets", "summary": "secret",
	})
	require.Equal(t, http.StatusCreated, st)
	catID := item["id"].(string)

	// Register company B.
	st, regB := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "BetaCo", "company_type": "buyer",
		"email": uniqueEmail("b-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	tokenB := regB["token"].(string)

	// B must NOT see A's catalog item; A can.
	st, _ = doJSON(t, app, "GET", "/v1/catalog/"+catID, tokenB, nil)
	require.Equal(t, http.StatusNotFound, st, "cross-tenant read must 404")
	st, _ = doJSON(t, app, "GET", "/v1/catalog/"+catID, tokenA, nil)
	require.Equal(t, http.StatusOK, st)
}

func TestA1_LoginInviteAndRoleGuard(t *testing.T) {
	app := buildAPI(t)
	email := uniqueEmail("owner-")

	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "GammaCo", "email": email, "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	ownerToken := reg["token"].(string)

	// Good + bad login.
	st, _ = doJSON(t, app, "POST", "/v1/auth/login", "", map[string]any{"email": email, "password": "supersecret"})
	require.Equal(t, http.StatusOK, st)
	st, _ = doJSON(t, app, "POST", "/v1/auth/login", "", map[string]any{"email": email, "password": "nope"})
	require.Equal(t, http.StatusUnauthorized, st)

	// Invite a member, accept, then confirm the member is forbidden from issuing creds.
	st, inv := doJSON(t, app, "POST", "/v1/users/invite", ownerToken, map[string]any{
		"email": uniqueEmail("m-"), "role": "member",
	})
	require.Equal(t, http.StatusCreated, st)
	st, acc := doJSON(t, app, "POST", "/v1/auth/accept-invite", "", map[string]any{
		"token": inv["token"], "password": "memberpass",
	})
	require.Equal(t, http.StatusCreated, st)
	memberToken := acc["token"].(string)

	st, _ = doJSON(t, app, "POST", "/v1/agent-credentials", memberToken, map[string]any{"label": "x"})
	require.Equal(t, http.StatusForbidden, st, "member must not issue agent credentials")

	// Owner can.
	st, _ = doJSON(t, app, "POST", "/v1/agent-credentials", ownerToken, map[string]any{"label": "x"})
	require.Equal(t, http.StatusCreated, st)
}

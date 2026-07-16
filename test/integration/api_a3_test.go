package integration

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/internal/attachments"
	"github.com/tradebridge/core/internal/gateway"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// buildGateway wires the real Agent Gateway against the test database. Redis is
// nil, so live pub/sub is disabled but replay + commands + acks work fully.
// The attachment store is a real on-disk store rooted at a fresh temp dir so
// GET /agent/v1/attachments/:id can be exercised end-to-end.
func buildGateway(t *testing.T) *fiber.App {
	t.Helper()
	pool := poolOrSkip(t)
	store, err := attachments.NewStore(t.TempDir())
	require.NoError(t, err)
	f := fiber.New()
	gateway.NewHandler(sqlc.New(pool), nil, store, zerolog.Nop(), gateway.RateLimitConfig{PerMin: 120, AuthfailMax: 20, AuthfailWindowSec: 300}).Register(f)
	return f
}

// buildGatewayWithStore is like buildGateway but returns the store too, so
// callers can seed attachment bytes directly (bypassing the buyer-facing
// upload API, which would otherwise write to a different temp dir).
func buildGatewayWithStore(t *testing.T) (*fiber.App, *sqlc.Queries, *attachments.Store) {
	t.Helper()
	pool := poolOrSkip(t)
	store, err := attachments.NewStore(t.TempDir())
	require.NoError(t, err)
	q := sqlc.New(pool)
	f := fiber.New()
	gateway.NewHandler(q, nil, store, zerolog.Nop(), gateway.RateLimitConfig{PerMin: 120, AuthfailMax: 20, AuthfailWindowSec: 300}).Register(f)
	return f, q, store
}

// TestA3_CommandsAcksAndAuth exercises the gateway protocol: an agent
// authenticates with its issued credential, submits a quote (advancing the RFQ
// to `quoted`), replays are idempotent, cross-RFQ commands are rejected, and
// events can be acked. Every accepted command writes an audit log.
func TestA3_CommandsAcksAndAuth(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	// Seller registers, publishes a matching profile, and issues an agent credential.
	st, seller := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AgentCo", "company_type": "seller",
		"email": uniqueEmail("agent-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	sellerToken := seller["token"].(string)
	sellerCompanyID := seller["company_id"].(string)

	st, _ = doJSON(t, api, "PUT", "/v1/suppliers/me", sellerToken, map[string]any{
		"description": "cartons and pallets", "categories": []string{"packaging"}, "location": "Dubai",
	})
	require.Equal(t, http.StatusOK, st)

	st, cred := doJSON(t, api, "POST", "/v1/agent-credentials", sellerToken, map[string]any{"label": "gw"})
	require.Equal(t, http.StatusCreated, st)
	secret := cred["secret"].(string) // "<id>.<raw>", shown once

	// Buyer posts an RFQ that matches the seller.
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "GwBuyer", "company_type": "buyer",
		"email": uniqueEmail("gwbuyer-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	// --- Auth: no credential is rejected. ---
	st, _ = doJSON(t, gw, "POST", "/agent/v1/heartbeat", "", nil)
	require.Equal(t, http.StatusUnauthorized, st)
	st, _ = doJSON(t, gw, "POST", "/agent/v1/heartbeat", "bogus.secret", nil)
	require.Equal(t, http.StatusUnauthorized, st)
	st, _ = doJSON(t, gw, "POST", "/agent/v1/heartbeat", secret, nil)
	require.Equal(t, http.StatusOK, st)

	// --- quote.submit: valid, matched → 201; RFQ advances to quoted. ---
	st, cmd := doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": "quote-1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1200, "currency": "USD"},
	})
	require.Equal(t, http.StatusCreated, st)
	cmdID := cmd["id"].(string)

	st, gotRFQ := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID, buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "quoted", gotRFQ["rfq"].(map[string]any)["status"], "quote.submit advances open → quoted")

	// --- Idempotency: same key returns the original, no new row. ---
	st, dup := doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": "quote-1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 9999},
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, cmdID, dup["id"], "duplicate idempotency key returns the original command")
	require.Equal(t, true, dup["idempotent"])

	var cmdCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_commands WHERE agent_credential_id = split_part($1,'.',1) AND idempotency_key = 'quote-1'`,
		secret).Scan(&cmdCount))
	require.Equal(t, 1, cmdCount, "idempotent retry must not insert a second command")

	// --- Authorization: command on an RFQ the agent isn't matched to → 403. ---
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "clarification.request", "idempotency_key": "clar-1", "rfq_id": "01ZZZZZZZZZZZZZZZZZZZZZZZZZ",
	})
	require.Equal(t, http.StatusForbidden, st)

	// --- Validation: unsupported type and missing rfq_id on quote.submit. ---
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "explode", "idempotency_key": "x",
	})
	require.Equal(t, http.StatusBadRequest, st)
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": "noref",
	})
	require.Equal(t, http.StatusBadRequest, st)

	// --- Audit log: an entry exists for the accepted command. ---
	var audits int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE company_id = $1 AND action = 'command.quote.submit' AND target_id = $2`,
		sellerCompanyID, cmdID).Scan(&audits))
	require.Equal(t, 1, audits)

	// --- Ack: the seller's rfq.matched event acks (204), then is not replayable. ---
	var seq int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT seq FROM agent_events WHERE company_id = $1 AND payload->>'rfq_id' = $2`,
		sellerCompanyID, rfqID).Scan(&seq))

	st, _ = doJSON(t, gw, "POST", "/agent/v1/events/9223372036854775807/ack", secret, nil)
	require.Equal(t, http.StatusNotFound, st, "acking an unknown event is a 404")

	ackPath := "/agent/v1/events/" + strconv.FormatInt(seq, 10) + "/ack"
	st, _ = doJSON(t, gw, "POST", ackPath, secret, nil)
	require.Equal(t, http.StatusNoContent, st)

	var ackedAt *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT acked_at::text FROM agent_events WHERE seq = $1`, seq).Scan(&ackedAt))
	require.NotNil(t, ackedAt, "acked event records acked_at")
}

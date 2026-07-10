package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

// registerSeller registers a seller, gives it a matching packaging profile, and
// issues an agent credential. Returns (token, companyID, agentSecret).
func registerSeller(t *testing.T, api *fiber.App, name string) (string, string, string) {
	t.Helper()
	st, s := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": name, "company_type": "seller",
		"email": uniqueEmail("s4-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	token := s["token"].(string)
	st, _ = doJSON(t, api, "PUT", "/v1/suppliers/me", token, map[string]any{
		"description": "cartons and pallets", "categories": []string{"packaging"}, "location": "Dubai",
	})
	require.Equal(t, http.StatusOK, st)
	st, cred := doJSON(t, api, "POST", "/v1/agent-credentials", token, map[string]any{"label": "gw"})
	require.Equal(t, http.StatusCreated, st)
	return token, s["company_id"].(string), cred["secret"].(string)
}

// TestA4_FullLoop is the Phase 1 definition of done: a plain-text RFQ → real
// match → agent quotes via the gateway → Core ranks → buyer accepts → purchase
// order exists → the winning seller's agent gets order.created. No mocks.
func TestA4_FullLoop(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	_, sellerACompany, secretA := registerSeller(t, api, "CheapPack")
	_, sellerBCompany, secretB := registerSeller(t, api, "PriceyPack")

	// Buyer posts an RFQ; both sellers match.
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "LoopBuyer", "company_type": "buyer",
		"email": uniqueEmail("b4-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	// Both agents submit quotes via the gateway. A is cheaper.
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretA, map[string]any{
		"type": "quote.submit", "idempotency_key": "a1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 10, "notes": "in stock"},
	})
	require.Equal(t, http.StatusCreated, st)
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretB, map[string]any{
		"type": "quote.submit", "idempotency_key": "b1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1500, "currency": "USD", "delivery_days": 7},
	})
	require.Equal(t, http.StatusCreated, st)

	// A malformed quote (no price) is rejected before any command is written.
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretA, map[string]any{
		"type": "quote.submit", "idempotency_key": "bad", "rfq_id": rfqID,
		"payload": map[string]any{"currency": "USD"},
	})
	require.Equal(t, http.StatusBadRequest, st)

	// Buyer sees ranked quotes — cheapest (seller A) first.
	st, listed := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quotes := listed["quotes"].([]any)
	require.Len(t, quotes, 2)
	top := quotes[0].(map[string]any)
	require.Equal(t, sellerACompany, top["seller_company_id"], "cheapest quote ranks first")
	require.Equal(t, float64(1), top["rank"])
	require.Equal(t, sellerBCompany, quotes[1].(map[string]any)["seller_company_id"])
	winningQuoteID := top["id"].(string)

	// Buyer accepts the winning quote → purchase order created.
	st, accepted := doJSON(t, api, "POST", "/v1/quotes/"+winningQuoteID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusCreated, st)
	orderID := accepted["order_id"].(string)
	require.Equal(t, float64(100000), accepted["total_cents"], "price normalized to cents")

	// RFQ is now closed; the losing quote is rejected.
	st, gotRFQ := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID, buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "closed", gotRFQ["rfq"].(map[string]any)["status"])

	var winStatus, loseStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM quotes WHERE id=$1`, winningQuoteID).Scan(&winStatus))
	require.Equal(t, "accepted", winStatus)
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT status FROM quotes WHERE rfq_id=$1 AND seller_company_id=$2`, rfqID, sellerBCompany).Scan(&loseStatus))
	require.Equal(t, "rejected", loseStatus)

	// Order is visible to both parties, hidden from a third company.
	st, _ = doJSON(t, api, "GET", "/v1/orders/"+orderID, buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	st, _ = doJSON(t, api, "GET", "/v1/orders/"+orderID, buyer["token"].(string), nil) // buyer again, sanity
	require.Equal(t, http.StatusOK, st)

	// The winning seller's agent has a durable order.created event.
	var events int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='order.created' AND payload->>'order_id'=$2`,
		sellerACompany, orderID).Scan(&events))
	require.Equal(t, 1, events, "winning seller's agent receives order.created")

	// The losing seller gets NO order.created.
	var loserEvents int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='order.created' AND payload->>'order_id'=$2`,
		sellerBCompany, orderID).Scan(&loserEvents))
	require.Equal(t, 0, loserEvents)

	// Re-accepting the (now accepted) quote is a conflict.
	st, _ = doJSON(t, api, "POST", "/v1/quotes/"+winningQuoteID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusConflict, st)
}

// TestQuotes_AcceptNotifiesLosers verifies accepting one quote writes a
// durable quote.rejected event for each other submitted seller (in addition
// to order.created for the winner).
func TestQuotes_AcceptNotifiesLosers(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	_, sellerACompany, secretA := registerSeller(t, api, "CheapPack2")
	_, sellerBCompany, secretB := registerSeller(t, api, "PriceyPack2")

	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "LoopBuyer2", "company_type": "buyer",
		"email": uniqueEmail("b4b-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	// Both agents submit quotes via the gateway. A is cheaper and will win.
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretA, map[string]any{
		"type": "quote.submit", "idempotency_key": "a1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 10, "notes": "in stock"},
	})
	require.Equal(t, http.StatusCreated, st)
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretB, map[string]any{
		"type": "quote.submit", "idempotency_key": "b1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1500, "currency": "USD", "delivery_days": 7},
	})
	require.Equal(t, http.StatusCreated, st)

	// Find the loser's (seller B's) quote id before it gets flipped to rejected.
	var loserQuoteID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM quotes WHERE rfq_id=$1 AND seller_company_id=$2`, rfqID, sellerBCompany).Scan(&loserQuoteID))

	st, listed := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quotes := listed["quotes"].([]any)
	require.Len(t, quotes, 2)
	winningQuoteID := quotes[0].(map[string]any)["id"].(string)
	require.Equal(t, sellerACompany, quotes[0].(map[string]any)["seller_company_id"])

	// Buyer accepts the winning quote.
	st, accepted := doJSON(t, api, "POST", "/v1/quotes/"+winningQuoteID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusCreated, st)
	orderID := accepted["order_id"].(string)

	// The losing quote is now rejected in the DB.
	var loseStatus string
	require.NoError(t, pool.QueryRow(ctx, `SELECT status FROM quotes WHERE id=$1`, loserQuoteID).Scan(&loseStatus))
	require.Equal(t, "rejected", loseStatus)

	// The winning seller's agent still gets order.created.
	var orderEvents int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='order.created' AND payload->>'order_id'=$2`,
		sellerACompany, orderID).Scan(&orderEvents))
	require.Equal(t, 1, orderEvents, "winning seller's agent receives order.created")

	// The losing seller's agent receives a durable quote.rejected event for its quote.
	var rejectedEvents int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='quote.rejected' AND payload->>'quote_id'=$2 AND payload->>'reason'='not_selected'`,
		sellerBCompany, loserQuoteID).Scan(&rejectedEvents))
	require.Equal(t, 1, rejectedEvents, "losing seller's agent receives a durable quote.rejected event")

	// The winning seller does NOT get a quote.rejected event.
	var winnerRejected int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='quote.rejected'`,
		sellerACompany).Scan(&winnerRejected))
	require.Equal(t, 0, winnerRejected)
}

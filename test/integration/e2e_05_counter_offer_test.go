package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// E2E #5 (Sprint 5): a buyer counters a submitted quote; the seller's agent
// replies with a revised, VERSIONED quote (quote.counter); the immutable thread
// carries both versions; and accepting the quote snapshots the LATEST (counter)
// terms into the purchase order.
//
// The agent is simulated by sending the quote.counter gateway command directly
// (as the A-series simulates quote.submit) — the real agent counter reasoning is
// unit-tested in the agent repo.
func TestE2E05_CounterOfferVersionedThenAcceptLatest(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)

	_, _, secret := registerSeller(t, api, "CounterPack")

	// Buyer signs up, verifies, and posts a matching RFQ.
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E5 Buyer", "company_type": "buyer",
		"email": uniqueEmail("e2e5-buyer-"), "password": "password123",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	verifyUser(t, pool, buyer["user_id"].(string))

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	// The seller's agent submits an initial quote ($1000) via the gateway → v1.
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": "e2e5-submit", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 14, "notes": "in stock"},
	})
	require.Equal(t, http.StatusCreated, st)

	// Buyer sees the ranked quote (v1 @ $1000).
	st, listed := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quotes := listed["quotes"].([]any)
	require.Len(t, quotes, 1)
	quoteID := quotes[0].(map[string]any)["id"].(string)
	require.Equal(t, float64(100000), quotes[0].(map[string]any)["price_cents"])

	// Buyer counters: proposes $800.
	st, _ = doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/counter", buyerToken, map[string]any{
		"price_cents": 80000, "message": "Can you do 800?",
	})
	require.Equal(t, http.StatusOK, st)

	// The seller's agent replies with a counter ($900) via the gateway → v2.
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.counter", "idempotency_key": "e2e5-counter",
		"payload": map[string]any{"quote_id": quoteID, "price": 900, "currency": "USD", "delivery_days": 10, "notes": "meeting you partway"},
	})
	require.Equal(t, http.StatusCreated, st)

	// The thread carries both immutable versions (v1 seller, v2 counter); head is v2 @ $900.
	st, thread := doJSON(t, api, "GET", "/v1/quotes/"+quoteID+"/thread", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	versions := thread["versions"].([]any)
	require.Len(t, versions, 2)
	require.Equal(t, float64(1), versions[0].(map[string]any)["version"])
	require.Equal(t, "seller", versions[0].(map[string]any)["source"])
	require.Equal(t, float64(2), versions[1].(map[string]any)["version"])
	require.Equal(t, "counter", versions[1].(map[string]any)["source"])
	require.Equal(t, float64(90000), versions[1].(map[string]any)["price_cents"])

	// Buyer accepts → the order snapshots the LATEST (counter) terms: $900.
	st, accepted := doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusCreated, st)
	require.Equal(t, float64(90000), accepted["total_cents"], "accept snapshots the latest countered terms into the PO")

	orderID := accepted["order_id"].(string)
	st, ord := doJSON(t, api, "GET", "/v1/orders/"+orderID, buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, float64(90000), ord["order"].(map[string]any)["total_cents"])
}

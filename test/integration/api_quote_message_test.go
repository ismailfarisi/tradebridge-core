package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestQuoteMessage_AttachmentsAndEvent covers Task 7: a buyer posting a
// message with attachments into a quote thread gets back JSON-array
// attachment metadata (not a base64 string), and the seller's agent receives
// a durable quote.message event carrying the same attachment. A
// seller-authored post must NOT produce a quote.message event (the event only
// closes the loop the agent opened via clarification.request).
func TestQuoteMessage_AttachmentsAndEvent(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	sellerToken, sellerCompany, sellerSecret := registerSeller(t, api, "MsgPack")

	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "MsgBuyer", "company_type": "buyer",
		"email": uniqueEmail("bmsg-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	verifyUser(t, pool, buyer["user_id"].(string))

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", sellerSecret, map[string]any{
		"type": "quote.submit", "idempotency_key": "m1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 10, "notes": "in stock"},
	})
	require.Equal(t, http.StatusCreated, st)

	st, listed := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quotes := listed["quotes"].([]any)
	require.Len(t, quotes, 1)
	quoteID := quotes[0].(map[string]any)["id"].(string)

	// Buyer uploads an attachment, then posts it into the quote thread.
	meta := uploadPNG(t, api, buyerToken)
	attID := meta["id"].(string)

	st, posted := doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/messages", buyerToken, map[string]any{
		"body": "see photo", "attachment_ids": []string{attID},
	})
	require.Equal(t, http.StatusCreated, st)

	// 1. Response carries attachment metadata as a real JSON array, not a
	// base64-encoded string.
	atts, ok := posted["attachments"].([]any)
	require.True(t, ok, "attachments must decode as a JSON array, got %T: %v", posted["attachments"], posted["attachments"])
	require.Len(t, atts, 1)
	require.Equal(t, attID, atts[0].(map[string]any)["id"])

	// 2. A durable quote.message event was written for the seller's agent,
	// with body/quote_id/attachments[0].id in the payload.
	var events int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events
		 WHERE company_id=$1 AND type='quote.message'
		   AND payload->>'quote_id'=$2 AND payload->>'body'='see photo'
		   AND payload->'attachments'->0->>'id'=$3`,
		sellerCompany, quoteID, attID).Scan(&events))
	require.Equal(t, 1, events, "seller's agent receives quote.message with body/quote_id/attachments[0].id")

	// 3. A seller-authored post produces NO quote.message event.
	st, _ = doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/messages", sellerToken, map[string]any{
		"body": "thanks, will review",
	})
	require.Equal(t, http.StatusCreated, st)

	var eventsAfter int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='quote.message'`,
		sellerCompany).Scan(&eventsAfter))
	require.Equal(t, 1, eventsAfter, "seller-authored message must not emit quote.message")
}

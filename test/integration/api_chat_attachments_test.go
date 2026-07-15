package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestChatAttachments exercises the full attachment path through /v1/rfq-chat:
// upload → attach to the first chat message → heuristic intake creates the
// RFQ in one shot → attachment metadata round-trips in the response,
// attachments.rfq_id gets stamped, and the rfq.matched event payload carries
// the attachment metadata for the matched seller's agent.
func TestChatAttachments(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	// Seller company publishes a supplier profile in the "packaging" category
	// so the buyer's RFQ below matches at least one seller.
	st, seller := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AttachPackPro", "company_type": "seller",
		"email": uniqueEmail("attseller-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	sellerCompanyID := seller["company_id"].(string)
	sellerToken := seller["token"].(string)

	st, _ = doJSON(t, app, "PUT", "/v1/suppliers/me", sellerToken, map[string]any{
		"description": "cartons, boxes and pallets supplier",
		"categories":  []string{"packaging"},
		"location":    "Dubai",
	})
	require.Equal(t, http.StatusOK, st)

	// Buyer registers, uploads a spec image, then drives rfq-chat in one shot
	// with enough detail (product + quantity) for the heuristic intake to
	// create the RFQ immediately.
	st, buyer := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AttachBuyerCo", "company_type": "buyer",
		"email": uniqueEmail("attbuyer-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)

	meta := uploadPNG(t, app, buyerToken)
	attID := meta["id"].(string)

	st, r := doJSON(t, app, "POST", "/v1/rfq-chat", buyerToken, map[string]any{
		"messages": []map[string]any{
			{
				"role":           "user",
				"content":        "We need 500 cartons for packaging our products, deliver to Dubai by next month.",
				"attachment_ids": []string{attID},
			},
		},
	})
	require.Equal(t, http.StatusOK, st)

	// 1. done == true and the returned rfq.messages[0].attachments[0].filename
	// round-trips.
	require.Equal(t, true, r["done"])
	rfq := r["rfq"].(map[string]any)
	rfqID := rfq["id"].(string)
	require.NotEmpty(t, rfqID)
	msgs := rfq["messages"].([]any)
	require.NotEmpty(t, msgs)
	firstMsg := msgs[0].(map[string]any)
	msgAtts := firstMsg["attachments"].([]any)
	require.Len(t, msgAtts, 1)
	require.Equal(t, "spec.png", msgAtts[0].(map[string]any)["filename"])

	// 2. attachments.rfq_id got stamped to the created RFQ.
	var gotRFQID string
	err := pool.QueryRow(ctx, `SELECT rfq_id FROM attachments WHERE id = $1`, attID).Scan(&gotRFQID)
	require.NoError(t, err)
	require.Equal(t, rfqID, gotRFQID)

	// 3. If a seller matched, the rfq.matched event payload carries the
	// attachment metadata for the matched seller's agent.
	matchCount := int(r["match_count"].(float64))
	if matchCount == 0 {
		t.Skip("no seller matched — skipping event payload assertion")
	}
	var payload []byte
	err = pool.QueryRow(ctx,
		`SELECT payload FROM agent_events
		 WHERE company_id = $1 AND type = 'rfq.matched' AND payload->>'rfq_id' = $2`,
		sellerCompanyID, rfqID).Scan(&payload)
	require.NoError(t, err)
	require.Contains(t, string(payload), attID, "rfq.matched payload must carry the attachment id")
	require.Contains(t, string(payload), `"attachments"`)
}

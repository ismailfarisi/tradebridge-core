package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// TestGatewayAttachment_MatchedAgentGetsBytes_UnmatchedGets404 exercises
// GET /agent/v1/attachments/:id: an agent whose seller company is matched to
// the attachment's RFQ can download the bytes with the right Content-Type,
// and an agent whose company is not matched is told 404 (not 403) so
// attachment IDs don't leak existence.
func TestGatewayAttachment_MatchedAgentGetsBytes_UnmatchedGets404(t *testing.T) {
	api := buildAPI(t)
	gw, q, store := buildGatewayWithStore(t)
	ctx := context.Background()

	// Matched seller: profile matches the RFQ's category, so RFQ creation
	// records a rfq_matches row for it.
	st, matchedSeller := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "MatchedAgentCo", "company_type": "seller",
		"email": uniqueEmail("matched-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	matchedToken := matchedSeller["token"].(string)

	st, _ = doJSON(t, api, "PUT", "/v1/suppliers/me", matchedToken, map[string]any{
		"description": "cartons and pallets", "categories": []string{"packaging"}, "location": "Dubai",
	})
	require.Equal(t, http.StatusOK, st)

	st, matchedCred := doJSON(t, api, "POST", "/v1/agent-credentials", matchedToken, map[string]any{"label": "gw"})
	require.Equal(t, http.StatusCreated, st)
	matchedSecret := matchedCred["secret"].(string)

	// Unmatched seller: unrelated category, so it never lands in rfq_matches
	// for this RFQ.
	st, unmatchedSeller := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "UnmatchedAgentCo", "company_type": "seller",
		"email": uniqueEmail("unmatched-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	unmatchedToken := unmatchedSeller["token"].(string)

	st, _ = doJSON(t, api, "PUT", "/v1/suppliers/me", unmatchedToken, map[string]any{
		"description": "bespoke jewellery", "categories": []string{"jewellery"}, "location": "Dubai",
	})
	require.Equal(t, http.StatusOK, st)

	st, unmatchedCred := doJSON(t, api, "POST", "/v1/agent-credentials", unmatchedToken, map[string]any{"label": "gw"})
	require.Equal(t, http.StatusCreated, st)
	unmatchedSecret := unmatchedCred["secret"].(string)

	// Buyer posts an RFQ that matches only the "packaging" seller.
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AttachBuyer", "company_type": "buyer",
		"email": uniqueEmail("attbuyer-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	buyerCompanyID := buyer["company_id"].(string)
	require.NoError(t, q.MarkUserEmailVerified(ctx, buyer["user_id"].(string)))

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	// Seed an attachment row owned by the buyer, put its bytes directly in
	// the gateway's store (the buyer-facing upload API uses a separate temp
	// dir in these tests), and stamp it onto the RFQ.
	data := []byte("pdf-bytes-for-gateway-attachment-test")
	sum := sha256.Sum256(data)
	att, err := q.CreateAttachment(ctx, sqlc.CreateAttachmentParams{
		CompanyID: buyerCompanyID,
		Filename:  "spec.pdf",
		Mime:      "application/pdf",
		SizeBytes: int64(len(data)),
		Sha256:    hex.EncodeToString(sum[:]),
	})
	require.NoError(t, err)
	require.NoError(t, store.Put(att.ID, data))
	require.NoError(t, q.SetAttachmentsRFQ(ctx, sqlc.SetAttachmentsRFQParams{
		RfqID: &rfqID, Ids: []string{att.ID}, CompanyID: buyerCompanyID,
	}))

	// --- Matched agent: 200, correct bytes + Content-Type. ---
	req := httptest.NewRequest("GET", "/agent/v1/attachments/"+att.ID, nil)
	req.Header.Set("Authorization", "Bearer "+matchedSecret)
	resp, err := gw.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/pdf", resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, data, body)

	// --- Unmatched agent: 404, attachment existence not revealed. ---
	req2 := httptest.NewRequest("GET", "/agent/v1/attachments/"+att.ID, nil)
	req2.Header.Set("Authorization", "Bearer "+unmatchedSecret)
	resp2, err := gw.Test(req2, -1)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp2.StatusCode)

	// --- Unknown attachment ID: also 404. ---
	req3 := httptest.NewRequest("GET", "/agent/v1/attachments/01ZZZZZZZZZZZZZZZZZZZZZZZZZ", nil)
	req3.Header.Set("Authorization", "Bearer "+matchedSecret)
	resp3, err := gw.Test(req3, -1)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp3.StatusCode)
}

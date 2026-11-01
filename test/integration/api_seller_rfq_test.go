package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// sellerRFQIDs returns the ids in a GET /v1/seller/rfqs response.
func sellerRFQIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	rows, ok := body["rfqs"].([]any)
	require.True(t, ok, "rfqs must be an array: %v", body)
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.(map[string]any)["id"].(string))
	}
	return ids
}

// TestSellerRFQ_InboxListDetailAndIsolation covers the seller RFQ inbox: a
// matched seller lists and opens the RFQ, the tabs move it from open to
// quoted to closed, the "new" flag clears on open, and neither the buyer's
// intake chat nor other companies' RFQs leak.
func TestSellerRFQ_InboxListDetailAndIsolation(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	sellerToken, sellerCompanyID, _ := seedSellerWithCatalog(t, app, "packaging")
	outsiderToken, _, _ := seedSellerWithCatalog(t, app, "industrial lasers")
	buyerToken, _ := seedVerifiedBuyer(t, app, pool, "sellerinbox-buyer-")

	st, created := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons needed",
		"text":  "We need 500 cartons for packaging our products, deliver to Dubai by next month.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	// Matched seller sees it as new and actionable.
	st, list := doJSON(t, app, "GET", "/v1/seller/rfqs", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, sellerRFQIDs(t, list), rfqID)
	row := list["rfqs"].([]any)[0].(map[string]any)
	require.Equal(t, rfqID, row["id"])
	require.Equal(t, true, row["unseen"], "a freshly matched RFQ is flagged new")
	require.Nil(t, row["quote"])
	require.NotEmpty(t, row["match_reason"])
	require.Equal(t, "packaging", row["category"])

	st, open := doJSON(t, app, "GET", "/v1/seller/rfqs?tab=open", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, sellerRFQIDs(t, open), rfqID)

	st, searched := doJSON(t, app, "GET", "/v1/seller/rfqs?q=cartons", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, sellerRFQIDs(t, searched), rfqID)
	st, missed := doJSON(t, app, "GET", "/v1/seller/rfqs?q=zzz-no-such-rfq", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Empty(t, sellerRFQIDs(t, missed))

	st, _ = doJSON(t, app, "GET", "/v1/seller/rfqs?tab=bogus", sellerToken, nil)
	require.Equal(t, http.StatusBadRequest, st)

	// Detail: requirements and match context, no buyer chat, no quote yet.
	st, detail := doJSON(t, app, "GET", "/v1/seller/rfqs/"+rfqID, sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	rfq := detail["rfq"].(map[string]any)
	require.Equal(t, "Cartons needed", rfq["title"])
	require.Contains(t, rfq["raw_text"], "500 cartons")
	require.NotNil(t, rfq["requirements"])
	require.NotNil(t, rfq["expires_at"], "a matched RFQ carries its expiry")
	require.NotContains(t, rfq, "messages", "the buyer's intake chat is never exposed to sellers")
	require.NotContains(t, rfq, "buyer_company_id")
	require.Nil(t, rfq["quote"])
	require.Equal(t, []any{}, detail["attachments"])

	// Opening the detail clears the new flag.
	st, list = doJSON(t, app, "GET", "/v1/seller/rfqs", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.NotContains(t, list["rfqs"].([]any)[0].(map[string]any), "unseen")

	// Once the seller has quoted, the RFQ moves from the open tab to quoted.
	_, err := pool.Exec(ctx,
		`INSERT INTO quotes (rfq_id, seller_company_id, price_cents, currency, delivery_days, notes)
		 VALUES ($1, $2, 125000, 'AED', 7, 'Double-wall cartons')`, rfqID, sellerCompanyID)
	require.NoError(t, err)

	st, open = doJSON(t, app, "GET", "/v1/seller/rfqs?tab=open", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.NotContains(t, sellerRFQIDs(t, open), rfqID)
	st, quoted := doJSON(t, app, "GET", "/v1/seller/rfqs?tab=quoted", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, sellerRFQIDs(t, quoted), rfqID)
	listed := quoted["rfqs"].([]any)[0].(map[string]any)["quote"].(map[string]any)
	require.Equal(t, float64(125000), listed["price_cents"])
	require.Equal(t, "AED", listed["currency"])

	st, detail = doJSON(t, app, "GET", "/v1/seller/rfqs/"+rfqID, sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quote := detail["rfq"].(map[string]any)["quote"].(map[string]any)
	require.Equal(t, "submitted", quote["status"])
	require.Equal(t, float64(7), quote["delivery_days"])
	require.Equal(t, "Double-wall cartons", quote["notes"])

	// Buyer closes it: it leaves quoted and lands in closed.
	st, _ = doJSON(t, app, "POST", "/v1/rfqs/"+rfqID+"/close", buyerToken, map[string]any{})
	require.Equal(t, http.StatusOK, st)
	st, quoted = doJSON(t, app, "GET", "/v1/seller/rfqs?tab=quoted", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.NotContains(t, sellerRFQIDs(t, quoted), rfqID)
	st, closed := doJSON(t, app, "GET", "/v1/seller/rfqs?tab=closed", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Contains(t, sellerRFQIDs(t, closed), rfqID)

	// Isolation: an unmatched seller and the buyer itself get 404 on the
	// seller view, and the unmatched seller's inbox does not list it.
	st, _ = doJSON(t, app, "GET", "/v1/seller/rfqs/"+rfqID, outsiderToken, nil)
	require.Equal(t, http.StatusNotFound, st)
	st, _ = doJSON(t, app, "GET", "/v1/seller/rfqs/"+rfqID, buyerToken, nil)
	require.Equal(t, http.StatusNotFound, st)
	st, outsiderList := doJSON(t, app, "GET", "/v1/seller/rfqs", outsiderToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.NotContains(t, sellerRFQIDs(t, outsiderList), rfqID)

	// Unauthenticated callers are rejected.
	st, _ = doJSON(t, app, "GET", "/v1/seller/rfqs", "", nil)
	require.Equal(t, http.StatusUnauthorized, st)
}

// TestSellerRFQ_MatchNotificationLinksToSellerInbox checks the "New RFQ
// matched" notification (in-app bell and email CTA) sends the seller to the
// seller RFQ page, not the buyer-only /rfqs/:id page.
func TestSellerRFQ_MatchNotificationLinksToSellerInbox(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)

	sellerToken, _, _ := seedSellerWithCatalog(t, app, "packaging")
	buyerToken, _ := seedVerifiedBuyer(t, app, pool, "sellernotify-buyer-")

	st, created := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Boxes", "text": "Need 200 cartons for packaging, Dubai delivery.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	st, notes := doJSON(t, app, "GET", "/v1/notifications", sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	items, ok := notes["notifications"].([]any)
	require.True(t, ok, "notifications must be an array: %v", notes)
	found := false
	for _, it := range items {
		n := it.(map[string]any)
		if n["type"] == "rfq.matched" {
			data, ok := n["data"].(map[string]any)
			require.True(t, ok, "rfq.matched carries data: %v", n)
			require.Equal(t, "/seller/rfqs/"+rfqID, data["cta_path"])
			require.Equal(t, rfqID, data["rfq_id"])
			found = true
		}
	}
	require.True(t, found, "the matched seller gets an rfq.matched notification")
}

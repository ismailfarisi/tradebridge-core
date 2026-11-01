package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// sellerTestCategory is the supplier category the seller RFQ tests register
// under, and a word their RFQs contain so matching finds those sellers. It is
// deliberately not "packaging": other tests rely on MatchSuppliers (LIMIT 25)
// still finding their packaging seller, so these tests must not add more.
const sellerTestCategory = "kraftliner"

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

	sellerToken, sellerCompanyID, _ := seedSellerWithCatalog(t, app, sellerTestCategory)
	outsiderToken, _, _ := seedSellerWithCatalog(t, app, "industrial lasers")
	buyerToken, _ := seedVerifiedBuyer(t, app, pool, "sellerinbox-buyer-")

	st, created := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons needed",
		"text":  "We need 500 kraftliner cartons for packaging our products, deliver to Dubai by next month.",
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
	require.Contains(t, rfq["raw_text"], "500 kraftliner cartons")
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

	sellerToken, _, _ := seedSellerWithCatalog(t, app, sellerTestCategory)
	buyerToken, _ := seedVerifiedBuyer(t, app, pool, "sellernotify-buyer-")

	st, created := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Boxes", "text": "Need 200 kraftliner cartons for packaging, Dubai delivery.",
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

// notificationsOfType returns the caller's notifications of one type, newest first.
func notificationsOfType(t *testing.T, app fiberApp, token, typ string) []map[string]any {
	t.Helper()
	st, body := doJSON(t, app, "GET", "/v1/notifications", token, nil)
	require.Equal(t, http.StatusOK, st)
	var out []map[string]any
	for _, it := range body["notifications"].([]any) {
		n := it.(map[string]any)
		if n["type"] == typ {
			out = append(out, n)
		}
	}
	return out
}

// TestSellerRFQ_ManualQuoteSubmitReviseAndLock covers the seller quote form:
// verified-email gate, validation, first quote (RFQ advances to quoted, buyer
// notified), revised offer (new version, thread message, buyer notified),
// the seller hearing about a buyer counter, and no changes after acceptance
// or once the RFQ is closed.
func TestSellerRFQ_ManualQuoteSubmitReviseAndLock(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	ctx := context.Background()

	sellerToken, sellerCompanyID, sellerUserID := seedSellerWithCatalog(t, app, sellerTestCategory)
	otherToken, _, otherUserID := seedSellerWithCatalog(t, app, sellerTestCategory)
	outsiderToken, _, outsiderUserID := seedSellerWithCatalog(t, app, "industrial lasers")
	buyerToken, _ := seedVerifiedBuyer(t, app, pool, "manualquote-buyer-")

	st, created := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons for launch",
		"text":  "We need 800 kraftliner cartons for packaging, deliver to Dubai by next month.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)
	path := "/v1/seller/rfqs/" + rfqID + "/quote"
	good := map[string]any{"price": 12500.5, "currency": "aed", "delivery_days": 7, "notes": "Double-wall, printed"}

	// Unverified sellers are asked to verify their email first.
	st, body := doJSON(t, app, "POST", path, sellerToken, good)
	require.Equal(t, http.StatusForbidden, st)
	require.Equal(t, "verify_email", body["code"])
	verifyUser(t, pool, sellerUserID)
	verifyUser(t, pool, otherUserID)
	verifyUser(t, pool, outsiderUserID)

	// Validation.
	for _, bad := range []map[string]any{
		{"price": 0},
		{"price": -5},
		{"currency": "AED"},
		{"price": 100, "delivery_days": -1},
		{"price": 100, "currency": "DIRHAM"},
		{"price": 100, "notes": strings.Repeat("x", 2001)},
	} {
		st, _ = doJSON(t, app, "POST", path, sellerToken, bad)
		require.Equal(t, http.StatusBadRequest, st, "payload %v", bad)
	}

	// Unmatched sellers cannot quote (and cannot learn the RFQ exists).
	st, _ = doJSON(t, app, "POST", path, outsiderToken, good)
	require.Equal(t, http.StatusNotFound, st)

	// First quote.
	st, body = doJSON(t, app, "POST", path, sellerToken, good)
	require.Equal(t, http.StatusCreated, st, "%v", body)
	require.Equal(t, false, body["revised"])
	quote := body["quote"].(map[string]any)
	quoteID := quote["id"].(string)
	require.Equal(t, float64(1250050), quote["price_cents"])
	require.Equal(t, "AED", quote["currency"])
	require.Equal(t, float64(1), quote["version"])

	st, got := doJSON(t, app, "GET", "/v1/rfqs/"+rfqID, buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "quoted", got["rfq"].(map[string]any)["status"], "first quote advances open → quoted")
	st, ranked := doJSON(t, app, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Len(t, ranked["quotes"].([]any), 1)
	require.NotEmpty(t, notificationsOfType(t, app, buyerToken, "quote.submitted"), "buyer is told a quote arrived")

	// Seller detail now shows the quote.
	st, detail := doJSON(t, app, "GET", "/v1/seller/rfqs/"+rfqID, sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, quoteID, detail["rfq"].(map[string]any)["quote"].(map[string]any)["id"])

	// Buyer counters: a seller without an autonomous agent is notified and
	// sent to the seller RFQ page.
	st, _ = doJSON(t, app, "POST", "/v1/quotes/"+quoteID+"/counter", buyerToken, map[string]any{
		"price_cents": 1100000, "message": "Can you do 11,000?",
	})
	require.Equal(t, http.StatusOK, st)
	counters := notificationsOfType(t, app, sellerToken, "quote.countered")
	require.NotEmpty(t, counters, "seller hears about the buyer's counter")
	require.Equal(t, "/seller/rfqs/"+rfqID, counters[0]["data"].(map[string]any)["cta_path"])

	// Revised offer: new version, seller thread message, buyer notified.
	st, body = doJSON(t, app, "POST", path, sellerToken, map[string]any{"price": 11500, "delivery_days": 5})
	require.Equal(t, http.StatusOK, st, "%v", body)
	require.Equal(t, true, body["revised"])
	revised := body["quote"].(map[string]any)
	require.Equal(t, quoteID, revised["id"], "a revision keeps the same quote")
	require.Equal(t, float64(1150000), revised["price_cents"])
	require.Equal(t, float64(2), revised["version"])

	var sources []string
	rows, err := pool.Query(ctx, `SELECT source FROM quote_versions WHERE quote_id = $1 ORDER BY version`, quoteID)
	require.NoError(t, err)
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		sources = append(sources, s)
	}
	require.Equal(t, []string{"seller", "counter"}, sources)

	st, msgs := doJSON(t, app, "GET", "/v1/quotes/"+quoteID+"/messages", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	list := msgs["messages"].([]any)
	last := list[len(list)-1].(map[string]any)
	require.Equal(t, "seller", last["author"])
	require.Contains(t, last["body"], "Revised offer: 11,500.00 AED, delivery in 5 days")
	require.NotEmpty(t, notificationsOfType(t, app, buyerToken, "quote.countered"), "buyer is told about the revision")

	// Buyer accepts: the quote is locked.
	st, accepted := doJSON(t, app, "POST", "/v1/quotes/"+quoteID+"/accept", buyerToken, map[string]any{})
	require.Equal(t, http.StatusCreated, st, "%v", accepted)
	st, _ = doJSON(t, app, "POST", path, sellerToken, good)
	require.Equal(t, http.StatusConflict, st)

	// The RFQ is closed now, so the other matched seller cannot quote either.
	st, _ = doJSON(t, app, "POST", path, otherToken, good)
	require.Equal(t, http.StatusConflict, st)

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM quotes WHERE rfq_id = $1 AND seller_company_id = $2`, rfqID, sellerCompanyID).Scan(&count))
	require.Equal(t, 1, count)
}

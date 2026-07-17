package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #4 (Sprint 4): a buyer accepts a quote — producing an invoice (PDF
// downloadable, buyer notified) — then the order walks its full lifecycle
// (confirmed → fulfilled → completed) driven by the correct party each step,
// and a post-completion rating appears on the seller's public profile.
//
// Quote submission itself (via the agent gateway) is exercised by E2E in the
// A-series; here the quote is seeded directly so the test stays focused on the
// Sprint-4 journey: accept → invoice → lifecycle → review → reputation.
func TestE2E04_OrderLifecycleInvoiceReview(t *testing.T) {
	app := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	sellerToken, sellerCompanyID, _ := seedSellerWithCatalog(t, app, "widgets")

	// Buyer signs up and verifies email (RFQ create is gated on verification).
	buyerEmail := uniqueEmail("e2e4-buyer-")
	st, reg := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E4 Buyer", "company_type": "buyer",
		"email": buyerEmail, "password": "password123",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := reg["token"].(string)
	buyerCompanyID := reg["company_id"].(string)

	var vtoken string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT token FROM email_verifications ev JOIN users u ON u.id=ev.user_id WHERE u.email=$1`,
		buyerEmail).Scan(&vtoken))
	st, _ = doJSON(t, app, "POST", "/v1/auth/verify-email", "", map[string]any{"token": vtoken})
	require.Equal(t, http.StatusOK, st)

	// Buyer posts an RFQ.
	st, rfqResp := doJSON(t, app, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Need widgets", "text": "500 widgets",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := rfqResp["rfq"].(map[string]any)["id"].(string)

	// Seed the seller's submitted quote on the RFQ.
	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqID, SellerCompanyID: sellerCompanyID,
		PriceCents: 5000, Currency: "USD", Notes: "in stock", Completeness: 1,
	})
	require.NoError(t, err)

	// Before any review, the ranked quote shows the seller with zero ratings.
	st, listed := doJSON(t, app, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quotes := listed["quotes"].([]any)
	require.Len(t, quotes, 1)
	require.Equal(t, float64(0), quotes[0].(map[string]any)["seller_review_count"])

	// Buyer accepts → purchase order + invoice, atomically.
	st, accepted := doJSON(t, app, "POST", "/v1/quotes/"+quote.ID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusCreated, st)
	orderID := accepted["order_id"].(string)

	// The invoice PDF downloads for the buyer (auth required).
	req := httptest.NewRequest("GET", "/v1/orders/"+orderID+"/invoice.pdf", nil)
	req.Header.Set("Authorization", "Bearer "+buyerToken)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/pdf", resp.Header.Get("Content-Type"))
	head := make([]byte, 5)
	_, err = resp.Body.Read(head)
	require.NoError(t, err)
	require.Equal(t, "%PDF-", string(head))

	// The buyer was notified the invoice was issued.
	var invNotifs int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM notifications WHERE company_id=$1 AND type='invoice.issued'`,
		buyerCompanyID).Scan(&invNotifs))
	require.GreaterOrEqual(t, invNotifs, int64(1))

	// The order walks its lifecycle, each step by the correct party.
	// Seller confirms.
	st, _ = doJSON(t, app, "POST", "/v1/orders/"+orderID+"/transitions", sellerToken, map[string]any{"to": "confirmed"})
	require.Equal(t, http.StatusOK, st)
	// A buyer cannot mark it fulfilled (seller-only).
	st, _ = doJSON(t, app, "POST", "/v1/orders/"+orderID+"/transitions", buyerToken, map[string]any{"to": "fulfilled"})
	require.Equal(t, http.StatusForbidden, st)
	// Seller fulfils.
	st, _ = doJSON(t, app, "POST", "/v1/orders/"+orderID+"/transitions", sellerToken, map[string]any{"to": "fulfilled"})
	require.Equal(t, http.StatusOK, st)
	// Buyer completes.
	st, _ = doJSON(t, app, "POST", "/v1/orders/"+orderID+"/transitions", buyerToken, map[string]any{"to": "completed"})
	require.Equal(t, http.StatusOK, st)

	// A review is only possible now (post-completion). Buyer rates the seller 5.
	st, _ = doJSON(t, app, "POST", "/v1/orders/"+orderID+"/reviews", buyerToken, map[string]any{"rating": 5, "body": "great"})
	require.Equal(t, http.StatusOK, st)

	// The rating shows on the seller's public storefront.
	st, pub := doJSON(t, app, "GET", "/public/v1/sellers/"+sellerCompanyID, "", nil)
	require.Equal(t, http.StatusOK, st)
	rep := pub["reputation"].(map[string]any)
	require.Equal(t, float64(1), rep["review_count"])
	require.Equal(t, float64(5), rep["avg_rating"])

	// And in the public reviews list.
	st, revs := doJSON(t, app, "GET", "/public/v1/sellers/"+sellerCompanyID+"/reviews", "", nil)
	require.Equal(t, http.StatusOK, st)
	require.Len(t, revs["reviews"].([]any), 1)
}

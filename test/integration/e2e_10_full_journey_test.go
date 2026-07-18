package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #10 (Sprint 10, capstone): the full buyer↔seller journey, driven
// entirely through the real HTTP API and the real agent gateway — register
// both companies, publish a seller catalog, post a matching RFQ, have the
// seller's agent submit a quote via the gateway protocol, have the buyer
// accept it (producing an order + auto-invoice, S4), and walk the order to
// "fulfilled". This is the launch gate: every subsystem exercised by the
// other E2Es (auth/tenancy, matching, the agent gateway, quotes, orders,
// invoicing) has to work together in one pass.
func TestE2E10_FullBuyerSellerJourney(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// --- Step 1: register the seller company + owner, publish a supplier
	// profile and issue an agent credential (mirrors registerSeller, used by
	// the A-series/E2E07). ---
	sellerToken, sellerCompanyID, agentSecret := registerSeller(t, api, "E2E10 Seller Co")

	// --- Step 2: seller publishes at least one catalog item in the matching
	// category (mirrors seedSellerWithCatalog / TestE2E07's catalog step). ---
	st, catItem := doJSON(t, api, "POST", "/v1/catalog", sellerToken, map[string]any{
		"name": "Shipping Cartons 30cm", "category": "packaging",
		"summary": "sturdy corrugated cartons, bulk packaging",
	})
	require.Equal(t, http.StatusCreated, st)
	require.NotEmpty(t, catItem["id"])

	// --- Step 1 (cont'd): register the buyer company + owner, verify email
	// (RFQ create is gated on verification; mirrors seedVerifiedBuyer). ---
	buyerToken, buyerUserID := seedVerifiedBuyer(t, api, pool, "e2e10-buyer-")
	_ = buyerUserID

	st, meResp := doJSON(t, api, "GET", "/v1/me", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	buyerCompanyID := meResp["company_id"].(string)

	// --- Step 3: buyer posts an RFQ matching the seller's packaging profile;
	// the real matcher must find at least the seller we just seeded. ---
	st, rfqResp := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Need shipping cartons",
		"text":  "Need 1000 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st, "rfq create: %v", rfqResp)
	rfq := rfqResp["rfq"].(map[string]any)
	rfqID := rfq["id"].(string)
	matchCount, ok := rfqResp["match_count"].(float64)
	require.True(t, ok, "expected a match_count in the RFQ create response, got %v", rfqResp)
	require.GreaterOrEqual(t, matchCount, float64(1), "the RFQ must match the seller we just published a profile+catalog for")

	// --- Step 4: the seller's agent submits a quote through the real gateway
	// protocol (Bearer = issued agent secret), reusing the exact command shape
	// the A-series/E2E07 use for quote.submit. ---
	st, cmdResp := doJSON(t, gw, "POST", "/agent/v1/commands", agentSecret, map[string]any{
		"type": "quote.submit", "idempotency_key": "e2e10-submit-1", "rfq_id": rfqID,
		"payload": map[string]any{
			"price": 2500, "currency": "USD", "delivery_days": 12, "notes": "in stock, ships same week",
		},
	})
	require.Equal(t, http.StatusCreated, st, "quote.submit via gateway: %v", cmdResp)

	st, listed := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	quotes := listed["quotes"].([]any)
	require.Len(t, quotes, 1, "exactly one quote should have been submitted")
	quote := quotes[0].(map[string]any)
	require.Equal(t, sellerCompanyID, quote["seller_company_id"])
	quoteID := quote["id"].(string)

	// --- Step 5: buyer accepts the quote → purchase order + auto-invoice,
	// atomically (S4). ---
	st, accepted := doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusCreated, st, "quote accept: %v", accepted)
	orderID := accepted["order_id"].(string)
	acceptedTotalCents := accepted["total_cents"].(float64)
	require.Equal(t, float64(250000), acceptedTotalCents, "price normalized to cents (2500 * 100)")

	order, err := q.GetOrder(ctx, orderID)
	require.NoError(t, err)
	require.Equal(t, buyerCompanyID, order.BuyerCompanyID)
	require.Equal(t, sellerCompanyID, order.SellerCompanyID)

	invoice, err := q.GetInvoiceByOrder(ctx, orderID)
	require.NoError(t, err, "an invoice row must exist for the order (auto-invoice on accept, S4)")
	require.NotEmpty(t, invoice.InvoiceNumber, "invoice must have a number")
	require.True(t, strings.HasPrefix(invoice.InvoiceNumber, "INV-"), "invoice number should look like INV-YYYY-NNNNNN, got %q", invoice.InvoiceNumber)
	require.Equal(t, int64(acceptedTotalCents), invoice.TotalCents, "invoice total must match the accepted quote's total")

	// The invoice PDF is downloadable by the buyer (mirrors E2E04).
	st, _ = doJSON(t, api, "GET", "/v1/orders/"+orderID+"/invoice.pdf", buyerToken, nil)
	require.Equal(t, http.StatusOK, st, "invoice PDF must be downloadable once the order+invoice exist")

	// --- Step 6: walk the order to a terminal-ish accepted state and assert
	// both the status and the invoice total line up with the quote. ---
	st, _ = doJSON(t, api, "POST", "/v1/orders/"+orderID+"/transitions", sellerToken, map[string]any{"to": "confirmed"})
	require.Equal(t, http.StatusOK, st, "seller confirms the order")
	st, _ = doJSON(t, api, "POST", "/v1/orders/"+orderID+"/transitions", sellerToken, map[string]any{"to": "fulfilled"})
	require.Equal(t, http.StatusOK, st, "seller fulfils the order")

	fulfilledOrder, err := q.GetOrder(ctx, orderID)
	require.NoError(t, err)
	require.Equal(t, "fulfilled", fulfilledOrder.Status, "order must be in the fulfilled state")
	require.Equal(t, int64(acceptedTotalCents), fulfilledOrder.TotalCents)

	finalInvoice, err := q.GetInvoiceByOrder(ctx, orderID)
	require.NoError(t, err)
	require.Equal(t, invoice.ID, finalInvoice.ID, "invoice is immutable across order transitions")
	require.Equal(t, int64(acceptedTotalCents), finalInvoice.TotalCents, "invoice total_cents must match the quote total")
}

package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestE2E11_InflatedOpeningDoesNotInflateVerifiedSavings: one supplier opens
// at twice the others and "concedes"; the opening-quote saving looks huge but
// the verified (headline) saving is measured against the market baseline, the
// opening is flagged as an anchor, and the figures are snapshot at close.
func TestE2E11_InflatedOpeningDoesNotInflateVerifiedSavings(t *testing.T) {
	api := buildAPI(t)
	_, coA, secretA := registerSeller(t, api, "AnchorPackA")
	_, coB, secretB := registerSeller(t, api, "AnchorPackB")
	_, coC, secretC := registerSeller(t, api, "AnchorPackC")
	buyerToken, buyerCo, rfqID := negotiationBuyerRFQ(t, "AnchorBuyer")
	makePro(t, buyerCo)
	for _, co := range []string{coA, coB, coC} {
		ensureMatched(t, rfqID, co)
	}
	submitQuote(t, secretA, "anc-a-"+rfqID, rfqID, 2000) // anchor
	submitQuote(t, secretB, "anc-b-"+rfqID, rfqID, 1000)
	submitQuote(t, secretC, "anc-c-"+rfqID, rfqID, 1050)

	st, view := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 100000, "max_delivery_days": 14,
		"max_rounds": 1, "auto_accept": true,
	})
	require.Equal(t, http.StatusOK, st)
	quoteBySeller := map[string]string{}
	for _, s := range view["suppliers"].([]any) {
		sv := s.(map[string]any)
		quoteBySeller[sv["seller_name"].(string)] = sv["quote_id"].(string)
		require.Equal(t, sv["seller_name"] == "AnchorPackA", sv["anchored"], "only A's opening is an anchor: %v", sv)
		if sv["seller_name"] == "AnchorPackA" {
			require.Contains(t, sv["anchor_note"], "Opening quote was 100% above other suppliers")
		}
	}
	// Live preview before close.
	require.Equal(t, false, view["baselines"].(map[string]any)["snapshot"])

	// A concedes far below its opening; B and C hold.
	counterQuote(t, secretA, "anc-a-c1-"+rfqID, quoteBySeller["AnchorPackA"], 800)
	counterQuote(t, secretB, "anc-b-c1-"+rfqID, quoteBySeller["AnchorPackB"], 1000)
	counterQuote(t, secretC, "anc-c-c1-"+rfqID, quoteBySeller["AnchorPackC"], 1050)

	st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/run", buyerToken, map[string]any{})
	require.Equal(t, http.StatusOK, st)
	strat := view["strategy"].(map[string]any)
	require.Equal(t, "completed", strat["status"])
	require.Equal(t, quoteBySeller["AnchorPackA"], strat["outcome_quote_id"])
	require.Contains(t, strat["outcome_summary"], "saving USD 250 vs the typical market quote")

	b := view["baselines"].(map[string]any)
	require.Equal(t, true, b["snapshot"])
	require.Equal(t, "close", b["source"])
	require.EqualValues(t, 200000, b["opening_cents"])
	require.EqualValues(t, 105000, b["market_cents"], "lower median of 1000/1050/2000")
	require.Equal(t, "market", b["verified_basis"])
	require.EqualValues(t, 25000, b["verified_savings_cents"])
	require.EqualValues(t, 120000, b["opening_savings_cents"])
	anchor := b["anchor"].(map[string]any)
	require.Equal(t, true, anchor["flagged"])
	require.Contains(t, b["reason"], "savings measured against market price instead")
	summary := view["summary"].(map[string]any)
	require.EqualValues(t, 25000, summary["verified_savings_cents"])
	require.Equal(t, "market", summary["verified_basis"])

	st, savings := doJSON(t, api, "GET", "/v1/negotiations/savings", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	// Backward compatible: totals stay opening-quote based.
	totals := savings["totals"].(map[string]any)
	require.EqualValues(t, 120000, totals["savings_cents"])
	verified := savings["verified"].(map[string]any)
	require.Equal(t, "USD", verified["currency"])
	require.EqualValues(t, 1, verified["deals"])
	require.EqualValues(t, 80000, verified["spend_cents"])
	require.EqualValues(t, 25000, verified["savings_cents"], "the inflated opening must not inflate verified savings")
	require.EqualValues(t, 120000, verified["opening_quote_savings_cents"])
	require.EqualValues(t, 1, verified["anchored_deals"])
	require.EqualValues(t, 1, verified["snapshot_deals"])
	recent := savings["recent_deals"].([]any)[0].(map[string]any)
	require.EqualValues(t, 25000, recent["verified_savings_cents"])
	require.Equal(t, true, recent["anchored"])
	require.EqualValues(t, 200000, recent["initial_cents"])

	// The snapshot does not drift: a later, much cheaper quote version on a
	// rival changes nothing that was reported.
	_, err := poolOrSkip(t).Exec(context.Background(), `UPDATE quote_versions SET price_cents = 10000
		WHERE quote_id = $1 AND version = 1`, quoteBySeller["AnchorPackB"])
	require.NoError(t, err)
	st, savings = doJSON(t, api, "GET", "/v1/negotiations/savings", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.EqualValues(t, 25000, savings["verified"].(map[string]any)["savings_cents"])

	st, list := doJSON(t, api, "GET", "/v1/negotiations", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	item := list["negotiations"].([]any)[0].(map[string]any)
	require.EqualValues(t, 25000, item["verified_savings_cents"])
	require.Equal(t, true, item["anchored"])
}

// TestE2E11_HistoricalBaselineCapsSavings: the buyer paid less for the same
// cartons last month, so although the agent got 10% off the opening quote,
// verified savings are zero against the buyer's own price history.
func TestE2E11_HistoricalBaselineCapsSavings(t *testing.T) {
	api := buildAPI(t)
	_, sellerCo, secret := registerSeller(t, api, "HistPack")
	buyerToken, buyerCo, rfqID := negotiationBuyerRFQ(t, "HistBuyer")
	ensureMatched(t, rfqID, sellerCo)

	// A prior confirmed purchase of the same item at USD 850 for 500 cartons.
	ctx := context.Background()
	pool := poolOrSkip(t)
	var pastRFQ, pastQuote string
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO rfqs (buyer_company_id, created_by, title, raw_text, requirements, category, status)
		SELECT buyer_company_id, created_by, title, raw_text, requirements, category, 'closed' FROM rfqs WHERE id = $1
		RETURNING id`, rfqID).Scan(&pastRFQ))
	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO quotes (rfq_id, seller_company_id, price_cents, currency, status)
		VALUES ($1, $2, 85000, 'USD', 'accepted') RETURNING id`, pastRFQ, sellerCo).Scan(&pastQuote))
	_, err := pool.Exec(ctx, `INSERT INTO orders (rfq_id, quote_id, buyer_company_id, seller_company_id, total_cents, currency, status, created_at)
		VALUES ($1, $2, $3, $4, 85000, 'USD', 'confirmed', NOW() - INTERVAL '30 days')`, pastRFQ, pastQuote, buyerCo, sellerCo)
	require.NoError(t, err)

	submitQuote(t, secret, "hist-submit-"+rfqID, rfqID, 1000)
	st, view := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 100000, "max_rounds": 1,
	})
	require.Equal(t, http.StatusOK, st)
	quoteID := view["suppliers"].([]any)[0].(map[string]any)["quote_id"].(string)
	counterQuote(t, secret, "hist-c1-"+rfqID, quoteID, 900)

	st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/run", buyerToken, map[string]any{})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "awaiting_approval", view["strategy"].(map[string]any)["status"])
	b := view["baselines"].(map[string]any)
	require.Equal(t, true, b["snapshot"])
	require.EqualValues(t, 85000, b["historical_cents"])
	require.Equal(t, "historical", b["verified_basis"])
	require.EqualValues(t, 0, b["verified_savings_cents"])
	require.EqualValues(t, 10000, b["opening_savings_cents"])
	require.NotContains(t, view["strategy"].(map[string]any)["outcome_summary"], "saving")

	st, _ = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{})
	require.Equal(t, http.StatusCreated, st)
	st, savings := doJSON(t, api, "GET", "/v1/negotiations/savings", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	verified := savings["verified"].(map[string]any)
	require.EqualValues(t, 0, verified["savings_cents"])
	require.EqualValues(t, 10000, verified["opening_quote_savings_cents"])
	require.EqualValues(t, 1, verified["by_basis"].(map[string]any)["historical"])
}

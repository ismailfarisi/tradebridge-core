package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func makePro(t *testing.T, companyID string) {
	t.Helper()
	_, err := poolOrSkip(t).Exec(context.Background(), `INSERT INTO subscriptions (company_id, plan, status)
		VALUES ($1, 'pro', 'active') ON CONFLICT (company_id) DO UPDATE SET plan = 'pro', status = 'active'`, companyID)
	require.NoError(t, err)
}

func negotiationBuyerRFQ(t *testing.T, name string) (token, companyID, rfqID string) {
	t.Helper()
	api := buildAPI(t)
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": name, "company_type": "buyer", "email": uniqueEmail("neg-buyer-"), "password": "password123",
	})
	require.Equal(t, http.StatusCreated, st)
	verifyUser(t, poolOrSkip(t), buyer["user_id"].(string))
	token = buyer["token"].(string)
	st, created := doJSON(t, api, "POST", "/v1/rfqs", token, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	return token, buyer["company_id"].(string), created["rfq"].(map[string]any)["id"].(string)
}

// ensureMatched pins the seller to the RFQ. Matching caps each needle at
// maxMatches, and the shared test DB accumulates many "packaging" sellers, so
// a freshly registered seller is not guaranteed to make the cut.
func ensureMatched(t *testing.T, rfqID, sellerCompanyID string) {
	t.Helper()
	_, err := poolOrSkip(t).Exec(context.Background(), `INSERT INTO rfq_matches (rfq_id, seller_company_id, score, reason)
		VALUES ($1, $2, 1, 'test') ON CONFLICT (rfq_id, seller_company_id) DO NOTHING`, rfqID, sellerCompanyID)
	require.NoError(t, err)
}

func submitQuote(t *testing.T, secret, key, rfqID string, price float64) {
	t.Helper()
	st, _ := doJSON(t, buildGateway(t), "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": key, "rfq_id": rfqID,
		"payload": map[string]any{"price": price, "currency": "USD", "delivery_days": 10},
	})
	require.Equal(t, http.StatusCreated, st)
}

func counterQuote(t *testing.T, secret, key, quoteID string, price float64) {
	t.Helper()
	st, _ := doJSON(t, buildGateway(t), "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.counter", "idempotency_key": key,
		"payload": map[string]any{"quote_id": quoteID, "price": price, "currency": "USD", "delivery_days": 10},
	})
	require.Equal(t, http.StatusCreated, st)
}

// TestE2E11_AutonomousNegotiationAutoAccept: two suppliers quote, the buyer's
// agent counters both within the mandate, one supplier concedes and one holds
// above the walk-away price; the agent walks away from the holder and
// auto-accepts the concession, producing an order at the negotiated price.
func TestE2E11_AutonomousNegotiationAutoAccept(t *testing.T) {
	api := buildAPI(t)
	_, coA, secretA := registerSeller(t, api, "NegPackA")
	_, coB, secretB := registerSeller(t, api, "NegPackB")
	buyerToken, buyerCo, rfqID := negotiationBuyerRFQ(t, "NegBuyer")
	ensureMatched(t, rfqID, coA)
	ensureMatched(t, rfqID, coB)

	submitQuote(t, secretA, "neg-a-submit-"+rfqID, rfqID, 1000)
	submitQuote(t, secretB, "neg-b-submit-"+rfqID, rfqID, 1100)

	strategy := map[string]any{
		"target_price_cents": 85000, "max_price_cents": 100000, "max_delivery_days": 14,
		"max_rounds": 2, "auto_accept": true, "tone": "collaborative",
	}
	// Auto-accept is a paid capability.
	st, _ := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, strategy)
	require.Equal(t, http.StatusPaymentRequired, st)
	makePro(t, buyerCo)

	st, view := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, strategy)
	require.Equal(t, http.StatusOK, st)
	rounds := view["rounds"].([]any)
	require.Len(t, rounds, 2)
	quoteBySeller := map[string]string{}
	for _, s := range view["suppliers"].([]any) {
		sv := s.(map[string]any)
		quoteBySeller[sv["seller_name"].(string)] = sv["quote_id"].(string)
	}
	for _, r := range rounds {
		rv := r.(map[string]any)
		require.Equal(t, "counter", rv["action"])
		require.EqualValues(t, 85000, rv["offer_price_cents"], "first counter anchors at target")
	}

	// Tenant isolation: another buyer cannot see it.
	otherToken, _, _ := negotiationBuyerRFQ(t, "NegOther")
	st, _ = doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/negotiation", otherToken, nil)
	require.Equal(t, http.StatusNotFound, st)

	counterQuote(t, secretA, "neg-a-c1-"+rfqID, quoteBySeller["NegPackA"], 920)
	counterQuote(t, secretB, "neg-b-c1-"+rfqID, quoteBySeller["NegPackB"], 1100)

	st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/run", buyerToken, map[string]any{})
	require.Equal(t, http.StatusOK, st)
	s := view["strategy"].(map[string]any)
	require.Equal(t, "completed", s["status"])
	require.Equal(t, quoteBySeller["NegPackA"], s["outcome_quote_id"])
	actions := map[string]string{}
	for _, r := range view["rounds"].([]any) {
		rv := r.(map[string]any)
		actions[rv["seller_name"].(string)] = rv["action"].(string)
	}
	require.Equal(t, "accept_ready", actions["NegPackA"])
	require.Equal(t, "walk_away", actions["NegPackB"])
	require.EqualValues(t, "closed", view["rfq"].(map[string]any)["status"])

	st, savings := doJSON(t, api, "GET", "/v1/negotiations/savings", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	totals := savings["totals"].(map[string]any)
	require.EqualValues(t, 1, totals["deals"])
	require.EqualValues(t, 92000, totals["spend_cents"])
	require.EqualValues(t, 8000, totals["savings_cents"])

	st, list := doJSON(t, api, "GET", "/v1/negotiations", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Len(t, list["negotiations"].([]any), 1)
}

// TestE2E11_NegotiationApprovalFlow: without auto-accept, a quote at target
// is held for the buyer, who approves it into an order.
func TestE2E11_NegotiationApprovalFlow(t *testing.T) {
	api := buildAPI(t)
	_, sellerCo, secret := registerSeller(t, api, "NegApprovePack")
	buyerToken, _, rfqID := negotiationBuyerRFQ(t, "NegApproveBuyer")
	ensureMatched(t, rfqID, sellerCo)
	submitQuote(t, secret, "neg-ap-submit-"+rfqID, rfqID, 800)

	st, view := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Nil(t, view["strategy"])
	require.NotNil(t, view["suggested"])

	st, _ = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{})
	require.Equal(t, http.StatusNotFound, st)

	st, _ = doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, map[string]any{
		"target_price_cents": 1, "max_price_cents": 0,
	})
	require.Equal(t, http.StatusBadRequest, st)

	st, view = doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 95000,
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "awaiting_approval", view["strategy"].(map[string]any)["status"])

	st, order := doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{})
	require.Equal(t, http.StatusCreated, st)
	require.EqualValues(t, 80000, order["total_cents"])

	st, _ = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{})
	require.Equal(t, http.StatusConflict, st)
}

func TestE2E11_NegotiationAwardAnotherSupplier(t *testing.T) {
	api := buildAPI(t)
	_, cheapCo, cheapSecret := registerSeller(t, api, "NegAwardCheap")
	_, fastCo, fastSecret := registerSeller(t, api, "NegAwardFast")
	buyerToken, _, rfqID := negotiationBuyerRFQ(t, "NegAwardBuyer")
	ensureMatched(t, rfqID, cheapCo)
	ensureMatched(t, rfqID, fastCo)
	submitQuote(t, cheapSecret, "neg-aw-cheap-"+rfqID, rfqID, 800)
	submitQuote(t, fastSecret, "neg-aw-fast-"+rfqID, rfqID, 820)

	st, view := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 95000,
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "awaiting_approval", view["strategy"].(map[string]any)["status"])

	var other string
	for _, s := range view["suppliers"].([]any) {
		sup := s.(map[string]any)
		if sup["recommended"] != true {
			other = sup["quote_id"].(string)
		}
	}
	require.NotEmpty(t, other)

	st, _ = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{"quote_id": "01NOTAQUOTE"})
	require.Equal(t, http.StatusNotFound, st)

	st, order := doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{"quote_id": other})
	require.Equal(t, http.StatusCreated, st)
	require.EqualValues(t, 82000, order["total_cents"])

	st, view = doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	strat := view["strategy"].(map[string]any)
	require.Equal(t, "completed", strat["status"])
	require.Contains(t, strat["outcome_summary"], "over the agent's pick")

	// Savings are measured from the cheapest opening quote on the RFQ, not the
	// awarded supplier's own opening, so paying more for speed saves nothing.
	st, savings := doJSON(t, api, "GET", "/v1/negotiations/savings", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.EqualValues(t, 0, savings["totals"].(map[string]any)["savings_cents"])
	deal := savings["recent_deals"].([]any)[0].(map[string]any)
	require.EqualValues(t, 80000, deal["initial_cents"])
}

func TestE2E11_NegotiationDecline(t *testing.T) {
	api := buildAPI(t)
	_, sellerCo, secret := registerSeller(t, api, "NegDeclinePack")
	buyerToken, _, rfqID := negotiationBuyerRFQ(t, "NegDeclineBuyer")
	ensureMatched(t, rfqID, sellerCo)
	submitQuote(t, secret, "neg-dc-submit-"+rfqID, rfqID, 800)

	st, _ := doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/decline", buyerToken, nil)
	require.Equal(t, http.StatusNotFound, st)

	st, view := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 95000,
	})
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "awaiting_approval", view["strategy"].(map[string]any)["status"])

	st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/decline", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	require.Equal(t, "cancelled", view["strategy"].(map[string]any)["status"])
	require.NotEqual(t, "closed", view["rfq"].(map[string]any)["status"])

	st, _ = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{})
	require.Equal(t, http.StatusConflict, st)
	st, _ = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/decline", buyerToken, nil)
	require.Equal(t, http.StatusConflict, st)
}

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func agentCommand(t *testing.T, secret string, body map[string]any) {
	t.Helper()
	st, out := doJSON(t, buildGateway(t), "POST", "/agent/v1/commands", secret, body)
	require.Equal(t, http.StatusCreated, st, "%v", out)
}

// lastRound returns the newest round for a seller in a negotiation view.
func lastRound(view map[string]any, seller string) map[string]any {
	var last map[string]any
	for _, r := range view["rounds"].([]any) {
		rv := r.(map[string]any)
		if rv["seller_name"] == seller {
			last = rv
		}
	}
	return last
}

// TestE2E12_PaymentTermsLogroll: a buyer mandate that prefers net 60 and
// accepts no less than net 15. Supplier A holds its price, so the agent
// logrolls — offering faster payment for a lower price — and A meets it on
// net 45. Supplier B is cheaper but only sells for cash and never moves, so
// it is never accepted. The deal held for approval is A's, on net 45.
func TestE2E12_PaymentTermsLogroll(t *testing.T) {
	api := buildAPI(t)
	_, coA, secretA := registerSeller(t, api, "TermsPackA")
	_, coB, secretB := registerSeller(t, api, "TermsCashB")
	buyerToken, _, rfqID := negotiationBuyerRFQ(t, "TermsBuyer")
	ensureMatched(t, rfqID, coA)
	ensureMatched(t, rfqID, coB)
	makePro(t, coA) // agent autonomy: counters reach A's agent as message.received

	agentCommand(t, secretA, map[string]any{"type": "quote.submit", "idempotency_key": "terms-a-submit-" + rfqID, "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 10, "payment_terms_days": 0}})
	agentCommand(t, secretB, map[string]any{"type": "quote.submit", "idempotency_key": "terms-b-submit-" + rfqID, "rfq_id": rfqID,
		"payload": map[string]any{"price": 880, "currency": "USD", "delivery_days": 10, "payment_terms_days": 0}})

	// Invalid mandates are rejected.
	for _, bad := range []map[string]any{
		{"target_price_cents": 85000, "max_price_cents": 100000, "min_payment_terms_days": 15},
		{"target_price_cents": 85000, "max_price_cents": 100000, "preferred_payment_terms_days": 30, "min_payment_terms_days": 45},
		{"target_price_cents": 85000, "max_price_cents": 100000, "preferred_payment_terms_days": 30, "price_weight": 0.8, "terms_weight": 0.3},
		{"target_price_cents": 85000, "max_price_cents": 100000, "preferred_payment_terms_days": 400},
	} {
		st, _ := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, bad)
		require.Equal(t, http.StatusBadRequest, st, "%v", bad)
	}

	st, view := doJSON(t, api, "PUT", "/v1/rfqs/"+rfqID+"/negotiation", buyerToken, map[string]any{
		"target_price_cents": 85000, "max_price_cents": 100000, "max_delivery_days": 14, "price_weight": 0.7,
		"preferred_payment_terms_days": 60, "min_payment_terms_days": 15, "terms_weight": 0.1,
		"max_rounds": 4, "tone": "collaborative",
	})
	require.Equal(t, http.StatusOK, st)
	strat := view["strategy"].(map[string]any)
	require.EqualValues(t, 60, strat["preferred_payment_terms_days"])
	require.EqualValues(t, 15, strat["min_payment_terms_days"])
	w := view["weights"].(map[string]any)
	require.InDelta(t, 0.7, w["price"], 1e-9)
	require.InDelta(t, 0.2, w["delivery"], 1e-9)
	require.InDelta(t, 0.1, w["terms"], 1e-9)

	quoteBySeller := map[string]string{}
	for _, s := range view["suppliers"].([]any) {
		sv := s.(map[string]any)
		quoteBySeller[sv["seller_name"].(string)] = sv["quote_id"].(string)
		require.EqualValues(t, 0, sv["payment_terms_days"])
	}
	for _, name := range []string{"TermsPackA", "TermsCashB"} {
		r := lastRound(view, name)
		require.Equal(t, "counter", r["action"])
		require.EqualValues(t, 85000, r["offer_price_cents"])
		require.EqualValues(t, 60, r["offer_payment_terms_days"], "first counter asks for the preferred terms")
		require.EqualValues(t, 0, r["seller_payment_terms_days"])
		require.Equal(t, "template", r["message_source"])
		require.Contains(t, r["message"], "payment within 60 days")
	}

	// The proposed terms reach A's agent in message.received.
	var payload []byte
	require.NoError(t, poolOrSkip(t).QueryRow(context.Background(),
		`SELECT payload FROM agent_events WHERE company_id=$1 AND type='message.received' AND payload->>'quote_id'=$2 ORDER BY seq DESC LIMIT 1`,
		coA, quoteBySeller["TermsPackA"]).Scan(&payload))
	var ev struct {
		Counter struct {
			PriceCents       int64  `json:"price_cents"`
			PaymentTermsDays *int32 `json:"payment_terms_days"`
		} `json:"counter"`
	}
	require.NoError(t, json.Unmarshal(payload, &ev))
	require.EqualValues(t, 85000, ev.Counter.PriceCents)
	require.NotNil(t, ev.Counter.PaymentTermsDays)
	require.EqualValues(t, 60, *ev.Counter.PaymentTermsDays)

	// A holds its price and offers net 30; B holds at cash.
	agentCommand(t, secretA, map[string]any{"type": "quote.counter", "idempotency_key": "terms-a-c1-" + rfqID,
		"payload": map[string]any{"quote_id": quoteBySeller["TermsPackA"], "price": 1000, "currency": "USD", "delivery_days": 10, "payment_terms_days": 30}})
	agentCommand(t, secretB, map[string]any{"type": "quote.counter", "idempotency_key": "terms-b-c1-" + rfqID,
		"payload": map[string]any{"quote_id": quoteBySeller["TermsCashB"], "price": 880, "currency": "USD", "delivery_days": 10, "payment_terms_days": 0}})

	st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/run", buyerToken, map[string]any{})
	require.Equal(t, http.StatusOK, st)
	ra := lastRound(view, "TermsPackA")
	require.Equal(t, "counter", ra["action"], "a held price is logrolled, not ended")
	require.EqualValues(t, 45, ra["offer_payment_terms_days"], "faster payment: net 60 -> net 45 (A asked net 30)")
	require.Less(t, ra["offer_price_cents"].(float64), float64(90000), "lower than the straight round-2 concession")
	require.GreaterOrEqual(t, ra["offer_price_cents"].(float64), float64(85000))
	require.Contains(t, ra["rationale"], "Trade-off: payment terms matter less to you (10%) than price (70%)")
	require.Contains(t, ra["message"], "net 45 days instead of net 60 days")

	// A meets the trade on net 45.
	agentCommand(t, secretA, map[string]any{"type": "quote.counter", "idempotency_key": "terms-a-c2-" + rfqID,
		"payload": map[string]any{"quote_id": quoteBySeller["TermsPackA"], "price": 940, "currency": "USD", "delivery_days": 10, "payment_terms_days": 45}})

	// B keeps holding at cash until the agent stops countering it.
	for i := 2; i < 8; i++ {
		if lastRound(view, "TermsCashB")["action"] != "counter" {
			break
		}
		agentCommand(t, secretB, map[string]any{"type": "quote.counter", "idempotency_key": "terms-b-c" + string(rune('0'+i)) + "-" + rfqID,
			"payload": map[string]any{"quote_id": quoteBySeller["TermsCashB"], "price": 880, "currency": "USD", "delivery_days": 10, "payment_terms_days": 0}})
		st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/run", buyerToken, map[string]any{})
		require.Equal(t, http.StatusOK, st)
		if rb := lastRound(view, "TermsCashB"); rb["action"] == "counter" {
			require.GreaterOrEqual(t, rb["offer_payment_terms_days"].(float64), float64(15), "never counters below the minimum terms")
		}
	}
	st, view = doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/run", buyerToken, map[string]any{})
	require.Equal(t, http.StatusOK, st)

	require.Equal(t, "accept_ready", lastRound(view, "TermsPackA")["action"])
	rb := lastRound(view, "TermsCashB")
	require.Equal(t, "walk_away", rb["action"], "cash only is below the net-15 minimum, however cheap")
	require.Contains(t, rb["rationale"], "shorter than your minimum of net 15 days")

	strat = view["strategy"].(map[string]any)
	require.Equal(t, "awaiting_approval", strat["status"])
	require.Equal(t, quoteBySeller["TermsPackA"], strat["outcome_quote_id"])
	require.Contains(t, strat["outcome_summary"], "USD 940 on net 45 days")

	st, quotes := doJSON(t, api, "GET", "/v1/rfqs/"+rfqID+"/quotes", buyerToken, nil)
	require.Equal(t, http.StatusOK, st)
	for _, q := range quotes["quotes"].([]any) {
		qv := q.(map[string]any)
		if qv["id"] == quoteBySeller["TermsPackA"] {
			require.EqualValues(t, 45, qv["payment_terms_days"])
		}
	}

	st, order := doJSON(t, api, "POST", "/v1/rfqs/"+rfqID+"/negotiation/approve", buyerToken, map[string]any{})
	require.Equal(t, http.StatusCreated, st)
	require.EqualValues(t, 94000, order["total_cents"])
}

// TestE2E12_SellerManualQuoteWithTerms: a seller quoting by hand states
// payment terms, sees them on the RFQ page, and a revision without terms
// keeps them.
func TestE2E12_SellerManualQuoteWithTerms(t *testing.T) {
	api := buildAPI(t)
	sellerToken, sellerCo, sellerUser := seedSellerWithCatalog(t, api, sellerTestCategory)
	verifyUser(t, poolOrSkip(t), sellerUser)
	_, _, rfqID := negotiationBuyerRFQ(t, "TermsManualBuyer")
	ensureMatched(t, rfqID, sellerCo)

	st, out := doJSON(t, api, "POST", "/v1/seller/rfqs/"+rfqID+"/quote", sellerToken, map[string]any{
		"price": 1200, "currency": "AED", "delivery_days": 7, "payment_terms_days": 400,
	})
	require.Equal(t, http.StatusBadRequest, st, "%v", out)

	st, out = doJSON(t, api, "POST", "/v1/seller/rfqs/"+rfqID+"/quote", sellerToken, map[string]any{
		"price": 1200, "currency": "AED", "delivery_days": 7, "payment_terms_days": 30,
	})
	require.Equal(t, http.StatusCreated, st, "%v", out)
	require.EqualValues(t, 30, out["quote"].(map[string]any)["payment_terms_days"])

	st, out = doJSON(t, api, "POST", "/v1/seller/rfqs/"+rfqID+"/quote", sellerToken, map[string]any{
		"price": 1150, "currency": "AED", "delivery_days": 7,
	})
	require.Equal(t, http.StatusOK, st, "%v", out)

	st, view := doJSON(t, api, "GET", "/v1/seller/rfqs/"+rfqID, sellerToken, nil)
	require.Equal(t, http.StatusOK, st)
	q := view["rfq"].(map[string]any)["quote"].(map[string]any)
	require.EqualValues(t, 115000, q["price_cents"])
	require.EqualValues(t, 30, q["payment_terms_days"], "a revision without terms keeps them")
}

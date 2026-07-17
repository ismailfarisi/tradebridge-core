package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// TestGateway_QuoteCounter_BumpsHeadAndNotifiesBuyer verifies the full
// counter-offer loop through the gateway: the matched seller's agent sends
// quote.counter for its own quote (201), the quote head is bumped to a new
// version with a corresponding immutable 'counter' version row, and the
// buyer is notified. It also verifies an agent that isn't the quote's own
// seller — even if it IS matched to the RFQ via its own quote — is
// forbidden from countering someone else's quote.
func TestGateway_QuoteCounter_BumpsHeadAndNotifiesBuyer(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	_, sellerACompany, secretA := registerSeller(t, api, "CounterPackA")
	_, sellerBCompany, secretB := registerSeller(t, api, "CounterPackB")

	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "CounterBuyer", "company_type": "buyer",
		"email": uniqueEmail("ctrbuyer-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	buyerUserID := buyer["user_id"].(string)
	verifyUser(t, pool, buyerUserID)

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	// Both sellers submit an initial quote (version 1).
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretA, map[string]any{
		"type": "quote.submit", "idempotency_key": "cA1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD", "delivery_days": 10},
	})
	require.Equal(t, http.StatusCreated, st)
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretB, map[string]any{
		"type": "quote.submit", "idempotency_key": "cB1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1500, "currency": "USD", "delivery_days": 7},
	})
	require.Equal(t, http.StatusCreated, st)

	var quoteAID, quoteBID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM quotes WHERE rfq_id=$1 AND seller_company_id=$2`, rfqID, sellerACompany).Scan(&quoteAID))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM quotes WHERE rfq_id=$1 AND seller_company_id=$2`, rfqID, sellerBCompany).Scan(&quoteBID))

	// --- Authorization: seller B (matched to the RFQ via its OWN quote)
	// cannot counter seller A's quote. ---
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretB, map[string]any{
		"type": "quote.counter", "idempotency_key": "cB-bad", "payload": map[string]any{
			"quote_id": quoteAID, "price": 1,
		},
	})
	require.Equal(t, http.StatusForbidden, st, "an agent may not counter a quote it doesn't own")

	// --- A totally unmatched agent cannot counter either. ---
	_, _, secretC := registerSeller(t, api, "UnrelatedPackC")
	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secretC, map[string]any{
		"type": "quote.counter", "idempotency_key": "cC-bad", "payload": map[string]any{
			"quote_id": quoteAID, "price": 1,
		},
	})
	require.Equal(t, http.StatusForbidden, st)

	// --- Seller A's own agent counters its own quote: 201. ---
	st, cmd := doJSON(t, gw, "POST", "/agent/v1/commands", secretA, map[string]any{
		"type": "quote.counter", "idempotency_key": "cA-counter", "payload": map[string]any{
			"quote_id": quoteAID, "price": 900, "currency": "USD", "delivery_days": 8, "notes": "best I can do",
		},
	})
	require.Equal(t, http.StatusCreated, st)
	require.NotEmpty(t, cmd["id"])

	// Head is bumped: version 2, new price.
	var headVersion int32
	var headPriceCents int64
	var headStatus string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT version, price_cents, status FROM quotes WHERE id=$1`, quoteAID).Scan(&headVersion, &headPriceCents, &headStatus))
	require.Equal(t, int32(2), headVersion)
	require.Equal(t, int64(90000), headPriceCents, "price normalized to cents")
	require.Equal(t, "submitted", headStatus)

	// Immutable history has both versions.
	versions, err := q.ListQuoteVersions(ctx, quoteAID)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, "seller", versions[0].Source)
	require.Equal(t, "counter", versions[1].Source)
	require.Equal(t, int64(90000), versions[1].PriceCents)

	// Seller B's quote is untouched (still version 1).
	var bVersion int32
	require.NoError(t, pool.QueryRow(ctx, `SELECT version FROM quotes WHERE id=$1`, quoteBID).Scan(&bVersion))
	require.Equal(t, int32(1), bVersion)

	// The buyer is notified of the counter-offer.
	require.Eventually(t, func() bool {
		c, _ := q.CountUnreadNotifications(ctx, buyerUserID)
		return c >= 1
	}, 3*time.Second, 25*time.Millisecond)
	rows, err := q.ListNotificationsByUser(ctx, sqlc.ListNotificationsByUserParams{UserID: buyerUserID, Limit: 5})
	require.NoError(t, err)
	var foundCountered bool
	for _, r := range rows {
		if r.Type == "quote.countered" {
			foundCountered = true
		}
	}
	require.True(t, foundCountered, "buyer receives a quote.countered notification")

	// The seller's counter is also threaded as a quote_messages row.
	var seller string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT author FROM quote_messages WHERE quote_id=$1 AND body LIKE '%best I can do%'`, quoteAID).Scan(&seller))
	require.Equal(t, "seller", seller)

	// Audit log recorded the command with the derived rfq_id.
	var audits int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_logs WHERE company_id=$1 AND action='command.quote.counter' AND detail->>'rfq_id'=$2`,
		sellerACompany, rfqID).Scan(&audits))
	require.Equal(t, 1, audits)
}

// TestQuotes_BuyerCounter_MessageReceivedGatedOnAutonomy verifies the buyer
// counter endpoint POST /v1/quotes/:id/counter: it always threads a message,
// and only emits a durable message.received agent event to the seller when
// the seller's plan grants agent autonomy. A non-buyer caller gets 404.
func TestQuotes_BuyerCounter_MessageReceivedGatedOnAutonomy(t *testing.T) {
	api := buildAPI(t)
	gw := buildGateway(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	sellerToken, sellerCompany, secret := registerSeller(t, api, "AutonomyPack")
	st, buyer := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AutonomyBuyer", "company_type": "buyer",
		"email": uniqueEmail("autobuyer-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := buyer["token"].(string)
	verifyUser(t, pool, buyer["user_id"].(string))

	st, created := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Cartons", "text": "Need 500 cartons for packaging, deliver to Dubai.",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := created["rfq"].(map[string]any)["id"].(string)

	st, _ = doJSON(t, gw, "POST", "/agent/v1/commands", secret, map[string]any{
		"type": "quote.submit", "idempotency_key": "auto1", "rfq_id": rfqID,
		"payload": map[string]any{"price": 1000, "currency": "USD"},
	})
	require.Equal(t, http.StatusCreated, st)

	var quoteID string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM quotes WHERE rfq_id=$1 AND seller_company_id=$2`, rfqID, sellerCompany).Scan(&quoteID))

	// --- No autonomy yet (Free plan): counter records a message, no agent event. ---
	st, _ = doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/counter", buyerToken, map[string]any{
		"price_cents": 900, "message": "can you do 900?",
	})
	require.Equal(t, http.StatusOK, st)

	var noAutoEvents int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='message.received'`, sellerCompany).Scan(&noAutoEvents))
	require.Equal(t, 0, noAutoEvents, "no message.received while the seller lacks agent autonomy")

	// --- A non-buyer (the seller itself) cannot counter: 404. ---
	st, _ = doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/counter", sellerToken, map[string]any{
		"message": "sneaky",
	})
	require.Equal(t, http.StatusNotFound, st)

	// --- Grant the seller agent autonomy (Pro plan) directly, then counter again. ---
	_, err := q.UpsertSubscription(ctx, sqlc.UpsertSubscriptionParams{
		CompanyID: sellerCompany, Plan: "pro", Interval: "month", Status: "active",
	})
	require.NoError(t, err)

	st, _ = doJSON(t, api, "POST", "/v1/quotes/"+quoteID+"/counter", buyerToken, map[string]any{
		"price_cents": 850, "message": "final offer",
	})
	require.Equal(t, http.StatusOK, st)

	var autoEvents int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_events WHERE company_id=$1 AND type='message.received' AND payload->>'quote_id'=$2`,
		sellerCompany, quoteID).Scan(&autoEvents))
	require.Equal(t, 1, autoEvents, "seller with agent autonomy receives message.received")
}

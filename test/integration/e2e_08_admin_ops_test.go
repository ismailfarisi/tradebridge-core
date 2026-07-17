package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #8 (Sprint 8): a platform admin runs the full operations playbook on a
// single company — suspend, unsuspend, verify, resolve its dispute, and
// impersonate it (read-only) — entirely through the real HTTP API, and every
// one of those actions shows up in the admin audit trail.
func TestE2E08_AdminOpsAllAudited(t *testing.T) {
	adminEmail := uniqueEmail("padmin-e2e8-")
	const adminPass = "admin-super-secret-e2e8"
	api := buildAPIWithAdmin(t, adminEmail, adminPass)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// --- Seed: a seller with a matching catalog item, and the buyer company
	// under test (this is the company we'll suspend/verify/dispute/impersonate). ---
	sellerToken, sellerCompanyID, _ := seedSellerWithCatalog(t, api, "packaging")

	buyerEmail := uniqueEmail("e2e8-buyer-")
	const buyerPassword = "password123"
	st, reg := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E8 Buyer", "company_type": "buyer",
		"email": buyerEmail, "password": buyerPassword,
	})
	require.Equal(t, http.StatusCreated, st)
	buyerToken := reg["token"].(string)
	buyerCompanyID := reg["company_id"].(string)
	buyerUserID := reg["user_id"].(string)
	verifyUser(t, pool, buyerUserID)

	// A pending domain verification for the buyer (owner + verified email
	// required by the write guard on /v1/verification/domain).
	st, _ = doJSON(t, api, "POST", "/v1/verification/domain", buyerToken, map[string]any{
		"domain": "e2e8buyer.example",
	})
	require.Equal(t, http.StatusOK, st)
	v, err := q.GetCompanyVerification(ctx, buyerCompanyID)
	require.NoError(t, err)
	require.Equal(t, "pending", v.VerificationStatus)

	// Buyer posts an RFQ; the seller's quote is seeded directly (quote
	// submission itself is covered by the A-series/E2E04), then the buyer
	// accepts it, producing an order.
	_ = sellerToken
	st, rfqResp := doJSON(t, api, "POST", "/v1/rfqs", buyerToken, map[string]any{
		"title": "Need cartons", "text": "500 cartons for packaging",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := rfqResp["rfq"].(map[string]any)["id"].(string)

	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqID, SellerCompanyID: sellerCompanyID,
		PriceCents: 5000, Currency: "USD", Notes: "in stock", Completeness: 1,
	})
	require.NoError(t, err)

	st, accepted := doJSON(t, api, "POST", "/v1/quotes/"+quote.ID+"/accept", buyerToken, nil)
	require.Equal(t, http.StatusCreated, st)
	orderID := accepted["order_id"].(string)

	// The buyer raises an open dispute on its own order, so the dispute's
	// RaisedByCompanyID (the audit target for dispute.resolve) is the same
	// company_id we suspend/verify/impersonate below.
	st, disputeResp := doJSON(t, api, "POST", "/v1/orders/"+orderID+"/disputes", buyerToken, map[string]any{
		"reason": "not_delivered", "detail": "never arrived",
	})
	require.Equal(t, http.StatusOK, st)
	disputeID := disputeResp["dispute"].(map[string]any)["id"].(string)

	// --- Admin token ---
	st, loginResp := doJSON(t, api, "POST", "/admin/v1/login", "", map[string]any{
		"email": adminEmail, "password": adminPass,
	})
	require.Equal(t, http.StatusOK, st)
	adminToken := loginResp["token"].(string)

	// --- Suspend, then confirm a fresh login for the company is blocked. ---
	st, _ = doJSON(t, api, "POST", "/admin/v1/companies/"+buyerCompanyID+"/suspend", adminToken, map[string]any{
		"reason": "policy review",
	})
	require.Equal(t, http.StatusOK, st)

	st, _ = doJSON(t, api, "POST", "/v1/auth/login", "", map[string]any{
		"email": buyerEmail, "password": buyerPassword,
	})
	require.Equal(t, http.StatusForbidden, st, "login must be blocked while the company is suspended")

	// --- Unsuspend, then confirm login succeeds again (so the company is
	// ACTIVE for the impersonation step below). ---
	st, _ = doJSON(t, api, "POST", "/admin/v1/companies/"+buyerCompanyID+"/unsuspend", adminToken, nil)
	require.Equal(t, http.StatusOK, st)

	st, _ = doJSON(t, api, "POST", "/v1/auth/login", "", map[string]any{
		"email": buyerEmail, "password": buyerPassword,
	})
	require.Equal(t, http.StatusOK, st, "login must succeed again once unsuspended")

	// --- Approve the pending verification. ---
	st, _ = doJSON(t, api, "POST", "/admin/v1/companies/"+buyerCompanyID+"/verification", adminToken, map[string]any{
		"status": "verified",
	})
	require.Equal(t, http.StatusOK, st)

	// --- Resolve the dispute. ---
	st, _ = doJSON(t, api, "POST", "/admin/v1/disputes/"+disputeID+"/resolve", adminToken, map[string]any{
		"outcome": "resolved", "order_to": "completed", "resolution": "refund issued to buyer",
	})
	require.Equal(t, http.StatusOK, st)

	// --- Impersonate the company: a mutating call is blocked, a read call
	// succeeds. ---
	st, impResp := doJSON(t, api, "POST", "/admin/v1/impersonate", adminToken, map[string]any{
		"company_id": buyerCompanyID,
	})
	require.Equal(t, http.StatusOK, st)
	impToken := impResp["token"].(string)

	st, _ = doJSON(t, api, "POST", "/v1/rfqs", impToken, map[string]any{
		"title": "should be blocked", "text": "read-only impersonation session",
	})
	require.Equal(t, http.StatusForbidden, st, "a read-only impersonation session must not be able to mutate")

	st, _ = doJSON(t, api, "GET", "/v1/me", impToken, nil)
	require.Equal(t, http.StatusOK, st, "a read-only impersonation session must still be able to read")

	// --- Audit viewer: all five actions recorded against this company, each
	// actor prefixed "admin:". ---
	st, auditResp := doJSON(t, api, "GET", "/admin/v1/audit?company_id="+buyerCompanyID, adminToken, nil)
	require.Equal(t, http.StatusOK, st)
	entries, ok := auditResp["entries"].([]any)
	require.True(t, ok, "expected an entries array, got %v", auditResp)

	wantActions := map[string]bool{
		"company.suspend":      false,
		"company.unsuspend":    false,
		"company.verification": false,
		"dispute.resolve":      false,
		"support.impersonate":  false,
	}
	for _, e := range entries {
		m := e.(map[string]any)
		action, _ := m["action"].(string)
		if _, tracked := wantActions[action]; !tracked {
			continue
		}
		wantActions[action] = true
		actor, _ := m["actor"].(string)
		require.True(t, strings.HasPrefix(actor, "admin:"),
			"actor for action %q must be admin-prefixed, got %q", action, actor)
	}
	for action, found := range wantActions {
		require.True(t, found, "expected an audit entry for action %q", action)
	}
}

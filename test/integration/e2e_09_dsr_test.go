package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #9 (Sprint 9): a company owner exercises the full GDPR data-subject-
// rights flow through the real HTTP API — export the company's data
// footprint, then delete (soft-delete + anonymize) the account — and every
// downstream effect (audit trail, anonymized rows, retained transactional
// records, blocked re-auth) is verified against the real DB and API.
func TestE2E09_DataExportAndAccountDeletion(t *testing.T) {
	api := buildAPI(t)
	pool := poolOrSkip(t)
	q := sqlc.New(pool)
	ctx := context.Background()

	// --- Seed: an owner company with a seeded RFQ and a completed order,
	// plus a seller counterparty (mirrors the seed pattern in e2e_08). ---
	sellerToken, sellerCompanyID, _ := seedSellerWithCatalog(t, api, "hardware")
	_ = sellerToken

	ownerEmail := uniqueEmail("e2e9-owner-")
	const ownerPassword = "owner-secret-e2e9"
	st, reg := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E9 Owner Co", "company_type": "buyer",
		"email": ownerEmail, "password": ownerPassword,
	})
	require.Equal(t, http.StatusCreated, st)
	ownerToken := reg["token"].(string)
	ownerCompanyID := reg["company_id"].(string)
	ownerUserID := reg["user_id"].(string)
	verifyUser(t, pool, ownerUserID)

	st, rfqResp := doJSON(t, api, "POST", "/v1/rfqs", ownerToken, map[string]any{
		"title": "Need fasteners", "text": "10000 M6 bolts, hardware",
	})
	require.Equal(t, http.StatusCreated, st)
	rfqID := rfqResp["rfq"].(map[string]any)["id"].(string)

	quote, err := q.UpsertQuote(ctx, sqlc.UpsertQuoteParams{
		RfqID: rfqID, SellerCompanyID: sellerCompanyID,
		PriceCents: 12000, Currency: "USD", Notes: "in stock", Completeness: 1,
	})
	require.NoError(t, err)

	st, accepted := doJSON(t, api, "POST", "/v1/quotes/"+quote.ID+"/accept", ownerToken, nil)
	require.Equal(t, http.StatusCreated, st)
	orderID := accepted["order_id"].(string)

	// --- Step 2: export, and assert no password hash ever leaves the API. ---
	st, exportResp := doJSON(t, api, "GET", "/v1/account/export", ownerToken, nil)
	require.Equal(t, http.StatusOK, st)
	raw, err := json.Marshal(exportResp)
	require.NoError(t, err)
	body := string(raw)
	require.Contains(t, body, ownerCompanyID, "export bundle must contain the company")
	require.Contains(t, body, rfqID, "export bundle must contain the seeded RFQ")
	require.Contains(t, body, orderID, "export bundle must contain the seeded order")
	require.NotContains(t, body, "password_hash", "export bundle must never contain a password hash field")

	// --- Step 3+4: delete, then verify the audit row + anonymized DB rows. ---
	st, delResp := doJSON(t, api, "POST", "/v1/account/delete", ownerToken, map[string]any{
		"password": ownerPassword,
	})
	require.Equal(t, http.StatusOK, st, "delete response: %v", delResp)

	entries, err := q.ListAuditLogs(ctx, sqlc.ListAuditLogsParams{
		CompanyID: &ownerCompanyID,
		Lim:       50,
	})
	require.NoError(t, err)
	var found bool
	for _, e := range entries {
		if e.Action == "account.delete" {
			found = true
			require.True(t, strings.HasPrefix(e.Actor, "user:"), "actor must be user-prefixed, got %q", e.Actor)
		}
	}
	require.True(t, found, "expected an account.delete audit row for the company")

	gate, err := q.GetCompanyGate(ctx, ownerCompanyID)
	require.NoError(t, err)
	require.True(t, gate.DeletedAt.Valid, "company deleted_at must be set")

	company, err := q.GetCompanyByID(ctx, ownerCompanyID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(company.Name, "Deleted company "), "company name must be anonymized, got %q", company.Name)

	ownerUser, err := q.GetUserByID(ctx, ownerUserID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(ownerUser.Email, "deleted+"), "owner email must be anonymized, got %q", ownerUser.Email)
	require.Nil(t, ownerUser.PasswordHash, "owner password_hash must be cleared")

	// --- Step 5: the deleted company can no longer act. ---
	st, _ = doJSON(t, api, "GET", "/v1/me", ownerToken, nil)
	require.Equal(t, http.StatusForbidden, st, "a token for a deleted company must be rejected by RequireActiveCompany")

	st, _ = doJSON(t, api, "GET", "/v1/account/export", ownerToken, nil)
	require.Equal(t, http.StatusForbidden, st, "export must also be blocked once the company is deleted")

	st, _ = doJSON(t, api, "POST", "/v1/auth/login", "", map[string]any{
		"email": ownerEmail, "password": ownerPassword,
	})
	require.NotEqual(t, http.StatusOK, st, "login with the original (now-anonymized) email must not succeed")
	require.True(t, st == http.StatusUnauthorized || st == http.StatusForbidden,
		"expected 401 or 403 for a post-delete login attempt, got %d", st)

	// --- Step 6: transactional rows (RFQ + order) are retained. ---
	rfqRow, err := q.GetRFQ(ctx, rfqID)
	require.NoError(t, err, "RFQ row must still exist after account deletion")
	require.Equal(t, rfqID, rfqRow.ID)

	orderRow, err := q.GetOrder(ctx, orderID)
	require.NoError(t, err, "order row must still exist after account deletion")
	require.Equal(t, orderID, orderRow.ID)

	// --- Step 7: guard cases, each on a separate fresh company. ---

	// 7a: wrong password → 403, and the company must NOT be deleted.
	wrongPassEmail := uniqueEmail("e2e9-wrongpass-")
	const wrongPassPassword = "correct-secret-e2e9"
	st, reg2 := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E9 WrongPass Co", "company_type": "buyer",
		"email": wrongPassEmail, "password": wrongPassPassword,
	})
	require.Equal(t, http.StatusCreated, st)
	wrongPassToken := reg2["token"].(string)
	wrongPassCompanyID := reg2["company_id"].(string)

	st, _ = doJSON(t, api, "POST", "/v1/account/delete", wrongPassToken, map[string]any{
		"password": "definitely-not-the-password",
	})
	require.Equal(t, http.StatusForbidden, st, "account delete with a wrong password must be forbidden")

	gate2, err := q.GetCompanyGate(ctx, wrongPassCompanyID)
	require.NoError(t, err)
	require.False(t, gate2.DeletedAt.Valid, "a failed delete attempt must not have deleted the company")

	// 7b: a non-owner (member) token → 403, company must NOT be deleted.
	memberOwnerEmail := uniqueEmail("e2e9-memberowner-")
	const memberOwnerPassword = "member-owner-secret-e2e9"
	st, reg3 := doJSON(t, api, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "E2E9 Member Co", "company_type": "buyer",
		"email": memberOwnerEmail, "password": memberOwnerPassword,
	})
	require.Equal(t, http.StatusCreated, st)
	memberOwnerToken := reg3["token"].(string)
	memberCompanyID := reg3["company_id"].(string)

	st, inv := doJSON(t, api, "POST", "/v1/users/invite", memberOwnerToken, map[string]any{
		"email": uniqueEmail("e2e9-member-"), "role": "member",
	})
	require.Equal(t, http.StatusCreated, st)
	st, acc := doJSON(t, api, "POST", "/v1/auth/accept-invite", "", map[string]any{
		"token": inv["token"], "password": "memberpass-e2e9",
	})
	require.Equal(t, http.StatusCreated, st)
	memberToken := acc["token"].(string)

	st, _ = doJSON(t, api, "POST", "/v1/account/delete", memberToken, map[string]any{
		"password": "memberpass-e2e9",
	})
	require.Equal(t, http.StatusForbidden, st, "a non-owner member must not be able to delete the account")

	gate3, err := q.GetCompanyGate(ctx, memberCompanyID)
	require.NoError(t, err)
	require.False(t, gate3.DeletedAt.Valid, "a member's delete attempt must not have deleted the company")
}

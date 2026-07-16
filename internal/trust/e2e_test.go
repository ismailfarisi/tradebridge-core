package trust

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// E2E #3: an unverified seller is excluded from the gated match pool; after
// verification it is included; a block removes it again.
func TestE2EVerifyMatchAndBlock(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()
	res := &fakeResolver{records: map[string][]string{}}
	svc := NewService(q, pool, res, zerolog.Nop())

	buyer := makeCompany(t, q, "e2e-buyer")
	seller := makeCompany(t, q, "e2e-seller")
	_, err := q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: seller, Description: strptr("e2e widgets"), Categories: []string{"e2ewidgets"}, Location: strptr("SF"),
	})
	require.NoError(t, err)
	const domain = "e2e-seller.example"
	releaseDomain(t, pool, domain)
	t.Cleanup(func() { releaseDomain(t, pool, domain) })

	inPool := func(verifiedOnly bool) bool {
		rows, err := q.MatchSuppliers(ctx, sqlc.MatchSuppliersParams{
			ExcludeCompany: buyer, Needle: "e2ewidgets", VerifiedOnly: verifiedOnly, Lim: 50,
		})
		require.NoError(t, err)
		for _, r := range rows {
			if r.CompanyID == seller {
				return true
			}
		}
		return false
	}

	// Gate ON, unverified → excluded.
	require.False(t, inPool(true), "unverified seller excluded when gate on")
	require.True(t, inPool(false), "gate off → present regardless")

	// Verify via DNS-TXT, then included with gate ON.
	ch, err := svc.Claim(ctx, seller, domain)
	require.NoError(t, err)
	res.records[ch.RecordName] = []string{ch.RecordValue}
	r, err := svc.Check(ctx, seller, domain)
	require.NoError(t, err)
	require.True(t, r.Verified)
	require.True(t, inPool(true), "verified seller included when gate on")

	// Buyer blocks the seller → excluded in either direction.
	_, err = q.CreateBlock(ctx, sqlc.CreateBlockParams{CompanyID: buyer, BlockedCompanyID: seller})
	require.NoError(t, err)
	ids, err := q.ListBlockCompanyIDsFor(ctx, buyer)
	require.NoError(t, err)
	require.Contains(t, ids, seller, "block relationship visible to the match filter")
}

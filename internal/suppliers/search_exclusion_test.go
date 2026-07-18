package suppliers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// The public directory (DiscoverSuppliers/GetPublicSeller) already excludes
// suspended/soft-deleted companies (S9 Task 5). SearchSuppliers — the
// AUTHENTICATED search used by internal/suppliers/handler.go's /v1/suppliers/search
// — was missed and must not surface a suspended or deleted company either.
func TestSearchSuppliersExcludesSuspendedAndDeleted(t *testing.T) {
	q := pubQueriesOrSkip(t)
	ctx := context.Background()
	tag := "searchexclprobe"

	mk := func(name string) string {
		co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: name, Type: "seller"})
		require.NoError(t, err)
		_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
			CompanyID: co.ID, Description: sp(tag + " widgets"), Categories: []string{tag}, Location: sp("NYC"),
		})
		require.NoError(t, err)
		return co.ID
	}

	active := mk("search-active-" + tag)
	suspended := mk("search-suspended-" + tag)
	require.NoError(t, q.SetCompanyStatus(ctx, sqlc.SetCompanyStatusParams{ID: suspended, Status: "suspended"}))
	deleted := mk("search-deleted-" + tag)
	require.NoError(t, q.SoftDeleteAndAnonymizeCompany(ctx, deleted))

	rows, err := q.SearchSuppliers(ctx, sqlc.SearchSuppliersParams{Query: tag, Lim: 50, Off: 0})
	require.NoError(t, err)

	ids := map[string]bool{}
	for _, r := range rows {
		ids[r.CompanyID] = true
	}
	require.True(t, ids[active], "active company must be present")
	require.False(t, ids[suspended], "suspended company must not appear in authenticated search")
	require.False(t, ids[deleted], "deleted company must not appear in authenticated search")
}

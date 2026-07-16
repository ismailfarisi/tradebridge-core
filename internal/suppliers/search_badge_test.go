package suppliers

import (
	"context"
	"testing"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/stretchr/testify/require"
)

func TestSearchSuppliersIncludesVerificationStatus(t *testing.T) {
	q := pubQueriesOrSkip(t)
	ctx := context.Background()
	co, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "search-badge-co-unique", Type: "seller"})
	require.NoError(t, err)
	_, err = q.UpsertSupplierProfile(ctx, sqlc.UpsertSupplierProfileParams{
		CompanyID: co.ID, Description: sp("searchable badge widgets"), Categories: []string{"badgewidgets"}, Location: sp("LA"),
	})
	require.NoError(t, err)

	rows, err := q.SearchSuppliers(ctx, sqlc.SearchSuppliersParams{Query: "badgewidgets", Lim: 10, Off: 0})
	require.NoError(t, err)
	require.NotEmpty(t, rows)
	found := false
	for _, r := range rows {
		if r.CompanyID == co.ID {
			found = true
			require.Equal(t, "unverified", r.VerificationStatus)
		}
	}
	require.True(t, found)
}

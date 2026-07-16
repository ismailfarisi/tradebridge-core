package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestMigration016_PlansSeeded(t *testing.T) {
	pool := poolOrSkip(t)
	q := sqlc.New(pool)

	plans, err := q.ListPlans(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 3)

	by := map[string]sqlc.Plan{}
	for _, p := range plans {
		by[p.Code] = p
	}

	require.False(t, by["free"].AgentAutonomy)
	require.Equal(t, int32(25), by["free"].CatalogMaxItems)
	require.Equal(t, int32(10), by["free"].RfqMonthlyLimit)
	require.Equal(t, int32(2), by["free"].Seats)

	require.True(t, by["pro"].AgentAutonomy)
	require.Equal(t, int32(500), by["pro"].CatalogMaxItems)
	require.Equal(t, int32(100), by["pro"].RfqMonthlyLimit)
	require.Equal(t, int32(5), by["pro"].Seats)

	require.True(t, by["business"].AgentAutonomy)
	require.Equal(t, int32(-1), by["business"].CatalogMaxItems)
	require.Equal(t, int32(-1), by["business"].RfqMonthlyLimit)
	require.Equal(t, int32(-1), by["business"].Seats)
}

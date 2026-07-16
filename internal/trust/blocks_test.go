package trust

import (
	"context"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestListBlockCompanyIDsBothDirections(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	ctx := context.Background()
	a := makeCompany(t, q, "block-a")
	b := makeCompany(t, q, "block-b")
	x := makeCompany(t, q, "block-x")

	_, err := q.CreateBlock(ctx, sqlc.CreateBlockParams{CompanyID: a, BlockedCompanyID: b})
	require.NoError(t, err)
	_, err = q.CreateBlock(ctx, sqlc.CreateBlockParams{CompanyID: x, BlockedCompanyID: a})
	require.NoError(t, err)

	ids, err := q.ListBlockCompanyIDsFor(ctx, a)
	require.NoError(t, err)
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	require.True(t, set[b], "a blocked b → b present")
	require.True(t, set[x], "x blocked a → x present (reverse direction)")
}

func TestCreateBlockIdempotent(t *testing.T) {
	q, _ := testQueriesOrSkip(t)
	me := makeCompany(t, q, "block-idem-me")
	other := makeCompany(t, q, "block-idem-other")
	app := fiber.New()
	NewBlocksHandler(q).Register(app, stubIdentity(me, "owner"))
	code, _ := doJSON(t, app, "POST", "/v1/blocks/", map[string]string{"blocked_company_id": other})
	require.Equal(t, 200, code)
	code, _ = doJSON(t, app, "POST", "/v1/blocks/", map[string]string{"blocked_company_id": other})
	require.Equal(t, 200, code, "duplicate block is a no-op success, not 500")
}

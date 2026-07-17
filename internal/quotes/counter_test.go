package quotes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// TestRecordCounter_BumpsHeadAndAppendsVersion verifies that RecordCounter
// atomically applies the counter's terms to the quote head (incrementing
// version) and appends an immutable quote_versions row, on top of the version
// 1 row that Record wrote for the seller's original submit.
func TestRecordCounter_BumpsHeadAndAppendsVersion(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "counter-buyer", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "counter-seller", Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@counter.test", Role: "owner",
	})
	require.NoError(t, err)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need cartons", RawText: "cartons",
	})
	require.NoError(t, err)

	// Version 1: the seller's initial submit, via Record.
	n1 := Normalized{PriceCents: 5000, Currency: "USD", Notes: "initial", Completeness: 1}
	head, err := Record(ctx, q, pool, r.ID, seller.ID, "", n1)
	require.NoError(t, err)
	require.Equal(t, int32(1), head.Version)
	require.Equal(t, int64(5000), head.PriceCents)

	// Version 2: the seller's agent counters with new terms.
	days := int32(5)
	n2 := Normalized{PriceCents: 4500, Currency: "USD", DeliveryDays: &days, Notes: "final offer", Completeness: 1}
	head2, ver2, err := RecordCounter(ctx, q, pool, head.ID, "", n2)
	require.NoError(t, err)
	require.Equal(t, int32(2), head2.Version)
	require.Equal(t, int64(4500), head2.PriceCents)
	require.Equal(t, "submitted", head2.Status)
	require.Equal(t, int32(2), ver2.Version)
	require.Equal(t, "counter", ver2.Source)
	require.Equal(t, int64(4500), ver2.PriceCents)

	// The head in the DB reflects the bump.
	reread, err := q.GetQuote(ctx, head.ID)
	require.NoError(t, err)
	require.Equal(t, int32(2), reread.Version)
	require.Equal(t, int64(4500), reread.PriceCents)

	// Immutable history has both versions, ascending, with the right sources.
	versions, err := q.ListQuoteVersions(ctx, head.ID)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.Equal(t, int32(1), versions[0].Version)
	require.Equal(t, "seller", versions[0].Source)
	require.Equal(t, int64(5000), versions[0].PriceCents)
	require.Equal(t, int32(2), versions[1].Version)
	require.Equal(t, "counter", versions[1].Source)
	require.Equal(t, int64(4500), versions[1].PriceCents)

	// A seller re-submit AFTER a counter must NOT overwrite the immutable
	// counter row (v2); it appends a new seller version (v3) and bumps the head.
	n3 := Normalized{PriceCents: 4600, Currency: "USD", Notes: "seller re-submit post-counter", Completeness: 1}
	head3, err := Record(ctx, q, pool, r.ID, seller.ID, "", n3)
	require.NoError(t, err)
	require.Equal(t, int32(3), head3.Version, "post-counter submit appends a new version, never overwrites the counter")
	require.Equal(t, int64(4600), head3.PriceCents)

	versions, err = q.ListQuoteVersions(ctx, head.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "counter", versions[1].Source, "the counter row (v2) is untouched")
	require.Equal(t, int64(4500), versions[1].PriceCents)
	require.Equal(t, int32(3), versions[2].Version)
	require.Equal(t, "seller", versions[2].Source)
	require.Equal(t, int64(4600), versions[2].PriceCents)
}

// TestRecord_ResubmitKeepsVersion1Current verifies that a seller resubmitting
// their quote (before any counter) upserts version 1 in place rather than
// appending a new version row.
func TestRecord_ResubmitKeepsVersion1Current(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()

	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "resubmit-buyer", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "resubmit-seller", Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{
		CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@resubmit.test", Role: "owner",
	})
	require.NoError(t, err)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{
		BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "need cartons", RawText: "cartons",
	})
	require.NoError(t, err)

	n1 := Normalized{PriceCents: 5000, Currency: "USD", Notes: "v1", Completeness: 1}
	head, err := Record(ctx, q, pool, r.ID, seller.ID, "", n1)
	require.NoError(t, err)
	require.Equal(t, int32(1), head.Version)

	n1b := Normalized{PriceCents: 4800, Currency: "USD", Notes: "v1 revised", Completeness: 1}
	head2, err := Record(ctx, q, pool, r.ID, seller.ID, "", n1b)
	require.NoError(t, err)
	require.Equal(t, int32(1), head2.Version, "resubmit before any counter keeps version 1")

	versions, err := q.ListQuoteVersions(ctx, head.ID)
	require.NoError(t, err)
	require.Len(t, versions, 1, "resubmit upserts version 1 in place, no new row")
	require.Equal(t, int64(4800), versions[0].PriceCents)
}

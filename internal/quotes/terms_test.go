package quotes

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestNormalize_PaymentTerms(t *testing.T) {
	n, err := Normalize([]byte(`{"price":100,"payment_terms_days":30}`))
	require.NoError(t, err)
	require.NotNil(t, n.PaymentTermsDays)
	require.EqualValues(t, 30, *n.PaymentTermsDays)
	require.InDelta(t, 1.0/3, n.Completeness, 0.001, "terms do not change completeness scoring")

	n, err = Normalize([]byte(`{"price":100}`))
	require.NoError(t, err)
	require.Nil(t, n.PaymentTermsDays, "old agents send no terms")

	for _, bad := range []string{`{"price":100,"payment_terms_days":-1}`, `{"price":100,"payment_terms_days":400}`} {
		_, err := Normalize([]byte(bad))
		require.Error(t, err, bad)
	}
}

func TestCounterBody_PaymentTerms(t *testing.T) {
	price := int64(90000)
	cash, net := int32(0), int32(45)
	require.Equal(t, "Proposed terms — price: 900.00 AED, payment: cash on delivery", counterBody("", &price, nil, &cash, "AED"))
	require.Equal(t, "Proposed terms — payment: net 45 days", counterBody("", nil, nil, &net, "AED"))
}

func TestRevisionMessage_PaymentTerms(t *testing.T) {
	d, p := int32(7), int32(30)
	got := revisionMessage(sqlc.Quote{PriceCents: 120000, Currency: "AED", DeliveryDays: &d, PaymentTermsDays: &p})
	require.Equal(t, "Revised offer: 1,200.00 AED, delivery in 7 days, payment net 30 days", got)
}

// Terms persist on the head and every version; a counter that omits them
// keeps the previous terms rather than clearing them.
func TestRecord_PaymentTermsPersistAcrossVersions(t *testing.T) {
	q, pool := testQueriesOrSkip(t)
	ctx := context.Background()
	buyer, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "terms-buyer", Type: "buyer"})
	require.NoError(t, err)
	seller, err := q.CreateCompany(ctx, sqlc.CreateCompanyParams{Name: "terms-seller", Type: "seller"})
	require.NoError(t, err)
	u, err := q.CreateUser(ctx, sqlc.CreateUserParams{CompanyID: buyer.ID, Email: "u-" + buyer.ID + "@terms.test", Role: "owner"})
	require.NoError(t, err)
	r, err := q.CreateRFQ(ctx, sqlc.CreateRFQParams{BuyerCompanyID: buyer.ID, CreatedBy: u.ID, Title: "cartons", RawText: "cartons"})
	require.NoError(t, err)

	net30, cash := int32(30), int32(0)
	head, err := Record(ctx, q, pool, r.ID, seller.ID, "", Normalized{PriceCents: 5000, Currency: "AED", PaymentTermsDays: &net30, Completeness: 1})
	require.NoError(t, err)
	require.EqualValues(t, 30, *head.PaymentTermsDays)

	head, _, err = RecordCounter(ctx, q, pool, head.ID, "", Normalized{PriceCents: 4800, Currency: "AED", Completeness: 1})
	require.NoError(t, err)
	require.NotNil(t, head.PaymentTermsDays, "omitted terms keep the previous terms")
	require.EqualValues(t, 30, *head.PaymentTermsDays)

	head, ver, err := RecordCounter(ctx, q, pool, head.ID, "", Normalized{PriceCents: 4600, Currency: "AED", PaymentTermsDays: &cash, Completeness: 1})
	require.NoError(t, err)
	require.EqualValues(t, 0, *head.PaymentTermsDays)
	require.EqualValues(t, 0, *ver.PaymentTermsDays)

	versions, err := q.ListQuoteVersions(ctx, head.ID)
	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.EqualValues(t, 30, *versions[0].PaymentTermsDays)
	require.EqualValues(t, 30, *versions[1].PaymentTermsDays)
	require.EqualValues(t, 0, *versions[2].PaymentTermsDays)
}

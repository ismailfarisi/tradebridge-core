package invoicing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestDeriveLineItemDefaultQuantity(t *testing.T) {
	o := sqlc.Order{ID: "o1", TotalCents: 5000}
	r := sqlc.Rfq{Title: "500 steel bolts", Requirements: []byte(`{}`)}
	desc, qty, unit, amt := deriveLineItem(o, r)
	require.Equal(t, "500 steel bolts", desc)
	require.Equal(t, int64(1), qty)
	require.Equal(t, int64(5000), unit)
	require.Equal(t, int64(5000), amt)
}

func TestDeriveLineItemParsesQuantity(t *testing.T) {
	o := sqlc.Order{ID: "o2", TotalCents: 6000}
	r := sqlc.Rfq{Title: "bolts", Requirements: []byte(`{"quantity": 3}`)}
	_, qty, unit, amt := deriveLineItem(o, r)
	require.Equal(t, int64(3), qty)
	require.Equal(t, int64(2000), unit) // 6000/3
	require.Equal(t, int64(6000), amt)  // total stays exact
}

func TestDeriveLineItemEmptyTitleFallback(t *testing.T) {
	o := sqlc.Order{ID: "oX", TotalCents: 100}
	r := sqlc.Rfq{Title: "", Requirements: []byte(`{}`)}
	desc, _, _, _ := deriveLineItem(o, r)
	require.Equal(t, "Order oX", desc)
}

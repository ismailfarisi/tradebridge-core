package invoicing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

func TestRenderProducesPDF(t *testing.T) {
	inv := sqlc.Invoice{
		InvoiceNumber: "INV-2026-000123", Currency: "USD",
		SubtotalCents: 5000, TotalCents: 5000,
		IssuedAt: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
	}
	lines := []sqlc.InvoiceLineItem{{Description: "500 steel bolts", Quantity: 1, UnitPriceCents: 5000, AmountCents: 5000}}
	b, err := Render(inv, lines, Party{Name: "Acme Steel"}, Party{Name: "BuildCo"})
	require.NoError(t, err)
	require.True(t, len(b) > 400, "pdf should be non-trivial")
	require.Equal(t, "%PDF-", string(b[:5]), "valid PDF header")
}

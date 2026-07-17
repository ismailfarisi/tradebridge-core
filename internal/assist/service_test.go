package assist

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestImproveRFQ_NoAPIKey_UsesDeterministicStub verifies that with no API key
// configured, ImproveRFQ never touches the network and instead returns a
// non-empty, deterministic suggestion that echoes the buyer's draft.
func TestImproveRFQ_NoAPIKey_UsesDeterministicStub(t *testing.T) {
	svc := NewService("", zerolog.Nop())
	out, err := svc.ImproveRFQ(context.Background(), "need bolts")
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.Contains(t, out, "bolts")
}

// TestExplainQuotes_NoAPIKey_NamesTheCheaperSeller verifies the stub template
// names the cheapest quote's seller when comparing two quotes.
func TestExplainQuotes_NoAPIKey_NamesTheCheaperSeller(t *testing.T) {
	svc := NewService("", zerolog.Nop())
	deliveryDays := int32(3)
	cheap := QuoteSummary{SellerName: "Cheap Co", PriceCents: 1000, Currency: "USD", DeliveryDays: &deliveryDays, ReviewCount: 5, AvgRating: 4.2, Verified: true}
	pricey := QuoteSummary{SellerName: "Pricey Inc", PriceCents: 5000, Currency: "USD", DeliveryDays: &deliveryDays, ReviewCount: 2, AvgRating: 4.8, Verified: false}

	out, err := svc.ExplainQuotes(context.Background(), "Bolts", []QuoteSummary{pricey, cheap})
	require.NoError(t, err)
	require.NotEmpty(t, out)
	require.True(t, strings.Contains(out, "Cheap Co"), "stub should name the cheaper seller: %s", out)
}

// TestExplainQuotes_NoQuotes_ReturnsFriendlyMessage verifies an empty quote
// list still produces a non-empty, friendly message rather than an error.
func TestExplainQuotes_NoQuotes_ReturnsFriendlyMessage(t *testing.T) {
	svc := NewService("", zerolog.Nop())
	out, err := svc.ExplainQuotes(context.Background(), "Bolts", nil)
	require.NoError(t, err)
	require.NotEmpty(t, out)
}

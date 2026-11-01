package quotes

import "testing"

func TestFormatAmount(t *testing.T) {
	cases := []struct {
		cents    int64
		currency string
		want     string
	}{
		{0, "AED", "0.00 AED"},
		{4500, "USD", "45.00 USD"},
		{1160050, "AED", "11,600.50 AED"},
		{123456789, "SAR", "1,234,567.89 SAR"},
		{100000, "", "1,000.00 AED"},
		{-250, "AED", "-2.50 AED"},
	}
	for _, c := range cases {
		if got := formatAmount(c.cents, c.currency); got != c.want {
			t.Errorf("formatAmount(%d, %q) = %q, want %q", c.cents, c.currency, got, c.want)
		}
	}
}

func TestCounterBody_UsesQuoteCurrency(t *testing.T) {
	price := int64(1100000)
	days := int32(5)
	got := counterBody("Can you do better?", &price, &days, "AED")
	want := "Can you do better?\n\nProposed terms — price: 11,000.00 AED, delivery: 5 days"
	if got != want {
		t.Errorf("counterBody = %q, want %q", got, want)
	}
}

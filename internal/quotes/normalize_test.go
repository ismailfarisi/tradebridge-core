package quotes

import "testing"

func TestNormalize(t *testing.T) {
	n, err := Normalize([]byte(`{"price":1200.5,"currency":"eur","delivery_days":14,"notes":"in stock"}`))
	if err != nil {
		t.Fatal(err)
	}
	if n.PriceCents != 120050 {
		t.Errorf("price_cents = %d, want 120050", n.PriceCents)
	}
	if n.Currency != "EUR" {
		t.Errorf("currency = %q, want EUR", n.Currency)
	}
	if n.Completeness != 1.0 {
		t.Errorf("completeness = %v, want 1.0 (all fields present)", n.Completeness)
	}

	// Price-only quote → completeness 1/3.
	n, err = Normalize([]byte(`{"price":50}`))
	if err != nil {
		t.Fatal(err)
	}
	if n.Currency != "USD" {
		t.Errorf("currency default = %q, want USD", n.Currency)
	}
	if n.Completeness < 0.33 || n.Completeness > 0.34 {
		t.Errorf("completeness = %v, want ~0.333", n.Completeness)
	}

	// Missing / non-positive price is rejected.
	for _, bad := range []string{`{}`, `{"price":0}`, `{"price":-5}`, `not json`} {
		if _, err := Normalize([]byte(bad)); err == nil {
			t.Errorf("Normalize(%q) should error", bad)
		}
	}
}

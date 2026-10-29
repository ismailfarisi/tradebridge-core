package simseller

import "testing"

func TestNextPriceConcedesThenHoldsAtFloor(t *testing.T) {
	p := Persona{Floor: 0.8, Concede: 0.5}
	opening := int64(100_000)
	cur := opening
	for i := 0; i < 10; i++ {
		next := NextPrice(p, opening, cur, 50_000)
		if next > cur {
			t.Fatalf("price went up: %d -> %d", cur, next)
		}
		if next < 80_000 {
			t.Fatalf("went below floor: %d", next)
		}
		cur = next
	}
	if cur != 80_000 {
		t.Fatalf("should settle at floor, got %d", cur)
	}
}

func TestNextPriceMeetsOfferAboveFloor(t *testing.T) {
	p := Persona{Floor: 0.5, Concede: 1}
	if got := NextPrice(p, 100_000, 100_000, 90_000); got != 90_000 {
		t.Fatalf("want 90000, got %d", got)
	}
}

func TestBasePriceDeterministic(t *testing.T) {
	a := BasePrice("x", "500 corrugated cartons", "500 units")
	b := BasePrice("y", "500 corrugated cartons", "500 units")
	if a != b || a < 50_000 {
		t.Fatalf("base price not deterministic or too small: %d %d", a, b)
	}
}

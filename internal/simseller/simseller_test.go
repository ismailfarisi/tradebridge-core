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

func TestFloorForPricesPaymentTermsAndLeadTime(t *testing.T) {
	p := Persona{Floor: 0.8, TermsDays: 0, MaxTermsDays: 60, CashValue: 0.03, DeliveryDays: 7, SlackValue: 0.01}
	base := FloorFor(p, 100_000, 0, 7)
	if base != 80_000 {
		t.Fatalf("floor at own terms = %d", base)
	}
	net60 := FloorFor(p, 100_000, 60, 7)
	net30 := FloorFor(p, 100_000, 30, 7)
	if !(net60 > net30 && net30 > base) {
		t.Fatalf("longer credit must raise the floor: cash %d net30 %d net60 %d", base, net30, net60)
	}
	if slow := FloorFor(p, 100_000, 30, 14); slow >= net30 {
		t.Fatalf("extra lead time must lower the floor: %d vs %d", slow, net30)
	}
	if AcceptTerms(p, 90) != 60 || AcceptTerms(p, 30) != 30 {
		t.Fatal("AcceptTerms caps at the credit limit")
	}
}

// A seller holding at its floor on net 60 can go lower when the buyer offers
// to pay on net 30: the trade the buyer engine's logroll relies on.
func TestFasterPaymentUnlocksLowerPrice(t *testing.T) {
	p := Persona{Floor: 0.8, Concede: 0.5, TermsDays: 0, MaxTermsDays: 60, CashValue: 0.03}
	held := nextPrice(p, FloorFor(p, 100_000, 60, 0), 84_800, 80_000)
	if held != 84_800 {
		t.Fatalf("at the net-60 floor the seller holds, got %d", held)
	}
	moved := nextPrice(p, FloorFor(p, 100_000, 30, 0), 84_800, 80_000)
	if moved >= held {
		t.Fatalf("net 30 should unlock a lower price: %d vs %d", moved, held)
	}
}

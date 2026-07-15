package rfq

import (
	"context"
	"testing"
)

func TestCanTransition(t *testing.T) {
	ok := [][2]string{
		{StatusDraft, StatusOpen},
		{StatusDraft, StatusClosed},
		{StatusOpen, StatusQuoted},
		{StatusOpen, StatusClosed},
		{StatusQuoted, StatusClosed},
	}
	for _, tc := range ok {
		if !CanTransition(tc[0], tc[1]) {
			t.Errorf("expected %s → %s to be allowed", tc[0], tc[1])
		}
	}
	bad := [][2]string{
		{StatusClosed, StatusOpen},
		{StatusQuoted, StatusOpen},
		{StatusOpen, StatusDraft},
		{StatusClosed, StatusClosed},
		{StatusDraft, StatusQuoted},
	}
	for _, tc := range bad {
		if CanTransition(tc[0], tc[1]) {
			t.Errorf("expected %s → %s to be rejected", tc[0], tc[1])
		}
	}
}

func TestHeuristicExtract(t *testing.T) {
	req, err := HeuristicExtractor{}.Extract(context.Background(),
		"Cartons needed",
		"We need 500 cartons for packaging our products, deliver to Dubai by next month.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Category != "packaging" {
		t.Errorf("category = %q, want packaging", req.Category)
	}
	if req.Quantity != "500 cartons" {
		t.Errorf("quantity = %q, want '500 cartons'", req.Quantity)
	}
	if req.Location != "Dubai" {
		t.Errorf("location = %q, want Dubai", req.Location)
	}
	if len(req.Keywords) == 0 {
		t.Error("expected non-empty keywords")
	}
}

func TestNeedlesDedup(t *testing.T) {
	req := Requirements{Category: "packaging", Keywords: []string{"packaging", "cartons", ""}}
	got := req.needles()
	want := []string{"packaging", "cartons"}
	if len(got) != len(want) {
		t.Fatalf("needles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("needles = %v, want %v", got, want)
		}
	}
}

package ulid

import "testing"

func TestNewLength(t *testing.T) {
	id := New()
	if len(id) != 26 {
		t.Fatalf("ULID length = %d, want 26 (%q)", len(id), id)
	}
}

func TestNewUnique(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := New()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ULID: %q", id)
		}
		seen[id] = struct{}{}
	}
}

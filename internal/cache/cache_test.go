package cache

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestNilClientFailsOpen(t *testing.T) {
	c := New(nil, zerolog.Nop())
	var dst map[string]int
	hit, err := c.GetJSON(context.Background(), "k", &dst)
	if err != nil || hit {
		t.Fatalf("nil client: want miss+no-err, got hit=%v err=%v", hit, err)
	}
	// Set + Delete must not panic with a nil client.
	c.SetJSON(context.Background(), "k", map[string]int{"a": 1}, time.Minute)
	c.Delete(context.Background(), "k")
}

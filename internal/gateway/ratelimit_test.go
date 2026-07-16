package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// fakeCounter is an in-memory counter for testing the limiter logic.
type fakeCounter struct {
	vals map[string]int64
	err  error
}

func newFake() *fakeCounter { return &fakeCounter{vals: map[string]int64{}} }
func (f *fakeCounter) incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.vals[key]++
	return f.vals[key], nil
}
func (f *fakeCounter) get(_ context.Context, key string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.vals[key], nil
}

func TestRateLimiter_PerCred(t *testing.T) {
	c := newFake()
	rl := &rateLimiter{c: c, perMin: 3, logger: zerolog.Nop()}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if !rl.allowCred(ctx, "cred1", 100) {
			t.Fatalf("call %d should be allowed", i)
		}
	}
	if rl.allowCred(ctx, "cred1", 100) {
		t.Fatal("4th call in the same window should be blocked")
	}
	if !rl.allowCred(ctx, "cred1", 101) {
		t.Fatal("next window should reset")
	}
}

func TestRateLimiter_AuthFail(t *testing.T) {
	c := newFake()
	rl := &rateLimiter{c: c, failMax: 2, failWin: time.Minute, logger: zerolog.Nop()}
	ctx := context.Background()
	if rl.authFailBlocked(ctx, "1.2.3.4") {
		t.Fatal("fresh IP not blocked")
	}
	rl.recordAuthFailure(ctx, "1.2.3.4")
	rl.recordAuthFailure(ctx, "1.2.3.4")
	if !rl.authFailBlocked(ctx, "1.2.3.4") {
		t.Fatal("IP should be blocked after reaching failMax")
	}
	if rl.authFailBlocked(ctx, "9.9.9.9") {
		t.Fatal("other IPs must be independent")
	}
}

func TestRateLimiter_FailOpen(t *testing.T) {
	// nil counter → always allow, never block.
	rl := &rateLimiter{c: nil, perMin: 1, failMax: 1, logger: zerolog.Nop()}
	ctx := context.Background()
	if !rl.allowCred(ctx, "x", 1) || !rl.allowCred(ctx, "x", 1) {
		t.Fatal("nil counter must fail open (allow)")
	}
	rl.recordAuthFailure(ctx, "ip")
	if rl.authFailBlocked(ctx, "ip") {
		t.Fatal("nil counter must not block")
	}
	// Erroring counter → also fail open.
	rl2 := &rateLimiter{c: &fakeCounter{err: context.DeadlineExceeded}, perMin: 1, failMax: 1, logger: zerolog.Nop()}
	if !rl2.allowCred(ctx, "x", 1) {
		t.Fatal("counter error must fail open (allow)")
	}
	if rl2.authFailBlocked(ctx, "ip") {
		t.Fatal("counter error must not block")
	}
}

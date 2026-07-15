package rfq

import (
	"errors"
	"sync"
	"testing"
)

func TestHealthTracker_EmptyIsHealthy(t *testing.T) {
	h := NewHealthTracker(20)
	s := h.Snapshot()
	if s.Degraded || s.RecentFallbacks != 0 || s.Window != 20 {
		t.Fatalf("empty tracker not healthy: %+v", s)
	}
}

func TestHealthTracker_FallbackThenSuccessClearsDegraded(t *testing.T) {
	h := NewHealthTracker(20)
	h.RecordFallback(errors.New("boom"))
	s := h.Snapshot()
	if !s.Degraded || s.RecentFallbacks != 1 || s.LastError != "boom" || s.LastFallbackAt == nil {
		t.Fatalf("after fallback: %+v", s)
	}
	h.RecordSuccess()
	s = h.Snapshot()
	if s.Degraded { // last outcome was success
		t.Fatalf("success should clear degraded: %+v", s)
	}
	if s.RecentFallbacks != 1 { // fallback still in the window
		t.Fatalf("fallback should remain in window: %+v", s)
	}
}

func TestHealthTracker_RingEvictsOldOutcomes(t *testing.T) {
	h := NewHealthTracker(3)
	h.RecordFallback(errors.New("old"))
	h.RecordSuccess()
	h.RecordSuccess()
	h.RecordSuccess() // evicts the fallback
	s := h.Snapshot()
	if s.RecentFallbacks != 0 {
		t.Fatalf("old fallback should be evicted: %+v", s)
	}
}

func TestHealthTracker_NilIsNoop(t *testing.T) {
	var h *HealthTracker
	h.RecordSuccess()
	h.RecordFallback(errors.New("x"))
	if s := h.Snapshot(); s.Degraded {
		t.Fatalf("nil tracker should be healthy no-op: %+v", s)
	}
}

func TestHealthTracker_ConcurrentAccess(t *testing.T) {
	h := NewHealthTracker(20)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				h.RecordSuccess()
			} else {
				h.RecordFallback(errors.New("e"))
			}
			_ = h.Snapshot()
		}(i)
	}
	wg.Wait()
}

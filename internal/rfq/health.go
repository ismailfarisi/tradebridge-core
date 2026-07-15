package rfq

import (
	"sync"
	"time"
)

// HealthSnapshot is a point-in-time view of recent LLM intake outcomes.
type HealthSnapshot struct {
	Window          int        `json:"window"`
	RecentFallbacks int        `json:"recent_fallbacks"`
	LastError       string     `json:"last_error"`
	LastFallbackAt  *time.Time `json:"last_fallback_at"`
	Degraded        bool       `json:"degraded"`
}

// HealthTracker records the last N intake outcomes (LLM success vs heuristic
// fallback) in a ring, so the admin UI can surface silent degradation. Safe for
// concurrent use. A nil *HealthTracker is a valid no-op recorder.
type HealthTracker struct {
	mu       sync.Mutex
	window   int
	outcomes []bool // true = fell back to heuristic
	lastErr  string
	lastAt   *time.Time
}

// NewHealthTracker returns a tracker over the last `window` outcomes (min 1,
// defaults to 20 for non-positive input).
func NewHealthTracker(window int) *HealthTracker {
	if window <= 0 {
		window = 20
	}
	return &HealthTracker{window: window}
}

// RecordSuccess notes one successful model-driven decision.
func (h *HealthTracker) RecordSuccess() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.push(false)
}

// RecordFallback notes one degradation to the heuristic, keeping the error text.
func (h *HealthTracker) RecordFallback(err error) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.push(true)
	if err != nil {
		msg := err.Error()
		const max = 300
		if len(msg) > max {
			msg = msg[:max]
		}
		h.lastErr = msg
	}
	t := time.Now()
	h.lastAt = &t
}

// push appends an outcome, evicting the oldest when the window is full.
// The caller holds the lock.
func (h *HealthTracker) push(fellBack bool) {
	h.outcomes = append(h.outcomes, fellBack)
	if len(h.outcomes) > h.window {
		h.outcomes = h.outcomes[len(h.outcomes)-h.window:]
	}
}

// Snapshot returns the current view. Degraded means the most recent outcome was
// a fallback; a single subsequent success clears it.
func (h *HealthTracker) Snapshot() HealthSnapshot {
	if h == nil {
		return HealthSnapshot{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	recent := 0
	for _, fb := range h.outcomes {
		if fb {
			recent++
		}
	}
	degraded := len(h.outcomes) > 0 && h.outcomes[len(h.outcomes)-1]
	var lastAt *time.Time
	if h.lastAt != nil {
		t := *h.lastAt
		lastAt = &t
	}
	return HealthSnapshot{
		Window:          h.window,
		RecentFallbacks: recent,
		LastError:       h.lastErr,
		LastFallbackAt:  lastAt,
		Degraded:        degraded,
	}
}

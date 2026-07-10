// Package rfq implements the RFQ engine: a buyer posts a plain-text request,
// Core extracts structured requirements, matches sellers, and broadcasts a
// durable `rfq.matched` event to each matched seller's agent.
package rfq

import "fmt"

// RFQ lifecycle states.
const (
	StatusDraft  = "draft"
	StatusOpen   = "open"
	StatusQuoted = "quoted"
	StatusClosed = "closed"
)

// transitions defines the allowed status graph: draft → open → quoted → closed,
// with open/quoted also able to close directly.
var transitions = map[string]map[string]bool{
	StatusDraft:  {StatusOpen: true, StatusClosed: true},
	StatusOpen:   {StatusQuoted: true, StatusClosed: true},
	StatusQuoted: {StatusClosed: true},
	StatusClosed: {},
}

// CanTransition reports whether an RFQ may move from one status to another.
func CanTransition(from, to string) bool {
	return transitions[from][to]
}

// ValidateTransition returns an error describing an illegal status change.
func ValidateTransition(from, to string) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("illegal RFQ transition %q → %q", from, to)
	}
	return nil
}

// Requirements is the structured form of a plain-text RFQ. It is stored as the
// rfqs.requirements JSONB column and included in the `rfq.matched` event payload.
type Requirements struct {
	Category string   `json:"category"`
	Quantity string   `json:"quantity"`
	Specs    []string `json:"specs"`
	Location string   `json:"location"`
	Deadline string   `json:"deadline"`
	Keywords []string `json:"keywords"`
}

// ChatMessage is one turn of the buyer↔assistant RFQ-intake conversation. The
// frontend holds the running transcript and posts it to /v1/rfq-chat; on RFQ
// creation the whole transcript is persisted to rfqs.messages.
type ChatMessage struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"`
}

// needles returns the distinct, non-empty search terms used to match sellers:
// the category first (highest signal), then any keywords.
func (r Requirements) needles() []string {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range append([]string{r.Category}, r.Keywords...) {
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

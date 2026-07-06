// Package protocol defines the wire contract shared between the Core API (which
// publishes agent events) and the Gateway (which streams them to agents). Both
// sides marshal/unmarshal the same Event envelope so the SSE `id:` (seq) and
// `event:` (type) fields line up with the durable agent_events row.
package protocol

import "encoding/json"

// Event is one durable agent event as it travels over Redis and SSE. Seq is the
// agent_events.seq — a monotonic cursor the agent echoes back as Last-Event-ID.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Channel is the Redis pub/sub channel an agent's Gateway connection subscribes
// to for its company's live events.
func Channel(companyID string) string {
	return "agent:events:" + companyID
}

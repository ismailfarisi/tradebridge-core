// Package activation computes onboarding progress and records funnel milestones.
package activation

import (
	"context"
	"encoding/json"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// Milestone event names (funnel vocabulary).
const (
	EventSignup         = "signup"
	EventProfileSet     = "profile_set"
	EventCatalogAdded   = "catalog_added"
	EventVerified       = "verified"
	EventAgentConnected = "agent_connected"
	EventFirstRFQ       = "first_rfq"
	EventFirstQuote     = "first_quote"
	EventFirstOrder     = "first_order"
	EventUpgrade        = "upgrade"
)

// Emit records a milestone for a company. Best-effort and idempotent: the
// UNIQUE(company_id,event) makes repeats a no-op, and ALL errors are swallowed
// so a failed emit never breaks the request it rides on. Call it AFTER the
// surrounding write/tx has committed, with the pool-backed queries.
func Emit(ctx context.Context, q *sqlc.Queries, companyID, userID, event string, props map[string]any) {
	if q == nil || companyID == "" || event == "" {
		return
	}
	raw := []byte("{}")
	if props != nil {
		if b, err := json.Marshal(props); err == nil {
			raw = b
		}
	}
	var uid *string
	if userID != "" {
		uid = &userID
	}
	_ = q.InsertActivationEvent(ctx, sqlc.InsertActivationEventParams{
		CompanyID: companyID,
		UserID:    uid,
		Event:     event,
		Props:     raw,
	})
}

package negotiation

import (
	"context"
	"strings"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// PauseState is the effective kill-switch state for one buyer company: the
// platform-wide switch (platform admins) and the company's own switch
// (company owner/admin). Either one stops the engine from countering,
// accepting or auto-accepting.
type PauseState struct {
	PlatformPaused bool   `json:"platform_paused"`
	PlatformReason string `json:"platform_reason"`
	CompanyPaused  bool   `json:"company_paused"`
	CompanyReason  string `json:"company_reason"`
}

// Paused reports whether any switch is engaged.
func (p PauseState) Paused() bool { return p.PlatformPaused || p.CompanyPaused }

// PausedBy names who engaged the switch ("platform" wins over "company"), or
// "" when negotiation is running.
func (p PauseState) PausedBy() string {
	switch {
	case p.PlatformPaused:
		return "platform"
	case p.CompanyPaused:
		return "company"
	}
	return ""
}

// Message is the buyer-facing explanation.
func (p PauseState) Message() string {
	var msg, reason string
	switch {
	case p.PlatformPaused:
		msg, reason = "Automated negotiation is paused by TradeBridge", p.PlatformReason
	case p.CompanyPaused:
		msg, reason = "Automated negotiation is paused by your company", p.CompanyReason
	default:
		return ""
	}
	if r := strings.TrimSpace(reason); r != "" {
		msg += ": " + r
	}
	return msg + "."
}

func pauseStateOf(r sqlc.GetNegotiationPauseStateRow) PauseState {
	return PauseState{PlatformPaused: r.PlatformPaused, PlatformReason: r.PlatformReason, CompanyPaused: r.CompanyPaused, CompanyReason: r.CompanyReason}
}

// pauseState reads both switches with q (pass a transaction's queries to make
// the read part of that transaction).
func (e *Engine) pauseState(ctx context.Context, q *sqlc.Queries, companyID string) (PauseState, error) {
	r, err := q.GetNegotiationPauseState(ctx, companyID)
	if err != nil {
		return PauseState{}, err
	}
	return pauseStateOf(r), nil
}

// PauseState returns the effective kill-switch state for a buyer company.
func (e *Engine) PauseState(ctx context.Context, companyID string) (PauseState, error) {
	return e.pauseState(ctx, e.q, companyID)
}

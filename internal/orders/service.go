// Package orders serves purchase orders created when a buyer accepts a
// quote. Both parties to an order (buyer and seller) may view it and drive
// it through its lifecycle: created -> confirmed -> fulfilled -> completed,
// with cancelled and disputed reachable from active states.
package orders

import (
	"errors"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

// ErrNotFound is returned when an order is missing or not visible to the caller.
var ErrNotFound = errors.New("order not found")

// ConflictError signals an illegal state transition (HTTP 409).
type ConflictError struct{ Msg string }

func (e ConflictError) Error() string { return e.Msg }

// ForbiddenError signals the caller's role may not drive this transition (403).
type ForbiddenError struct{ Msg string }

func (e ForbiddenError) Error() string { return e.Msg }

// allowedTransitions is the order state machine. disputed is reachable from any
// active state (via the dispute endpoint, Plan 4) and resolved by an admin.
var allowedTransitions = map[string]map[string]bool{
	"created":   {"confirmed": true, "cancelled": true, "disputed": true},
	"confirmed": {"fulfilled": true, "cancelled": true, "disputed": true},
	"fulfilled": {"completed": true, "disputed": true},
	"disputed":  {"completed": true, "cancelled": true},
}

// PartyTargets are the states a buyer/seller may drive via POST .../transitions.
// disputed is excluded — it must go through the dispute endpoint so a dispute
// row is created alongside the transition.
var PartyTargets = map[string]bool{
	"confirmed": true, "fulfilled": true, "completed": true, "cancelled": true,
}

// CanTransition reports whether from->to is a legal edge in the machine.
func CanTransition(from, to string) bool { return allowedTransitions[from][to] }

// RoleFor returns which party role may drive a transition to `to`:
// "seller", "buyer", or "either".
func RoleFor(to string) string {
	switch to {
	case "confirmed", "fulfilled":
		return "seller"
	case "completed":
		return "buyer"
	default: // cancelled, disputed
		return "either"
	}
}

// actorRole returns "buyer" or "seller" for companyID relative to the order,
// and false if the company is not a party to it.
func actorRole(o sqlc.Order, companyID string) (string, bool) {
	switch companyID {
	case o.BuyerCompanyID:
		return "buyer", true
	case o.SellerCompanyID:
		return "seller", true
	default:
		return "", false
	}
}

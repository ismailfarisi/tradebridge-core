// Package orders serves purchase orders created when a buyer accepts a
// quote. Both parties to an order (buyer and seller) may view it and drive
// it through its lifecycle: created -> confirmed -> fulfilled -> completed,
// with cancelled and disputed reachable from active states.
package orders

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/notify"
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

// Service drives order transitions (state machine + authz), transactionally
// updating status and appending to the order_events timeline, then notifying
// the counterparty after commit.
type Service struct {
	q        *sqlc.Queries
	pool     *pgxpool.Pool
	notifier *notify.Notifier
	logger   zerolog.Logger
}

// NewService builds the orders service.
func NewService(q *sqlc.Queries, pool *pgxpool.Pool, notifier *notify.Notifier, logger zerolog.Logger) *Service {
	return &Service{q: q, pool: pool, notifier: notifier, logger: logger}
}

// Transition validates and applies a state transition driven by actorCompanyID
// (a party to the order), recording it in the order_events timeline and
// notifying the counterparty after commit.
func (s *Service) Transition(ctx context.Context, orderID, actorCompanyID, to, note string) (sqlc.Order, error) {
	order, err := s.q.GetOrder(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Order{}, ErrNotFound
	} else if err != nil {
		return sqlc.Order{}, err
	}

	role, ok := actorRole(order, actorCompanyID)
	if !ok {
		// Not a party to this order — don't leak existence across tenants.
		return sqlc.Order{}, ErrNotFound
	}
	if !PartyTargets[to] {
		return sqlc.Order{}, ConflictError{Msg: "unsupported transition target"}
	}
	if !CanTransition(order.Status, to) {
		return sqlc.Order{}, ConflictError{Msg: "cannot move from " + order.Status + " to " + to}
	}
	if want := RoleFor(to); want != "either" && want != role {
		return sqlc.Order{}, ForbiddenError{Msg: role + " may not drive transition to " + to}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sqlc.Order{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	fromStatus := order.Status
	updated, err := qtx.SetOrderStatus(ctx, sqlc.SetOrderStatusParams{ID: orderID, Status: to})
	if err != nil {
		return sqlc.Order{}, err
	}
	if _, err := qtx.CreateOrderEvent(ctx, sqlc.CreateOrderEventParams{
		OrderID:        orderID,
		FromStatus:     fromStatus,
		ToStatus:       to,
		Actor:          role,
		ActorCompanyID: &actorCompanyID,
		Note:           note,
	}); err != nil {
		return sqlc.Order{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Order{}, err
	}

	if s.notifier != nil {
		if ev, ok := notifyEventFor(to, orderID); ok {
			counterparty := updated.SellerCompanyID
			if role == "seller" {
				counterparty = updated.BuyerCompanyID
			}
			s.notifier.NotifyCompany(ctx, counterparty, ev)
		}
	}

	return updated, nil
}

// notifyEventFor maps a transition target to the notify.Event sent to the
// counterparty, or false if the target has no associated notification.
func notifyEventFor(to, orderID string) (notify.Event, bool) {
	m := map[string]struct{ typ, title, body string }{
		"confirmed": {notify.TypeOrderConfirmed, "Order confirmed", "The seller confirmed your order."},
		"fulfilled": {notify.TypeOrderFulfilled, "Order fulfilled", "The seller marked your order fulfilled."},
		"completed": {notify.TypeOrderCompleted, "Order completed", "The order was marked completed."},
		"cancelled": {notify.TypeOrderCancelled, "Order cancelled", "The order was cancelled."},
	}
	e, ok := m[to]
	if !ok {
		return notify.Event{}, false
	}
	return notify.Event{
		Type: e.typ, Title: e.title, Body: e.body,
		Data:     map[string]any{"order_id": orderID},
		CTALabel: "View order", CTAPath: "/orders/" + orderID,
	}, true
}

// List returns every order companyID is a party to (buyer or seller),
// newest first.
func (s *Service) List(ctx context.Context, companyID string) ([]sqlc.Order, error) {
	return s.q.ListOrdersByCompany(ctx, companyID)
}

// GetWithTimeline returns an order and its full event timeline, scoped to a
// caller who is a party to it.
func (s *Service) GetWithTimeline(ctx context.Context, orderID, companyID string) (sqlc.Order, []sqlc.OrderEvent, error) {
	order, err := s.q.GetOrder(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Order{}, nil, ErrNotFound
	} else if err != nil {
		return sqlc.Order{}, nil, err
	}
	if order.BuyerCompanyID != companyID && order.SellerCompanyID != companyID {
		return sqlc.Order{}, nil, ErrNotFound
	}
	events, err := s.q.ListOrderEvents(ctx, orderID)
	if err != nil {
		return sqlc.Order{}, nil, err
	}
	return order, events, nil
}

// Package disputes lets either party to an active order escalate it —
// locking the order out of further party-driven transitions — and lets a
// platform admin resolve the dispute, moving the order to a terminal state
// and logging the action to audit_logs. Raise and Resolve are each a single
// transaction (all-or-nothing); notifications fire only after commit.
package disputes

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// ErrNotFound is returned when an order/dispute is missing, or the caller is
// not a party to it (indistinguishable on purpose, so a non-party can't
// probe for existence).
var ErrNotFound = errors.New("not found")

// ConflictError signals an illegal dispute action (HTTP 409).
type ConflictError struct{ Msg string }

func (e ConflictError) Error() string { return e.Msg }

// ValidationError signals bad input (HTTP 400).
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

var validReasons = map[string]bool{
	"not_delivered":    true,
	"not_as_described": true,
	"payment":          true,
	"other":            true,
}

// disputableStatuses are the order states a dispute may be raised from — the
// active lifecycle before completion.
var disputableStatuses = map[string]bool{
	"created": true, "confirmed": true, "fulfilled": true,
}

// validOutcomes are the dispute statuses an admin resolution may set.
var validOutcomes = map[string]bool{"resolved": true, "rejected": true}

// validOrderTargets are the terminal order states a resolution may move the
// order to.
var validOrderTargets = map[string]bool{"completed": true, "cancelled": true}

// Service raises and resolves disputes, transactionally moving the parent
// order alongside the dispute row, and notifying the affected parties after
// commit.
type Service struct {
	q        *sqlc.Queries
	pool     *pgxpool.Pool
	notifier *notify.Notifier
	logger   zerolog.Logger
}

// NewService builds the disputes service.
func NewService(q *sqlc.Queries, pool *pgxpool.Pool, notifier *notify.Notifier, logger zerolog.Logger) *Service {
	return &Service{q: q, pool: pool, notifier: notifier, logger: logger}
}

// Raise escalates orderID on behalf of raisedByCompanyID (a party to it),
// moving the order to disputed and appending an order_event, all inside one
// transaction. The counterparty is notified after commit.
func (s *Service) Raise(ctx context.Context, orderID, raisedByCompanyID, reason, detail string) (sqlc.Dispute, error) {
	if !validReasons[reason] {
		return sqlc.Dispute{}, ValidationError{Msg: "invalid reason"}
	}

	order, err := s.q.GetOrder(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Dispute{}, ErrNotFound
	} else if err != nil {
		return sqlc.Dispute{}, err
	}

	var role string
	switch raisedByCompanyID {
	case order.BuyerCompanyID:
		role = "buyer"
	case order.SellerCompanyID:
		role = "seller"
	default:
		// Not a party to this order — don't leak existence across tenants.
		return sqlc.Dispute{}, ErrNotFound
	}

	if !disputableStatuses[order.Status] {
		return sqlc.Dispute{}, ConflictError{Msg: "order is not in a disputable state"}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sqlc.Dispute{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	dispute, err := qtx.CreateDispute(ctx, sqlc.CreateDisputeParams{
		OrderID:           orderID,
		RaisedByCompanyID: raisedByCompanyID,
		Reason:            reason,
		Detail:            detail,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return sqlc.Dispute{}, ConflictError{Msg: "a dispute is already open for this order"}
		}
		return sqlc.Dispute{}, err
	}

	fromStatus := order.Status
	updated, err := qtx.SetOrderStatus(ctx, sqlc.SetOrderStatusParams{ID: orderID, Status: "disputed", FromStatus: fromStatus})
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent transition moved the order out of fromStatus between our
		// read and this write — the validated read is stale.
		return sqlc.Dispute{}, ConflictError{Msg: "order was concurrently modified"}
	} else if err != nil {
		return sqlc.Dispute{}, err
	}

	if _, err := qtx.CreateOrderEvent(ctx, sqlc.CreateOrderEventParams{
		OrderID:        orderID,
		FromStatus:     fromStatus,
		ToStatus:       "disputed",
		Actor:          role,
		ActorCompanyID: &raisedByCompanyID,
		Note:           reason,
	}); err != nil {
		return sqlc.Dispute{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Dispute{}, err
	}

	if s.notifier != nil {
		counterparty := updated.SellerCompanyID
		if role == "seller" {
			counterparty = updated.BuyerCompanyID
		}
		s.notifier.NotifyCompany(ctx, counterparty, notify.Event{
			Type:     notify.TypeDisputeOpened,
			Title:    "Dispute opened",
			Body:     "A dispute was opened on your order.",
			Data:     map[string]any{"order_id": orderID, "dispute_id": dispute.ID},
			CTALabel: "View order",
			CTAPath:  "/orders/" + orderID,
		})
	}

	return dispute, nil
}

// ListForOrder returns the disputes raised on orderID, scoped to a caller
// who is a party to it.
func (s *Service) ListForOrder(ctx context.Context, orderID, companyID string) ([]sqlc.Dispute, error) {
	order, err := s.q.GetOrder(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if order.BuyerCompanyID != companyID && order.SellerCompanyID != companyID {
		return nil, ErrNotFound
	}
	return s.q.ListDisputesForOrder(ctx, orderID)
}

// ListForAdmin returns disputes for the platform-admin queue, optionally
// filtered by status ("open"/"resolved"/"rejected"); an empty status
// returns all of them.
func (s *Service) ListForAdmin(ctx context.Context, status string) ([]sqlc.Dispute, error) {
	var narg *string
	if status != "" {
		narg = &status
	}
	return s.q.ListDisputes(ctx, narg)
}

// Resolve applies an admin's resolution to disputeID: it flips the dispute
// to a terminal status, moves the parent order out of disputed to orderTo,
// appends an order_event, and writes an audit_logs row — all inside one
// transaction. Both parties are notified after commit. A dispute that is
// already resolved/rejected (0 rows on the conditional UPDATE) is a 409, so
// a double-resolve is safe.
func (s *Service) Resolve(ctx context.Context, disputeID, adminID, outcome, orderTo, resolution string) (sqlc.Dispute, error) {
	if !validOutcomes[outcome] {
		return sqlc.Dispute{}, ValidationError{Msg: "invalid outcome"}
	}
	if !validOrderTargets[orderTo] {
		return sqlc.Dispute{}, ValidationError{Msg: "invalid order target"}
	}

	if _, err := s.q.GetDispute(ctx, disputeID); errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Dispute{}, ErrNotFound
	} else if err != nil {
		return sqlc.Dispute{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sqlc.Dispute{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := s.q.WithTx(tx)

	resolved, err := qtx.ResolveDispute(ctx, sqlc.ResolveDisputeParams{
		ID:                disputeID,
		Status:            outcome,
		Resolution:        resolution,
		ResolvedByAdminID: &adminID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Dispute{}, ConflictError{Msg: "dispute is not open"}
	} else if err != nil {
		return sqlc.Dispute{}, err
	}

	order, err := qtx.GetOrder(ctx, resolved.OrderID)
	if err != nil {
		return sqlc.Dispute{}, err
	}

	updatedOrder, err := qtx.SetOrderStatus(ctx, sqlc.SetOrderStatusParams{ID: order.ID, Status: orderTo, FromStatus: "disputed"})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Dispute{}, ConflictError{Msg: "order was concurrently modified"}
	} else if err != nil {
		return sqlc.Dispute{}, err
	}

	if _, err := qtx.CreateOrderEvent(ctx, sqlc.CreateOrderEventParams{
		OrderID:    order.ID,
		FromStatus: "disputed",
		ToStatus:   orderTo,
		Actor:      "admin",
		Note:       resolution,
	}); err != nil {
		return sqlc.Dispute{}, err
	}

	detail, err := json.Marshal(map[string]any{
		"outcome": outcome, "order_to": orderTo, "resolution": resolution,
	})
	if err != nil {
		return sqlc.Dispute{}, err
	}
	targetID := disputeID
	if _, err := qtx.CreateAuditLog(ctx, sqlc.CreateAuditLogParams{
		CompanyID: order.BuyerCompanyID,
		Actor:     adminID,
		Action:    "dispute.resolved",
		TargetID:  &targetID,
		Detail:    detail,
	}); err != nil {
		return sqlc.Dispute{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return sqlc.Dispute{}, err
	}

	if s.notifier != nil {
		s.notifier.NotifyCompany(ctx, updatedOrder.BuyerCompanyID, notify.Event{
			Type:     notify.TypeDisputeResolved,
			Title:    "Dispute resolved",
			Body:     "The dispute on your order was resolved.",
			Data:     map[string]any{"order_id": order.ID, "dispute_id": disputeID, "outcome": outcome},
			CTALabel: "View order",
			CTAPath:  "/orders/" + order.ID,
		})
		s.notifier.NotifyCompany(ctx, updatedOrder.SellerCompanyID, notify.Event{
			Type:     notify.TypeDisputeResolved,
			Title:    "Dispute resolved",
			Body:     "The dispute on your order was resolved.",
			Data:     map[string]any{"order_id": order.ID, "dispute_id": disputeID, "outcome": outcome},
			CTALabel: "View order",
			CTAPath:  "/orders/" + order.ID,
		})
	}

	return resolved, nil
}

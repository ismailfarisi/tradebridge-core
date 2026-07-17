// Package reviews lets either party to a completed order leave a 1-5 star
// review of the other. direction and reviewee are always derived server-side
// from which party the reviewer is — the client's word for either is never
// trusted. A review is allowed only on a 'completed' order, only by a party
// to it, and only once per party per order (UNIQUE(order_id,
// reviewer_company_id), mapped to a 409 conflict). An admin may remove a
// review, which excludes it from reputation aggregates and public listings.
package reviews

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// ErrNotFound is returned when an order is missing, or the caller is not a
// party to it (the two cases are indistinguishable on purpose, so a
// non-party can't probe for an order's existence).
var ErrNotFound = errors.New("order not found")

// ConflictError signals an illegal review action (HTTP 409): the order isn't
// completed yet, or this party already reviewed this order.
type ConflictError struct{ Msg string }

func (e ConflictError) Error() string { return e.Msg }

// ForbiddenError signals the caller may not perform this action (HTTP 403).
type ForbiddenError struct{ Msg string }

func (e ForbiddenError) Error() string { return e.Msg }

// ValidationError signals bad input (HTTP 400).
type ValidationError struct{ Msg string }

func (e ValidationError) Error() string { return e.Msg }

// Service creates, lists, and aggregates reviews, and lets an admin remove one.
type Service struct {
	q        *sqlc.Queries
	notifier *notify.Notifier
	logger   zerolog.Logger
}

// NewService builds the reviews service.
func NewService(q *sqlc.Queries, notifier *notify.Notifier, logger zerolog.Logger) *Service {
	return &Service{q: q, notifier: notifier, logger: logger}
}

// Create records a review of the counterparty on a completed order.
// reviewerCompanyID must be a party (buyer or seller) to orderID; direction
// and reviewee are derived from that role, never from client input.
func (s *Service) Create(ctx context.Context, orderID, reviewerCompanyID string, rating int, body string) (sqlc.Review, error) {
	if rating < 1 || rating > 5 {
		return sqlc.Review{}, ValidationError{Msg: "rating must be between 1 and 5"}
	}

	order, err := s.q.GetOrder(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Review{}, ErrNotFound
	} else if err != nil {
		return sqlc.Review{}, err
	}

	var reviewee, direction string
	switch reviewerCompanyID {
	case order.BuyerCompanyID:
		reviewee = order.SellerCompanyID
		direction = "buyer_on_seller"
	case order.SellerCompanyID:
		reviewee = order.BuyerCompanyID
		direction = "seller_on_buyer"
	default:
		// Not a party to this order — don't leak existence across tenants.
		return sqlc.Review{}, ErrNotFound
	}

	if order.Status != "completed" {
		return sqlc.Review{}, ConflictError{Msg: "order is not completed"}
	}

	review, err := s.q.CreateReview(ctx, sqlc.CreateReviewParams{
		OrderID:           orderID,
		ReviewerCompanyID: reviewerCompanyID,
		RevieweeCompanyID: reviewee,
		Direction:         direction,
		Rating:            int32(rating),
		Body:              body,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return sqlc.Review{}, ConflictError{Msg: "already reviewed this order"}
		}
		return sqlc.Review{}, err
	}

	if s.notifier != nil {
		s.notifier.NotifyCompany(ctx, reviewee, notify.Event{
			Type:     notify.TypeReviewReceived,
			Title:    "New review",
			Body:     "You received a " + strconv.Itoa(rating) + "-star review.",
			Data:     map[string]any{"order_id": orderID, "rating": rating},
			CTAPath:  "/orders/" + orderID,
			CTALabel: "View order",
		})
	}

	return review, nil
}

// ListForOrder returns the reviews left on orderID, scoped to a caller who is
// a party to it.
func (s *Service) ListForOrder(ctx context.Context, orderID, companyID string) ([]sqlc.Review, error) {
	order, err := s.q.GetOrder(ctx, orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if order.BuyerCompanyID != companyID && order.SellerCompanyID != companyID {
		return nil, ErrNotFound
	}
	return s.q.ListReviewsForOrder(ctx, orderID)
}

// Reputation returns companyID's average rating and visible review count.
func (s *Service) Reputation(ctx context.Context, companyID string) (avg float64, count int64, err error) {
	rep, err := s.q.GetCompanyReputation(ctx, companyID)
	if err != nil {
		return 0, 0, err
	}
	return rep.AvgRating, rep.ReviewCount, nil
}

// Remove marks a review as removed (admin only), excluding it from
// reputation aggregates and public listings.
func (s *Service) Remove(ctx context.Context, reviewID string) (sqlc.Review, error) {
	review, err := s.q.RemoveReview(ctx, reviewID)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Review{}, ErrNotFound
	}
	return review, err
}

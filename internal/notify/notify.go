// Package notify is the platform's single "reach a human" surface: it turns
// domain occurrences (an RFQ matched, a quote was submitted, an order was
// created, an email needs verifying) into notifications and transactional
// email, so the rest of the codebase never talks to a delivery provider or
// template directly.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

const (
	// TypeRFQMatched fires when a buyer's RFQ matches a seller's catalog.
	TypeRFQMatched = "rfq.matched"
	// TypeQuoteSubmitted fires when a seller submits a quote on an RFQ.
	TypeQuoteSubmitted = "quote.submitted"
	// TypeOrderCreated fires when an order is created from an accepted quote.
	TypeOrderCreated = "order.created"
	// TypeOrderConfirmed fires when a seller confirms an order.
	TypeOrderConfirmed = "order.confirmed"
	// TypeOrderFulfilled fires when a seller marks an order fulfilled/shipped.
	TypeOrderFulfilled = "order.fulfilled"
	// TypeOrderCompleted fires when a buyer marks an order completed.
	TypeOrderCompleted = "order.completed"
	// TypeOrderCancelled fires when either party cancels an order.
	TypeOrderCancelled = "order.cancelled"
	// TypeEmailVerify fires when a user needs to verify their email address.
	TypeEmailVerify = "email.verify"
	// TypeInvoiceIssued fires when an invoice is generated for a new order.
	TypeInvoiceIssued = "invoice.issued"
	// TypeReviewReceived fires when a company receives a review from its
	// counterparty on a completed order.
	TypeReviewReceived = "review.received"
	// TypeDisputeOpened fires when a party raises a dispute on an order,
	// notifying the counterparty.
	TypeDisputeOpened = "dispute.opened"
	// TypeDisputeResolved fires when an admin resolves a dispute, notifying
	// both parties.
	TypeDisputeResolved = "dispute.resolved"
)

// Event is a single thing worth telling a company about.
type Event struct {
	Type     string
	Title    string
	Body     string
	Data     map[string]any
	CTALabel string
	CTAPath  string
}

// Notifier turns Events into in-app notification rows and, subject to each
// user's preferences, transactional email. It is the platform's single
// "reach a human" surface: callers never talk to sqlc, a Sender, or a
// template directly.
type Notifier struct {
	q       *sqlc.Queries
	sender  Sender
	baseURL string
	logger  zerolog.Logger
}

// New builds a Notifier. sender may be nil, in which case email is skipped
// entirely (SendTransactional becomes a no-op and NotifyCompany only writes
// in-app rows).
func New(q *sqlc.Queries, sender Sender, baseURL string, logger zerolog.Logger) *Notifier {
	return &Notifier{q: q, sender: sender, baseURL: baseURL, logger: logger}
}

// NotifyCompany fans an Event out to every user at companyID, honoring each
// user's per-event-type notification preferences. It is a best-effort side
// effect: it never returns an error, only logs warnings on failure, so a
// notification problem never fails the request that triggered it.
func (n *Notifier) NotifyCompany(ctx context.Context, companyID string, ev Event) {
	users, err := n.q.ListUsersByCompany(ctx, companyID)
	if err != nil {
		n.logger.Warn().Err(err).Str("company_id", companyID).Msg("notify: list users by company failed")
		return
	}

	dataJSON := n.encodeData(ev)

	var emailTo []string
	for _, u := range users {
		email, inApp := n.channels(ctx, u.ID, ev.Type)
		if inApp {
			if _, err := n.q.CreateNotification(ctx, sqlc.CreateNotificationParams{
				UserID:    u.ID,
				CompanyID: companyID,
				Type:      ev.Type,
				Title:     ev.Title,
				Body:      ev.Body,
				Data:      dataJSON,
			}); err != nil {
				n.logger.Warn().Err(err).Str("user_id", u.ID).Msg("notify: create notification failed")
				continue
			}
		}
		if email && n.sender != nil {
			emailTo = append(emailTo, u.Email)
		}
	}

	if len(emailTo) > 0 {
		html, text := renderEventEmail(ev, n.baseURL)
		n.dispatchEmail(Email{To: emailTo, Subject: ev.Title, HTML: html, Text: text})
	}
}

// SendTransactional sends a one-off transactional email (e.g. verification)
// that isn't tied to a company fan-out or in-app notification. A nil sender
// makes this a no-op.
func (n *Notifier) SendTransactional(to, subject, html, text string) {
	if n.sender == nil {
		return
	}
	n.dispatchEmail(Email{To: []string{to}, Subject: subject, HTML: html, Text: text})
}

// BaseURL returns the base URL used to build links in rendered emails.
func (n *Notifier) BaseURL() string {
	return n.baseURL
}

// channels reports whether userID wants email and/or in-app delivery for
// eventType. Absent a stored preference (pgx.ErrNoRows) or on any lookup
// error, it defaults to both channels on, so notifications are never
// silently dropped by a preferences-lookup hiccup.
func (n *Notifier) channels(ctx context.Context, userID, eventType string) (email, inApp bool) {
	pref, err := n.q.GetNotificationPreference(ctx, sqlc.GetNotificationPreferenceParams{
		UserID:    userID,
		EventType: eventType,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			n.logger.Warn().Err(err).Str("user_id", userID).Str("event_type", eventType).Msg("notify: get notification preference failed")
		}
		return true, true
	}
	return pref.Email, pref.InApp
}

// encodeData folds ev.Data plus the CTA (if any) into the JSON blob stored
// alongside a notification row. It returns nil if there is nothing to store.
func (n *Notifier) encodeData(ev Event) []byte {
	data := ev.Data
	if ev.CTAPath != "" {
		if data == nil {
			data = map[string]any{}
		} else {
			data = maps.Clone(data)
		}
		data["cta_label"] = ev.CTALabel
		data["cta_path"] = ev.CTAPath
	}
	if data == nil {
		return nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		n.logger.Warn().Err(err).Msg("notify: marshal notification data failed")
		return nil
	}
	return b
}

// dispatchEmail sends e in the background, detached from the request
// context, so a slow email provider never adds latency to the caller. It
// touches no database state — only the strings in e — so it is safe to
// outlive the request.
func (n *Notifier) dispatchEmail(e Email) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				n.logger.Error().Interface("panic", r).Strs("to", e.To).Msg("notify: email send panicked")
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := n.sender.Send(ctx, e); err != nil {
			n.logger.Warn().Err(err).Strs("to", e.To).Msg("notify: send email failed")
		}
	}()
}

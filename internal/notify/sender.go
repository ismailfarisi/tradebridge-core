// Package notify provides provider-agnostic transactional email delivery.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Email is a single transactional message to send.
type Email struct {
	To      []string
	Subject string
	HTML    string
	Text    string
}

// Sender delivers an Email. Implementations must be safe for concurrent use.
type Sender interface {
	Send(ctx context.Context, e Email) error
}

// NewSender chooses the Sender implementation: LogSender when resendAPIKey is
// empty (dev/CI never break without a provider), otherwise a ResendSender.
func NewSender(resendAPIKey, fromAddr string, logger zerolog.Logger) Sender {
	if resendAPIKey == "" {
		return LogSender{logger: logger}
	}
	return &ResendSender{
		apiKey: resendAPIKey,
		from:   fromAddr,
		http:   &http.Client{Timeout: 15 * time.Second},
		logger: logger,
	}
}

// LogSender logs the email instead of sending it. It is the default when no
// email provider is configured, so dev/CI environments never break.
type LogSender struct {
	logger zerolog.Logger
}

// Send logs the recipient(s) and subject at info level and returns nil.
func (s LogSender) Send(_ context.Context, e Email) error {
	s.logger.Info().Strs("to", e.To).Str("subject", e.Subject).Msg("email (logged, not sent)")
	return nil
}

// ResendSender sends email via the Resend API (https://resend.com).
type ResendSender struct {
	apiKey string
	from   string
	http   *http.Client
	logger zerolog.Logger
}

// resendRequest is the JSON body posted to https://api.resend.com/emails.
type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html,omitempty"`
	Text    string   `json:"text,omitempty"`
}

// Send POSTs the email to the Resend API.
func (s *ResendSender) Send(ctx context.Context, e Email) error {
	body, err := json.Marshal(resendRequest{
		From:    s.from,
		To:      e.To,
		Subject: e.Subject,
		HTML:    e.HTML,
		Text:    e.Text,
	})
	if err != nil {
		return fmt.Errorf("notify: marshal resend request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify: build resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("notify: resend request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snip, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("notify: resend returned status %d: %s", resp.StatusCode, string(snip))
	}
	return nil
}

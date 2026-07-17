package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/notify"
	"github.com/tradebridge/core/internal/quotes"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// supportedCommands is the closed set of commands an agent may send.
var supportedCommands = map[string]bool{
	"quote.submit":          true,
	"clarification.request": true,
	"no_bid":                true,
	"quote.counter":         true,
}

type commandReq struct {
	Type           string          `json:"type"`
	IdempotencyKey string          `json:"idempotency_key"`
	RFQID          string          `json:"rfq_id"`
	Payload        json.RawMessage `json:"payload"`
}

// command validates and records an agent command. It enforces the command
// vocabulary, tenant authorization (the agent must be matched to the RFQ it
// acts on), and idempotency, then writes an audit-log entry.
func (h *Handler) command(c *fiber.Ctx) error {
	agent, err := h.authenticate(c)
	if err != nil {
		return err
	}

	var req commandReq
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if !supportedCommands[req.Type] {
		return fiber.NewError(fiber.StatusBadRequest, "unsupported command type")
	}
	if req.IdempotencyKey == "" {
		return fiber.NewError(fiber.StatusBadRequest, "idempotency_key required")
	}

	payload := req.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}

	// Authorization: any command naming an RFQ requires the agent's company to
	// be a matched seller for it. quote.submit must name an RFQ. quote.counter
	// carries a quote_id (not rfq_id) in its payload, so its RFQ — and the
	// stronger check that the quote actually belongs to this agent's
	// company — must be derived from the quote itself.
	var rfqID *string
	var counterQuote sqlc.Quote
	if req.Type == "quote.counter" {
		var qp struct {
			QuoteID string `json:"quote_id"`
		}
		if err := json.Unmarshal(payload, &qp); err != nil || qp.QuoteID == "" {
			return fiber.NewError(fiber.StatusBadRequest, "quote.counter requires quote_id")
		}
		counterQuote, err = h.q.GetQuote(c.Context(), qp.QuoteID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fiber.NewError(fiber.StatusNotFound, "quote not found")
		} else if err != nil {
			return err
		}
		if counterQuote.SellerCompanyID != agent.CompanyID {
			// Never trust the client-set quote_id to belong to this agent.
			return fiber.NewError(fiber.StatusForbidden, "not your quote")
		}
		matched, err := h.q.RFQMatchExists(c.Context(), sqlc.RFQMatchExistsParams{
			RfqID: counterQuote.RfqID, SellerCompanyID: agent.CompanyID,
		})
		if err != nil {
			return err
		}
		if !matched {
			return fiber.NewError(fiber.StatusForbidden, "not matched to this rfq")
		}
		rfqID = &counterQuote.RfqID
		req.RFQID = counterQuote.RfqID // so the audit trail records the derived rfq_id
	} else if req.RFQID != "" {
		matched, err := h.q.RFQMatchExists(c.Context(), sqlc.RFQMatchExistsParams{
			RfqID: req.RFQID, SellerCompanyID: agent.CompanyID,
		})
		if err != nil {
			return err
		}
		if !matched {
			return fiber.NewError(fiber.StatusForbidden, "not matched to this rfq")
		}
		rfqID = &req.RFQID
	} else if req.Type == "quote.submit" {
		return fiber.NewError(fiber.StatusBadRequest, "quote.submit requires rfq_id")
	}

	// Validate the quote body up front so a malformed quote is rejected before
	// any command row is written.
	var normalized quotes.Normalized
	if req.Type == "quote.submit" || req.Type == "quote.counter" {
		normalized, err = quotes.Normalize(payload)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
	}

	cmd, err := h.q.CreateAgentCommand(c.Context(), sqlc.CreateAgentCommandParams{
		AgentCredentialID: agent.CredentialID,
		CompanyID:         agent.CompanyID,
		RfqID:             rfqID,
		Type:              req.Type,
		IdempotencyKey:    req.IdempotencyKey,
		Payload:           payload,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Duplicate idempotency key — return the original, unchanged.
		existing, err := h.q.GetAgentCommandByKey(c.Context(), sqlc.GetAgentCommandByKeyParams{
			AgentCredentialID: agent.CredentialID,
			IdempotencyKey:    req.IdempotencyKey,
		})
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"id": existing.ID, "status": existing.Status, "idempotent": true})
	} else if err != nil {
		return err
	}

	// Side effects by command type.
	switch req.Type {
	case "quote.submit":
		if _, err := quotes.Record(c.Context(), h.q, h.pool, *rfqID, agent.CompanyID, cmd.ID, normalized); err != nil {
			return err
		}
		h.maybeQuote(c.Context(), *rfqID)
		if h.notifier != nil {
			if r, err := h.q.GetRFQ(c.Context(), *rfqID); err == nil {
				n, _ := h.q.CountSubmittedQuotesByRFQ(c.Context(), *rfqID)
				h.notifier.NotifyCompany(c.Context(), r.BuyerCompanyID, notify.Event{
					Type:     notify.TypeQuoteSubmitted,
					Title:    quoteCountTitle(n),
					Body:     "Open your RFQ to compare quotes.",
					Data:     map[string]any{"rfq_id": r.ID},
					CTALabel: "Compare quotes",
					CTAPath:  "/rfqs/" + r.ID,
				})
			}
		}
	case "clarification.request":
		if rfqID != nil {
			h.threadClarification(c.Context(), *rfqID, agent.CompanyID, payload)
		}
	case "quote.counter":
		head, _, err := quotes.RecordCounter(c.Context(), h.q, h.pool, counterQuote.ID, cmd.ID, normalized)
		if err != nil {
			return err
		}
		// Thread the seller's counter as a message too, so the buyer sees the
		// human-readable ask alongside the new head terms.
		sellerBody := normalized.Notes
		if sellerBody == "" {
			sellerBody = fmt.Sprintf("New offer: %.2f %s", float64(normalized.PriceCents)/100, normalized.Currency)
		}
		if _, err := h.q.CreateQuoteMessage(c.Context(), sqlc.CreateQuoteMessageParams{
			QuoteID: head.ID, Author: "seller", Body: sellerBody, Attachments: nil,
		}); err != nil {
			h.logger.Warn().Err(err).Str("quote_id", head.ID).Msg("failed to thread seller counter")
		}
		if h.notifier != nil {
			if r, err := h.q.GetRFQ(c.Context(), head.RfqID); err == nil {
				h.notifier.NotifyCompany(c.Context(), r.BuyerCompanyID, notify.Event{
					Type:     notify.TypeQuoteCountered,
					Title:    "Seller sent a counter-offer",
					Body:     "The seller revised their quote.",
					Data:     map[string]any{"rfq_id": r.ID, "quote_id": head.ID},
					CTALabel: "View thread",
					CTAPath:  "/rfqs/" + r.ID,
				})
			}
		}
	}

	h.audit(c.Context(), agent, "command."+req.Type, cmd.ID, req)

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": cmd.ID, "status": cmd.Status})
}

// threadClarification appends the agent's clarification as a seller message on
// its existing quote, if one exists. Best-effort; never fails the command.
func (h *Handler) threadClarification(ctx context.Context, rfqID, sellerCompanyID string, payload []byte) {
	q, err := h.q.GetQuoteBySeller(ctx, sqlc.GetQuoteBySellerParams{RfqID: rfqID, SellerCompanyID: sellerCompanyID})
	if err != nil {
		return // no quote yet — nothing to thread onto
	}
	var body struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(payload, &body)
	if body.Message == "" {
		return
	}
	if _, err := h.q.CreateQuoteMessage(ctx, sqlc.CreateQuoteMessageParams{
		QuoteID: q.ID, Author: "seller", Body: body.Message, Attachments: nil,
	}); err != nil {
		h.logger.Warn().Err(err).Str("quote_id", q.ID).Msg("failed to thread clarification")
	}
}

// maybeQuote advances an RFQ to `quoted` when a quote is submitted, if the
// transition is legal (open → quoted). Best-effort; never fails the command.
func (h *Handler) maybeQuote(ctx context.Context, rfqID string) {
	r, err := h.q.GetRFQ(ctx, rfqID)
	if err != nil {
		return
	}
	if rfq.CanTransition(r.Status, rfq.StatusQuoted) {
		if _, err := h.q.SetRFQStatus(ctx, sqlc.SetRFQStatusParams{ID: rfqID, Status: rfq.StatusQuoted}); err != nil {
			h.logger.Warn().Err(err).Str("rfq_id", rfqID).Msg("failed to advance rfq to quoted")
		}
	}
}

// quoteCountTitle renders the buyer-facing notification title for a
// quote.submitted event, singular for the first quote and pluralized with
// the running count thereafter.
func quoteCountTitle(n int64) string {
	if n <= 1 {
		return "A supplier quoted your RFQ"
	}
	return fmt.Sprintf("%d suppliers quoted your RFQ", n)
}

// audit writes one append-only audit row. A logging failure must not fail the
// command, so errors are logged, not returned.
func (h *Handler) audit(ctx context.Context, agent authedAgent, action, targetID string, req commandReq) {
	detail, _ := json.Marshal(fiber.Map{
		"type":            req.Type,
		"rfq_id":          req.RFQID,
		"idempotency_key": req.IdempotencyKey,
	})
	target := targetID
	if _, err := h.q.CreateAuditLog(ctx, sqlc.CreateAuditLogParams{
		CompanyID: agent.CompanyID,
		Actor:     agent.CredentialID,
		Action:    action,
		TargetID:  &target,
		Detail:    detail,
	}); err != nil {
		h.logger.Warn().Err(err).Str("action", action).Msg("audit log write failed")
	}
}

// ack marks a delivered event acknowledged so it is not replayed on reconnect.
func (h *Handler) ack(c *fiber.Ctx) error {
	agent, err := h.authenticate(c)
	if err != nil {
		return err
	}
	seq := parseSeq(c.Params("seq"))
	if seq == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid event seq")
	}
	if _, err := h.q.AckAgentEvent(c.Context(), sqlc.AckAgentEventParams{
		CompanyID: agent.CompanyID, Seq: seq,
	}); errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "event not found")
	} else if err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

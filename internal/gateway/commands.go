package gateway

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/internal/quotes"
	"github.com/tradebridge/core/internal/rfq"
	"github.com/tradebridge/core/pkg/db/sqlc"
)

// supportedCommands is the closed set of commands an agent may send.
var supportedCommands = map[string]bool{
	"quote.submit":          true,
	"clarification.request": true,
	"no_bid":                true,
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

	// Authorization: any command naming an RFQ requires the agent's company to
	// be a matched seller for it. quote.submit must name an RFQ.
	var rfqID *string
	if req.RFQID != "" {
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

	payload := req.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}

	// Validate the quote body up front so a malformed quote is rejected before
	// any command row is written.
	var normalized quotes.Normalized
	if req.Type == "quote.submit" {
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
		if _, err := quotes.Record(c.Context(), h.q, *rfqID, agent.CompanyID, cmd.ID, normalized); err != nil {
			return err
		}
		h.maybeQuote(c.Context(), *rfqID)
	case "clarification.request":
		if rfqID != nil {
			h.threadClarification(c.Context(), *rfqID, agent.CompanyID, payload)
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

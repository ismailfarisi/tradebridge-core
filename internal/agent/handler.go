// Package agent (Core-side) issues and manages the credentials a seller's agent
// uses to authenticate to the Gateway. The agent-facing connect/auth exchange
// lives in the Gateway (Sprint A3); this is the issuance surface.
package agent

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/gofiber/fiber/v2"

	"github.com/tradebridge/core/internal/identity"
	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves agent-credential issuance + management routes.
type Handler struct {
	q *sqlc.Queries
}

// NewHandler builds the agent-credentials handler over the query layer.
func NewHandler(q *sqlc.Queries) *Handler {
	return &Handler{q: q}
}

// Register mounts the agent-credential routes under the given tenant guard.
func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/agent-credentials", guard...)
	g.Get("/", h.list)
	g.Post("/", middleware.RequireRole("owner", "admin"), h.issue)
	g.Post("/:id/revoke", middleware.RequireRole("owner", "admin"), h.revoke)
}

type issueReq struct {
	Label string `json:"label"`
}

func (h *Handler) issue(c *fiber.Ctx) error {
	var req issueReq
	_ = c.BodyParser(&req)
	if req.Label == "" {
		req.Label = "default"
	}

	rawSecret, err := randomHex(32)
	if err != nil {
		return err
	}
	hash, err := identity.HashPassword(rawSecret)
	if err != nil {
		return err
	}
	cred, err := h.q.CreateAgentCredential(c.Context(), sqlc.CreateAgentCredentialParams{
		CompanyID:  middleware.CompanyID(c),
		Label:      req.Label,
		SecretHash: hash,
	})
	if err != nil {
		return err
	}

	// The full secret (`<id>.<raw>`) is shown exactly once — only the hash is stored.
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":     cred.ID,
		"label":  cred.Label,
		"secret": cred.ID + "." + rawSecret,
	})
}

type credResp struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	LastSeen any    `json:"last_seen"`
}

func (h *Handler) list(c *fiber.Ctx) error {
	creds, err := h.q.ListAgentCredentialsByCompany(c.Context(), middleware.CompanyID(c))
	if err != nil {
		return err
	}
	out := make([]credResp, len(creds))
	for i, cr := range creds {
		var last any
		if cr.LastSeen.Valid {
			last = cr.LastSeen.Time
		}
		out[i] = credResp{ID: cr.ID, Label: cr.Label, Status: cr.Status, LastSeen: last}
	}
	return c.JSON(fiber.Map{"credentials": out})
}

func (h *Handler) revoke(c *fiber.Ctx) error {
	err := h.q.RevokeAgentCredential(c.Context(), sqlc.RevokeAgentCredentialParams{
		ID:        c.Params("id"),
		CompanyID: middleware.CompanyID(c),
	})
	if err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

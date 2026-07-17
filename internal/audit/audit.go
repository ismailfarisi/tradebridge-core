// Package audit writes and reads the append-only audit_logs trail. The trail is
// shared: the gateway writes agent-command rows; Admin writes platform-admin
// action rows. The viewer (handler.go) reads both.
package audit

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type auditWriter interface {
	CreateAuditLog(ctx context.Context, arg sqlc.CreateAuditLogParams) (sqlc.AuditLog, error)
}

// Auditor records platform-admin actions. Construct one per process.
type Auditor struct {
	q      auditWriter
	logger zerolog.Logger
}

// New builds an Auditor. *sqlc.Queries satisfies auditWriter.
func New(q auditWriter, logger zerolog.Logger) *Auditor {
	return &Auditor{q: q, logger: logger}
}

// Admin writes one append-only row for a platform-admin action against a target
// company. Best-effort: a write failure is logged, never returned.
func (a *Auditor) Admin(ctx context.Context, adminEmail, action, targetCompanyID string, detail map[string]any) {
	var payload []byte
	if detail == nil {
		payload = []byte("{}")
	} else if p, err := json.Marshal(detail); err == nil {
		payload = p
	} else {
		payload = []byte("{}")
	}
	target := targetCompanyID
	if _, err := a.q.CreateAuditLog(ctx, sqlc.CreateAuditLogParams{
		CompanyID: targetCompanyID,
		Actor:     "admin:" + adminEmail,
		Action:    action,
		TargetID:  &target,
		Detail:    payload,
	}); err != nil {
		a.logger.Warn().Err(err).Str("action", action).Msg("admin audit write failed")
	}
}

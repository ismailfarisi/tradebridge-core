package audit

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

type fakeWriter struct {
	last sqlc.CreateAuditLogParams
	n    int
	err  error
}

func (f *fakeWriter) CreateAuditLog(_ context.Context, arg sqlc.CreateAuditLogParams) (sqlc.AuditLog, error) {
	f.last = arg
	f.n++
	return sqlc.AuditLog{}, f.err
}

func TestAdminWritesRow(t *testing.T) {
	fw := &fakeWriter{}
	New(fw, zerolog.Nop()).Admin(context.Background(), "ops@x.com", "company.suspend", "co_1", map[string]any{"reason": "fraud"})
	if fw.n != 1 {
		t.Fatalf("want 1 write, got %d", fw.n)
	}
	if fw.last.Actor != "admin:ops@x.com" {
		t.Fatalf("actor = %q", fw.last.Actor)
	}
	if fw.last.CompanyID != "co_1" || fw.last.Action != "company.suspend" {
		t.Fatalf("bad row: %+v", fw.last)
	}
	if fw.last.TargetID == nil || *fw.last.TargetID != "co_1" {
		t.Fatalf("target = %v", fw.last.TargetID)
	}
}

func TestAdminSwallowsWriteError(t *testing.T) {
	fw := &fakeWriter{err: context.DeadlineExceeded}
	// Must not panic and must not surface the error (best-effort).
	New(fw, zerolog.Nop()).Admin(context.Background(), "ops@x.com", "x", "co_1", nil)
	if fw.n != 1 {
		t.Fatalf("want attempted write")
	}
}

func TestAdminNilDetailIsEmptyObject(t *testing.T) {
	fw := &fakeWriter{}
	New(fw, zerolog.Nop()).Admin(context.Background(), "ops@x.com", "x", "co_1", nil)
	if string(fw.last.Detail) != "{}" {
		t.Fatalf("detail = %q, want %q", string(fw.last.Detail), "{}")
	}
}

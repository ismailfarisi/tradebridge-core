// Package attachments stores buyer-uploaded RFQ files (images + PDFs): bytes
// on local disk behind Store (swap for S3 later without touching callers),
// metadata in Postgres, ownership-checked loading via Loader.
package attachments

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
)

const (
	MaxFileBytes  = 10 << 20 // 10 MB per file
	MaxPerMessage = 5
)

// Meta is the wire shape for attachment metadata in API responses, rfqs
// messages JSONB, quote_messages.attachments JSONB, and agent event payloads.
type Meta struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"size_bytes"`
}

// Loaded is Meta plus the file bytes, ready for an LLM call.
type Loaded struct {
	Meta
	Data []byte
}

func MetaOf(a sqlc.Attachment) Meta {
	return Meta{ID: a.ID, Filename: a.Filename, Mime: a.Mime, SizeBytes: a.SizeBytes}
}

var allowed = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
	"image/gif":       true,
	"application/pdf": true,
}

// Sniff detects the MIME type from magic bytes; the client header is never
// trusted.
func Sniff(data []byte) string {
	mt := http.DetectContentType(data)
	if i := strings.Index(mt, ";"); i >= 0 {
		mt = mt[:i]
	}
	return strings.TrimSpace(mt)
}

func Allowed(mime string) bool { return allowed[mime] }

// ResolveDir returns the configured dir, or ~/tradebridge-data/attachments.
func ResolveDir(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "tradebridge-data", "attachments"), nil
}

// Store is the disk blob store. IDs are DB-generated ULIDs — callers must
// pass IDs from an attachments row (never raw user input) so path traversal
// is impossible.
type Store struct{ dir string }

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(id string) string { return filepath.Join(s.dir, id) }

func (s *Store) Put(id string, data []byte) error { return os.WriteFile(s.path(id), data, 0o644) }
func (s *Store) Get(id string) ([]byte, error)    { return os.ReadFile(s.path(id)) }
func (s *Store) Delete(id string) error           { return os.Remove(s.path(id)) }

// Loader resolves attachment IDs to metadata (+bytes), enforcing that every
// ID was uploaded by companyID.
type Loader struct {
	q     *sqlc.Queries
	store *Store
}

func NewLoader(q *sqlc.Queries, store *Store) *Loader { return &Loader{q: q, store: store} }

func (l *Loader) rowOwned(ctx context.Context, id, companyID string) (sqlc.Attachment, error) {
	row, err := l.q.GetAttachment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, fiber.NewError(fiber.StatusBadRequest, "unknown attachment "+id)
	} else if err != nil {
		return row, err
	}
	if row.CompanyID != companyID {
		return row, fiber.NewError(fiber.StatusForbidden, "attachment does not belong to you")
	}
	return row, nil
}

func (l *Loader) MetasOwned(ctx context.Context, ids []string, companyID string) ([]Meta, error) {
	var out []Meta
	for _, id := range ids {
		row, err := l.rowOwned(ctx, id, companyID)
		if err != nil {
			return nil, err
		}
		out = append(out, MetaOf(row))
	}
	return out, nil
}

func (l *Loader) LoadOwned(ctx context.Context, ids []string, companyID string) ([]Loaded, error) {
	var out []Loaded
	for _, id := range ids {
		row, err := l.rowOwned(ctx, id, companyID)
		if err != nil {
			return nil, err
		}
		data, err := l.store.Get(row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Loaded{Meta: MetaOf(row), Data: data})
	}
	return out, nil
}

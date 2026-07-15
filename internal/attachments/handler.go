package attachments

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"

	"github.com/tradebridge/core/pkg/db/sqlc"
	"github.com/tradebridge/core/pkg/middleware"
)

// Handler serves buyer-facing attachment upload/download.
type Handler struct {
	q     *sqlc.Queries
	store *Store
}

func NewHandler(q *sqlc.Queries, store *Store) *Handler { return &Handler{q: q, store: store} }

func (h *Handler) Register(app *fiber.App, guard ...fiber.Handler) {
	g := app.Group("/v1/attachments", guard...)
	g.Post("/", h.upload)
	g.Get("/:id", h.download)
}

func (h *Handler) upload(c *fiber.Ctx) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "multipart field 'file' required")
	}
	if fh.Size > MaxFileBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "file exceeds 10 MB limit")
	}
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return err
	}
	if len(data) > MaxFileBytes {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, "file exceeds 10 MB limit")
	}
	mime := Sniff(data)
	if !Allowed(mime) {
		return fiber.NewError(fiber.StatusUnsupportedMediaType,
			"only JPEG, PNG, WebP, GIF images and PDFs are accepted")
	}
	sum := sha256.Sum256(data)
	row, err := h.q.CreateAttachment(c.Context(), sqlc.CreateAttachmentParams{
		CompanyID: middleware.CompanyID(c),
		Filename:  filepath.Base(fh.Filename),
		Mime:      mime,
		SizeBytes: int64(len(data)),
		Sha256:    hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return err
	}
	if err := h.store.Put(row.ID, data); err != nil {
		// Compensate so no metadata row points at missing bytes.
		_ = h.q.DeleteAttachment(c.Context(), row.ID)
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(MetaOf(row))
}

// download authorizes: the uploader, the buyer of the referencing RFQ, or a
// matched seller of that RFQ. Everyone else sees 404 (no existence leak).
func (h *Handler) download(c *fiber.Ctx) error {
	row, err := h.q.GetAttachment(c.Context(), c.Params("id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "attachment not found")
	} else if err != nil {
		return err
	}
	me := middleware.CompanyID(c)
	ok := row.CompanyID == me
	if !ok && row.RfqID != nil {
		if r, err := h.q.GetRFQ(c.Context(), *row.RfqID); err == nil && r.BuyerCompanyID == me {
			ok = true
		}
		if !ok {
			matched, err := h.q.SellerMatchedToRFQ(c.Context(), sqlc.SellerMatchedToRFQParams{
				RfqID: *row.RfqID, SellerCompanyID: me,
			})
			ok = err == nil && matched
		}
	}
	if !ok {
		return fiber.NewError(fiber.StatusNotFound, "attachment not found")
	}
	data, err := h.store.Get(row.ID)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "attachment data missing")
	}
	c.Set(fiber.HeaderContentType, row.Mime)
	c.Set(fiber.HeaderContentDisposition, ContentDisposition(row.Filename))
	return c.Send(data)
}

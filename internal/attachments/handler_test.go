package attachments

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

func newTestApp(t *testing.T) *fiber.App {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{BodyLimit: 12 << 20})
	NewHandler(nil, store).Register(app) // nil queries: only pre-DB paths are exercised
	return app
}

func TestUploadRejectsMissingFile(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest("POST", "/v1/attachments", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestUploadRejectsDisallowedMime(t *testing.T) {
	app := newTestApp(t)
	body, ct := multipartBody(t, "file", "evil.html", []byte("<html><body>hi</body></html>"))
	req := httptest.NewRequest("POST", "/v1/attachments", body)
	req.Header.Set("Content-Type", ct)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestUploadRejectsOversize(t *testing.T) {
	app := newTestApp(t)
	big := append([]byte("%PDF-1.4\n"), make([]byte, MaxFileBytes)...) // > 10 MB
	body, ct := multipartBody(t, "file", "big.pdf", big)
	req := httptest.NewRequest("POST", "/v1/attachments", body)
	req.Header.Set("Content-Type", ct)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/require"
)

// uploadPNG posts a tiny valid PNG as multipart and returns the attachment id.
func uploadPNG(t *testing.T, app *fiber.App, token string) map[string]any {
	t.Helper()
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 64)...)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "spec.png")
	require.NoError(t, err)
	_, err = fw.Write(png)
	require.NoError(t, err)
	w.Close()

	req := httptest.NewRequest("POST", "/v1/attachments", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out
}

func TestAttachmentUploadDownload(t *testing.T) {
	app := buildAPI(t)

	// Register company A (the uploader).
	st, regA := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AttachCoA", "company_type": "buyer",
		"email": uniqueEmail("att-a-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	tokenA := regA["token"].(string)

	meta := uploadPNG(t, app, tokenA)
	require.Equal(t, "image/png", meta["mime"])
	id := meta["id"].(string)

	req := httptest.NewRequest("GET", "/v1/attachments/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+tokenA)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
	body, _ := io.ReadAll(resp.Body)
	require.Len(t, body, 72)

	// A different company must get 404 (cross-tenant download authorization).
	st, regB := doJSON(t, app, "POST", "/v1/auth/register", "", map[string]any{
		"company_name": "AttachCoB", "company_type": "buyer",
		"email": uniqueEmail("att-b-"), "password": "supersecret",
	})
	require.Equal(t, http.StatusCreated, st)
	tokenB := regB["token"].(string)

	req2 := httptest.NewRequest("GET", "/v1/attachments/"+id, nil)
	req2.Header.Set("Authorization", "Bearer "+tokenB)
	resp2, err := app.Test(req2, -1)
	require.NoError(t, err)
	require.Equal(t, 404, resp2.StatusCode)
}

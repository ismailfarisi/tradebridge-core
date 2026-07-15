package rfq

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/tradebridge/core/internal/attachments"
)

// userContent builds the Anthropic content blocks for one user turn:
// attachments first (images as vision blocks, PDFs as document blocks), then
// the text.
func userContent(text string, atts []attachments.Loaded) []anthropic.ContentBlockParamUnion {
	var blocks []anthropic.ContentBlockParamUnion
	for _, a := range atts {
		b64 := base64.StdEncoding.EncodeToString(a.Data)
		if a.Mime == "application/pdf" {
			blocks = append(blocks, anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{Data: b64}))
		} else {
			blocks = append(blocks, anthropic.NewImageBlockBase64(a.Mime, b64))
		}
	}
	return append(blocks, anthropic.NewTextBlock(text))
}

// attachmentNote is the text-only degradation for providers without
// vision/document support: append one marker line per attachment.
func attachmentNote(text string, atts []attachments.Loaded) string {
	if len(atts) == 0 {
		return text
	}
	var b strings.Builder
	b.WriteString(text)
	for _, a := range atts {
		fmt.Fprintf(&b, "\n[attached: %s]", a.Filename)
	}
	return b.String()
}

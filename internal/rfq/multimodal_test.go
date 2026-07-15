package rfq

import (
	"testing"

	"github.com/tradebridge/core/internal/attachments"
)

func loadedPNG() attachments.Loaded {
	return attachments.Loaded{
		Meta: attachments.Meta{ID: "a1", Filename: "photo.png", Mime: "image/png", SizeBytes: 3},
		Data: []byte{1, 2, 3},
	}
}

func loadedPDF() attachments.Loaded {
	return attachments.Loaded{
		Meta: attachments.Meta{ID: "a2", Filename: "spec.pdf", Mime: "application/pdf", SizeBytes: 3},
		Data: []byte("%PD"),
	}
}

func TestUserContentBlocks(t *testing.T) {
	blocks := userContent("need 500 units", []attachments.Loaded{loadedPNG(), loadedPDF()})
	if len(blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3 (image, document, text)", len(blocks))
	}
	if blocks[0].OfImage == nil {
		t.Fatal("block 0 should be an image block")
	}
	if blocks[1].OfDocument == nil {
		t.Fatal("block 1 should be a document block")
	}
	if blocks[2].OfText == nil || blocks[2].OfText.Text != "need 500 units" {
		t.Fatal("block 2 should be the text block")
	}
}

func TestUserContentTextOnly(t *testing.T) {
	blocks := userContent("hello", nil)
	if len(blocks) != 1 || blocks[0].OfText == nil {
		t.Fatalf("want single text block, got %+v", blocks)
	}
}

func TestAttachmentNote(t *testing.T) {
	got := attachmentNote("hello", []attachments.Loaded{loadedPDF()})
	want := "hello\n[attached: spec.pdf]"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if attachmentNote("hello", nil) != "hello" {
		t.Fatal("no attachments must leave text unchanged")
	}
}

func TestAnthropicParamsCarryAttachments(t *testing.T) {
	a := anthropicChat{model: "claude-haiku-4-5"}
	msgs := []ChatMessage{{Role: "user", Content: "hi", Loaded: []attachments.Loaded{loadedPNG()}}}
	params := a.params("sys", msgs)
	if len(params.Messages) != 1 {
		t.Fatalf("messages = %d", len(params.Messages))
	}
	if len(params.Messages[0].Content) != 2 {
		t.Fatalf("content blocks = %d, want 2 (image + text)", len(params.Messages[0].Content))
	}
}

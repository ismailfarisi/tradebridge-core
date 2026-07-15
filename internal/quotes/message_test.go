package quotes

import (
	"encoding/json"
	"testing"

	"github.com/tradebridge/core/internal/attachments"
)

func TestQuoteMessageEventShape(t *testing.T) {
	ev := QuoteMessageEvent{
		Type: "quote.message", RFQID: "r1", QuoteID: "q1", Author: "buyer", Body: "here is the photo",
		Attachments: []attachments.Meta{{ID: "a1", Filename: "x.png", Mime: "image/png", SizeBytes: 5}},
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"type", "rfq_id", "quote_id", "author", "body", "attachments"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing key %q in %s", k, b)
		}
	}
}

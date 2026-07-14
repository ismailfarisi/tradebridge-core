package rfq

import (
	"strings"
	"testing"
)

// feedAll splits input into fixed-size chunks and feeds them, returning the
// concatenated deltas. Chunk size 1 exercises every possible split point.
func feedAll(t *testing.T, field, input string, chunkSize int) (deltas string, raw string) {
	t.Helper()
	var out strings.Builder
	fs := newFieldStreamer(field, func(s string) { out.WriteString(s) })
	for i := 0; i < len(input); i += chunkSize {
		end := i + chunkSize
		if end > len(input) {
			end = len(input)
		}
		fs.Feed(input[i:end])
	}
	return out.String(), fs.Raw()
}

func TestFieldStreamer_SimpleAsk(t *testing.T) {
	in := `{"action":"ask","message":"What quantity do you need?"}`
	for _, size := range []int{1, 3, 7, len(in)} {
		deltas, raw := feedAll(t, "message", in, size)
		if deltas != "What quantity do you need?" {
			t.Fatalf("chunk=%d deltas = %q", size, deltas)
		}
		if raw != in {
			t.Fatalf("chunk=%d raw = %q", size, raw)
		}
	}
}

func TestFieldStreamer_EscapesAcrossChunks(t *testing.T) {
	in := `{"message":"line1\nline2 \"quoted\" tab\there"}`
	want := "line1\nline2 \"quoted\" tab\there"
	for _, size := range []int{1, 2, 5} {
		deltas, _ := feedAll(t, "message", in, size)
		if deltas != want {
			t.Fatalf("chunk=%d deltas = %q, want %q", size, deltas, want)
		}
	}
}

func TestFieldStreamer_UnicodeEscapesAndSurrogatePair(t *testing.T) {
	in := `{"message":"café 😀 done"}`
	want := "café 😀 done"
	for _, size := range []int{1, 4} {
		deltas, _ := feedAll(t, "message", in, size)
		if deltas != want {
			t.Fatalf("chunk=%d deltas = %q, want %q", size, deltas, want)
		}
	}
}

func TestFieldStreamer_RawUTF8SplitMidRune(t *testing.T) {
	in := `{"message":"héllo 世界"}`
	deltas, _ := feedAll(t, "message", in, 1) // splits multi-byte runes
	if deltas != "héllo 世界" {
		t.Fatalf("deltas = %q", deltas)
	}
}

func TestFieldStreamer_FieldAbsent_NoDeltas(t *testing.T) {
	in := `{"action":"create","title":"Steel pipes","rfq_text":"500 units"}`
	deltas, raw := feedAll(t, "message", in, 4)
	if deltas != "" {
		t.Fatalf("deltas = %q, want empty", deltas)
	}
	if raw != in {
		t.Fatalf("raw = %q", raw)
	}
}

func TestFieldStreamer_ProseNotJSON_NoDeltas(t *testing.T) {
	deltas, raw := feedAll(t, "message", "Sorry, I can't help with that.", 5)
	if deltas != "" {
		t.Fatalf("deltas = %q, want empty", deltas)
	}
	if raw == "" {
		t.Fatal("raw should accumulate")
	}
}

func TestFieldStreamer_KeyLiteralInsideOtherValue_NotMatched(t *testing.T) {
	// `"message"` inside another string value arrives with escaped quotes, so
	// the raw scan must not fire on it.
	in := `{"action":"say \"message\": hi","message":"real"}`
	deltas, _ := feedAll(t, "message", in, 1)
	if deltas != "real" {
		t.Fatalf("deltas = %q, want %q", deltas, "real")
	}
}

func TestFieldStreamer_StopsAtClosingQuote(t *testing.T) {
	in := `{"message":"hi","action":"ask"}`
	deltas, _ := feedAll(t, "message", in, 2)
	if deltas != "hi" {
		t.Fatalf("deltas = %q, want %q", deltas, "hi")
	}
}

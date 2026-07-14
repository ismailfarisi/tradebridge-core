package rfq

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// fieldStreamer incrementally scans streamed JSON text for one top-level
// string field (e.g. "message") and emits the field's decoded value as it
// arrives, tolerating any chunking (mid-key, mid-escape, mid-\uXXXX, mid-rune).
// Raw() returns everything fed so far, so the complete response still goes
// through the normal parser at stream end.
//
// The raw scan for `"field"` is safe: a literal `"field"` can't appear inside
// another JSON string value, because quotes inside values arrive escaped.
type fieldStreamer struct {
	key     string // quoted key to find, e.g. `"message"`
	onDelta func(string)

	raw     strings.Builder
	pending string // unconsumed input carried across Feed calls
	state   fsState
	uniBuf  string // hex digits of the current \uXXXX escape
	surr    rune   // pending high surrogate from a \uXXXX pair
	partial []byte // trailing bytes of an incomplete UTF-8 rune, held back
}

type fsState int

const (
	fsSeekKey fsState = iota
	fsSeekColon
	fsSeekQuote
	fsInString
	fsEscape
	fsUnicode
	fsDone
)

func newFieldStreamer(field string, onDelta func(string)) *fieldStreamer {
	return &fieldStreamer{key: `"` + field + `"`, onDelta: onDelta}
}

// Raw returns the full text fed so far.
func (f *fieldStreamer) Raw() string { return f.raw.String() }

// Feed consumes the next chunk; newly decoded field characters are emitted as
// a single onDelta call (complete UTF-8 only — a split rune waits for its tail).
func (f *fieldStreamer) Feed(chunk string) {
	f.raw.WriteString(chunk)
	if f.state == fsDone {
		return
	}
	in := f.pending + chunk
	f.pending = ""
	out := f.partial
	f.partial = nil

	i := 0
	for i < len(in) {
		switch f.state {
		case fsSeekKey:
			idx := strings.Index(in[i:], f.key)
			if idx < 0 {
				// Keep a possible partial-key suffix for the next chunk.
				keep := len(f.key) - 1
				if rem := len(in) - i; rem < keep {
					keep = rem
				}
				f.pending = in[len(in)-keep:]
				i = len(in)
				continue
			}
			i += idx + len(f.key)
			f.state = fsSeekColon
		case fsSeekColon:
			switch c := in[i]; c {
			case ' ', '\t', '\n', '\r':
				i++
			case ':':
				i++
				f.state = fsSeekQuote
			default:
				f.state = fsSeekKey // matched text that wasn't a key position
			}
		case fsSeekQuote:
			switch c := in[i]; c {
			case ' ', '\t', '\n', '\r':
				i++
			case '"':
				i++
				f.state = fsInString
			default:
				f.state = fsSeekKey // non-string value; not our field shape
			}
		case fsInString:
			switch c := in[i]; c {
			case '"':
				f.state = fsDone
				i = len(in)
			case '\\':
				f.state = fsEscape
				i++
			default:
				out = append(out, c)
				i++
			}
		case fsEscape:
			c := in[i]
			i++
			if c == 'u' {
				f.uniBuf = ""
				f.state = fsUnicode
				continue
			}
			switch c {
			case 'n':
				out = append(out, '\n')
			case 't':
				out = append(out, '\t')
			case 'r':
				out = append(out, '\r')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			default: // \" \\ \/ and anything unknown → literal
				out = append(out, c)
			}
			f.state = fsInString
		case fsUnicode:
			f.uniBuf += string(in[i])
			i++
			if len(f.uniBuf) == 4 {
				if n, err := strconv.ParseUint(f.uniBuf, 16, 32); err == nil {
					r := rune(n)
					switch {
					case utf16.IsSurrogate(r) && f.surr == 0:
						f.surr = r
					case utf16.IsSurrogate(r):
						out = utf8.AppendRune(out, utf16.DecodeRune(f.surr, r))
						f.surr = 0
					default:
						f.surr = 0
						out = utf8.AppendRune(out, r)
					}
				}
				f.state = fsInString
			}
		case fsDone:
			i = len(in)
		}
	}

	complete, partial := splitCompleteUTF8(out)
	f.partial = partial
	if len(complete) > 0 {
		f.onDelta(string(complete))
	}
}

// splitCompleteUTF8 cuts b so that `complete` ends on a rune boundary; any
// trailing bytes of an unfinished rune are returned as `partial`.
func splitCompleteUTF8(b []byte) (complete, partial []byte) {
	i := len(b)
	for i > 0 && !utf8.RuneStart(b[i-1]) {
		i--
	}
	if i == 0 {
		return nil, b
	}
	if utf8.FullRune(b[i-1:]) {
		return b, nil
	}
	return b[:i-1], b[i-1:]
}

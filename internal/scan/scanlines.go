package scan

import (
	"bufio"
	"strings"
	"unicode/utf8"
)

// MaxScanLineBytes caps a single physical line fed to a line-oriented
// scanner.  A line longer than this is overwhelmingly machine-generated
// (minified JSON, a base64 blob) rather than the natural-language threat
// surface this scanner exists to catch, so the over-long remainder is dropped
// on a rune boundary and scanning continues — a single pathological line must
// never abort the whole scan and discard already-collected findings or
// docstrings.
//
// This threshold is shared by the detector (internal/detect) and by the
// ecosystem extractors in this package so a >1 MiB line cannot silently abort
// either layer.
const MaxScanLineBytes = 1 << 20

// NewSplitLongTolerant returns a bufio.SplitFunc that behaves like
// bufio.ScanLines but never returns bufio.ErrTooLong AND never splits a
// single over-long physical line into more than one token.  A line longer
// than MaxScanLineBytes is emitted once as a rune-safe truncated prefix; the
// remainder of that same physical line — up to and including the next '\n' —
// is then consumed silently, emitting no further token, so the whole physical
// line counts as exactly one logical line.  This keeps the caller's line
// counter faithful to the source: before this fix bufio re-buffered the
// dropped tail and emitted it as a SECOND token, so every finding or
// docstring after a >1 MiB line was reported one (or more) lines too high —
// or, when used by a default-ScanLines extractor, bufio.ErrTooLong stopped
// the scan outright and silently dropped every docstring / comment block
// after the over-long line.
//
// The skip state lives in a closure, so each scan gets its own split
// function — the returned closure must NOT be shared across scanners.
func NewSplitLongTolerant() bufio.SplitFunc {
	skipRemainder := false
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		if skipRemainder {
			// A truncated prefix was already emitted for the current
			// over-long line; swallow the rest of that physical line
			// (through the next '\n') without producing a token so the
			// tail is not counted as an additional line.
			if i := strings.IndexByte(string(data), '\n'); i >= 0 {
				skipRemainder = false
				return i + 1, nil, nil
			}
			// No line end yet: consume what we have and ask for more (or
			// stop cleanly at EOF).  Advancing keeps bufio from panicking
			// on a no-progress split.
			skipRemainder = !atEOF
			return len(data), nil, nil
		}
		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := strings.IndexByte(string(data), '\n'); i >= 0 {
			// Found a newline; emit the line (dropping a trailing \r) verbatim.
			return i + 1, dropCR(data[:i]), nil
		}
		// No newline in the current buffer.  If we are at EOF, emit whatever
		// remains.  Otherwise, when the buffer is already full the line is
		// longer than MaxScanLineBytes: emit a rune-safe prefix, then enter
		// skip mode so the rest of this physical line is consumed as part of
		// the SAME logical line instead of re-emitted as a second token.
		if atEOF {
			return len(data), dropCR(data), nil
		}
		if len(data) >= MaxScanLineBytes {
			cut := safeRuneCut(data)
			skipRemainder = true
			return len(data), data[:cut], nil
		}
		// Request more data.
		return 0, nil, nil
	}
}

// dropCR removes a single trailing carriage return so CRLF inputs split the
// same way bufio.ScanLines would.
func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}
	return data
}

// safeRuneCut returns the largest length <= len(data) that does not split a
// multibyte UTF-8 rune, so a truncated over-long line stays valid UTF-8.
func safeRuneCut(data []byte) int {
	cut := len(data)
	for cut > 0 && !utf8.RuneStart(data[cut-1]) {
		cut--
	}
	// data[cut-1] is now a rune start; verify the rune it begins is whole.
	if cut > 0 && cut < len(data) {
		if r, _ := utf8.DecodeRune(data[cut-1:]); r == utf8.RuneError {
			cut--
		}
	}
	if cut < 0 {
		cut = 0
	}
	return cut
}

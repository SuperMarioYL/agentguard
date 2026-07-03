// Package detect loads the YAML payload corpus, compiles its regexes, and
// runs them — together with a small heuristic layer in heuristics.go —
// against the prose files emitted by package scan.
package detect

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/SuperMarioYL/agentguard/corpus"
	"github.com/SuperMarioYL/agentguard/internal/scan"
)

// Severity is the ordered tier of a finding.  Order matters: callers
// gate the CLI exit code on a configurable minimum, so the integer
// values must form a strict total order from low (0) to high (2).
type Severity int

const (
	SeverityLow Severity = iota
	SeverityMedium
	SeverityHigh
)

// String returns the lowercase YAML / CLI spelling of the severity.
func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "low"
	case SeverityMedium:
		return "medium"
	case SeverityHigh:
		return "high"
	default:
		return "unknown"
	}
}

// ParseSeverity is the inverse of String — case-insensitive and tolerant
// of the common "med" abbreviation.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return SeverityLow, nil
	case "medium", "med":
		return SeverityMedium, nil
	case "high":
		return SeverityHigh, nil
	}
	return 0, fmt.Errorf("unknown severity %q (want low|medium|high)", s)
}

// Finding is one matched rule against one prose-file line.  Two findings
// referring to the same (file, line, rule) are deduplicated by the
// detector before being returned.
type Finding struct {
	// Package is the human-friendly package label, e.g. "jqwik@1.9.2".
	Package string
	// Ecosystem is "npm" / "pypi" / "go" / "generic".
	Ecosystem string
	// File is the display path relative to the scan root.
	File string
	// Line is 1-indexed.
	Line int
	// RuleID is the corpus rule id or the synthetic heuristic id (Hxxx).
	RuleID string
	// Severity is the rule's tier, copied for sorting convenience.
	Severity Severity
	// Excerpt is the matched line, trimmed and length-capped.
	Excerpt string
	// Why is the human-readable rule title.
	Why string
}

// Rule is one entry in the embedded corpus, after regex compilation.
type Rule struct {
	ID          string
	Severity    Severity
	Title       string
	Description string
	References  []string
	Patterns    []*regexp.Regexp
}

// Metadata is the small header the corpus self-reports.
type Metadata struct {
	Version   string
	Updated   string
	Schema    int
	RuleCount int
}

// Detector applies the curated corpus and the heuristics layer to a
// slice of prose files.  It is goroutine-safe after construction.
type Detector struct {
	rules      []Rule
	heuristics []heuristic
	meta       Metadata
}

// NewDefault returns a Detector loaded with the embedded corpus and the
// built-in heuristic set.
func NewDefault() (*Detector, error) {
	return newFromBytes(corpus.Bytes)
}

// CorpusMetadata exposes just the header of the embedded corpus, for
// the `agentguard corpus` subcommand.
func CorpusMetadata() (Metadata, error) {
	d, err := newFromBytes(corpus.Bytes)
	if err != nil {
		return Metadata{}, err
	}
	return d.meta, nil
}

// FilterMinSeverity returns only findings at or above floor.  The
// underlying array is reused so callers must treat the input slice as
// consumed.
func FilterMinSeverity(findings []Finding, floor Severity) []Finding {
	out := findings[:0]
	for _, f := range findings {
		if f.Severity >= floor {
			out = append(out, f)
		}
	}
	return out
}

// corpusFile is the on-disk shape of corpus/payloads.yaml.
type corpusFile struct {
	Version string       `yaml:"version"`
	Updated string       `yaml:"updated"`
	Schema  int          `yaml:"schema"`
	Rules   []corpusRule `yaml:"rules"`
}

type corpusRule struct {
	ID          string        `yaml:"id"`
	Severity    string        `yaml:"severity"`
	Title       string        `yaml:"title"`
	Description string        `yaml:"description"`
	References  []string      `yaml:"references"`
	Patterns    []corpusRegex `yaml:"patterns"`
}

type corpusRegex struct {
	Regex string `yaml:"regex"`
}

func newFromBytes(data []byte) (*Detector, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("corpus is empty")
	}
	var cf corpusFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("corpus parse: %w", err)
	}
	if len(cf.Rules) == 0 {
		return nil, fmt.Errorf("corpus has zero rules")
	}
	rules := make([]Rule, 0, len(cf.Rules))
	for _, r := range cf.Rules {
		sev, err := ParseSeverity(r.Severity)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", r.ID, err)
		}
		patterns := make([]*regexp.Regexp, 0, len(r.Patterns))
		for i, p := range r.Patterns {
			// Anchor case-insensitive without forcing the rule author
			// to remember the (?i) prefix; the corpus contract says
			// rules are case-insensitive at runtime.
			re, err := regexp.Compile("(?i)" + p.Regex)
			if err != nil {
				return nil, fmt.Errorf("rule %s pattern[%d]: %w", r.ID, i, err)
			}
			patterns = append(patterns, re)
		}
		rules = append(rules, Rule{
			ID:          r.ID,
			Severity:    sev,
			Title:       r.Title,
			Description: r.Description,
			References:  append([]string(nil), r.References...),
			Patterns:    patterns,
		})
	}

	return &Detector{
		rules:      rules,
		heuristics: defaultHeuristics(),
		meta: Metadata{
			Version:   cf.Version,
			Updated:   cf.Updated,
			Schema:    cf.Schema,
			RuleCount: len(rules),
		},
	}, nil
}

// maxScanLineBytes caps a single prose line fed to the rule engine.  A
// line longer than this is overwhelmingly machine-generated (minified
// JSON, a base64 blob) rather than the natural-language threat surface
// this scanner exists to catch, so the over-long remainder is dropped on
// a rune boundary and scanning continues — a single pathological line
// must never abort the whole scan and discard already-collected findings.
const maxScanLineBytes = 1 << 20

// ScanAll applies every rule and heuristic to every file's prose
// content and returns the matched findings.  A line that triggers
// multiple rules emits multiple findings; the (file, line, rule) tuple
// is deduplicated so the same rule does not appear twice for the same
// line even when several of its patterns match.
//
// ScanAll is best-effort: a line longer than the scanner buffer (e.g. a
// >1 MiB minified blob) is rune-safely truncated rather than treated as
// a hard error, so one bad line cannot abort the scan or discard the
// findings already accumulated from other files.  The returned error is
// reserved for genuinely unrecoverable failures; today there are none,
// but the signature is kept stable for callers.
func (d *Detector) ScanAll(files []scan.File) ([]Finding, error) {
	var findings []Finding
	seen := make(map[string]struct{})

	for _, file := range files {
		if file.Content == "" {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(file.Content))
		scanner.Buffer(make([]byte, 0, 64*1024), maxScanLineBytes)
		// On an over-long token bufio.Scanner stops early; recover the
		// dropped lines by re-splitting them ourselves so the remainder of
		// the file is still scanned.  splitLongTolerant truncates any
		// single line longer than the buffer instead of erroring out.
		scanner.Split(newSplitLongTolerant())
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			for _, rule := range d.rules {
				for _, pat := range rule.Patterns {
					if !pat.MatchString(line) {
						continue
					}
					key := fmt.Sprintf("%s|%d|%s", file.DisplayPath, lineNo, rule.ID)
					if _, ok := seen[key]; ok {
						break
					}
					seen[key] = struct{}{}
					findings = append(findings, Finding{
						Package:   file.Package,
						Ecosystem: file.Ecosystem,
						File:      file.DisplayPath,
						Line:      lineNo,
						RuleID:    rule.ID,
						Severity:  rule.Severity,
						Excerpt:   truncateExcerpt(trimmed, 200),
						Why:       rule.Title,
					})
					break
				}
			}
			for _, h := range d.heuristics {
				if !h.match(line) {
					continue
				}
				key := fmt.Sprintf("%s|%d|%s", file.DisplayPath, lineNo, h.id)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				findings = append(findings, Finding{
					Package:   file.Package,
					Ecosystem: file.Ecosystem,
					File:      file.DisplayPath,
					Line:      lineNo,
					RuleID:    h.id,
					Severity:  h.severity,
					Excerpt:   truncateExcerpt(trimmed, 200),
					Why:       h.title,
				})
			}
		}
		if err := scanner.Err(); err != nil {
			// With splitLongTolerant in place bufio.ErrTooLong can no longer
			// surface, but should any other I/O-style error appear, skip the
			// rest of this file and keep the findings already collected
			// instead of discarding the whole scan.
			continue
		}
	}
	return findings, nil
}

// newSplitLongTolerant returns a bufio.SplitFunc that behaves like
// bufio.ScanLines but never returns bufio.ErrTooLong AND never splits a
// single over-long physical line into more than one token.  A line longer
// than maxScanLineBytes is emitted once as a rune-safe truncated prefix;
// the remainder of that same physical line — up to and including the next
// '\n' — is then consumed silently, emitting no further token, so the whole
// physical line counts as exactly one logical line.  This keeps ScanAll's
// lineNo faithful to the source: before this fix bufio re-buffered the
// dropped tail and emitted it as a SECOND token, so every finding after a
// >1 MiB line was reported one (or more) lines too high.
//
// The skip state lives in a closure, so each scan gets its own split
// function — the returned closure must NOT be shared across scanners.
func newSplitLongTolerant() bufio.SplitFunc {
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
		// longer than maxScanLineBytes: emit a rune-safe prefix, then enter
		// skip mode so the rest of this physical line is consumed as part of
		// the SAME logical line instead of re-emitted as a second token.
		if atEOF {
			return len(data), dropCR(data), nil
		}
		if len(data) >= maxScanLineBytes {
			cut := safeRuneCut(data)
			skipRemainder = true
			return len(data), data[:cut], nil
		}
		// Request more data.
		return 0, nil, nil
	}
}

// dropCR removes a single trailing carriage return so CRLF inputs split
// the same way bufio.ScanLines would.
func dropCR(data []byte) []byte {
	if len(data) > 0 && data[len(data)-1] == '\r' {
		return data[:len(data)-1]
	}
	return data
}

// safeRuneCut returns the largest length <= len(data) that does not split
// a multibyte UTF-8 rune, so a truncated over-long line stays valid UTF-8.
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

// Rules returns a defensive copy of the loaded rule set, for callers
// that want to render or audit the corpus at runtime.
func (d *Detector) Rules() []Rule {
	out := make([]Rule, len(d.rules))
	copy(out, d.rules)
	return out
}

// truncateExcerpt caps the excerpt at max runes (not bytes) and appends an
// ellipsis when it cuts.  Slicing on a rune boundary keeps the excerpt
// valid UTF-8 for non-ASCII content (the project's primary zh locale and
// any multibyte README), so the text report and the SARIF result message
// never emit a broken trailing rune that a strict SARIF consumer rejects.
func truncateExcerpt(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	// Keep max-1 runes, leaving room for the ellipsis.
	runes := 0
	for i := range s {
		if runes == max-1 {
			return s[:i] + "…"
		}
		runes++
	}
	return s + "…"
}

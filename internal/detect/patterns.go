// Package detect loads the YAML payload corpus, compiles its regexes, and
// runs them — together with a small heuristic layer in heuristics.go —
// against the prose files emitted by package scan.
package detect

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/supermario-leo/agentguard/corpus"
	"github.com/supermario-leo/agentguard/internal/scan"
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

// ScanAll applies every rule and heuristic to every file's prose
// content and returns the matched findings.  A line that triggers
// multiple rules emits multiple findings; the (file, line, rule) tuple
// is deduplicated so the same rule does not appear twice for the same
// line even when several of its patterns match.
func (d *Detector) ScanAll(files []scan.File) ([]Finding, error) {
	var findings []Finding
	seen := make(map[string]struct{})

	for _, file := range files {
		if file.Content == "" {
			continue
		}
		scanner := bufio.NewScanner(strings.NewReader(file.Content))
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
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
			return findings, fmt.Errorf("scan %s: %w", file.DisplayPath, err)
		}
	}
	return findings, nil
}

// Rules returns a defensive copy of the loaded rule set, for callers
// that want to render or audit the corpus at runtime.
func (d *Detector) Rules() []Rule {
	out := make([]Rule, len(d.rules))
	copy(out, d.rules)
	return out
}

func truncateExcerpt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// Package config loads the optional .agentguard.yaml project config that
// suppresses or re-weights detector findings for a specific repository.
//
// A project drops a .agentguard.yaml at the root of the directory it scans
// (the path passed to `agentguard check`).  The config is loaded at scan
// start and applied to findings before the reporter renders them, so a
// project can mute benign rule matches without weakening the global,
// static 30-rule corpus that ships with the binary.
//
// Absence is backward-compatible: a missing .agentguard.yaml yields an empty
// Config (no error), every corpus rule stays active, and behaviour is
// identical to a scan run with no project file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the parsed .agentguard.yaml. The zero value is a no-op: no rules
// disabled, no allowlist, no severity overrides — the backward-compatible
// default where every corpus rule stays active.
type Config struct {
	// Disable lists rule IDs whose findings are suppressed before
	// reporting. A disabled rule produces no finding regardless of
	// severity. Matching is case-insensitive.
	Disable []string `yaml:"disable"`
	// Allow, when non-empty, is an allowlist of rule IDs: only findings
	// whose rule ID appears here are reported; every other rule is
	// suppressed. Empty means no allowlist (all non-disabled rules run).
	// Matching is case-insensitive.
	Allow []string `yaml:"allow"`
	// Severity overrides a rule's severity tier. Keys are rule IDs;
	// values are "low" | "medium" | "high". An override is applied
	// before the minimum-severity floor so a project can downgrade a
	// noisy high rule to low and keep it out of the CI gate.
	// Matching is case-insensitive.
	Severity map[string]string `yaml:"severity"`
}

// validSeverity is the set of accepted severity override values. The "med"
// alias matches detect.ParseSeverity so the config and the CLI --severity
// flag accept the same spellings.
var validSeverity = map[string]bool{
	"low":    true,
	"medium": true,
	"med":    true,
	"high":   true,
}

// Load reads .agentguard.yaml from dir. A missing file yields a zero Config
// (no error) so absence is backward-compatible: every rule stays active. An
// unreadable file is an error; a malformed file is an error so a typo does
// not silently weaken the gate.
func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, ".agentguard.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return &Config{}, nil
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", path, err)
	}
	cfg.Disable = normIDs(cfg.Disable)
	cfg.Allow = normIDs(cfg.Allow)
	sev, err := normSeverity(cfg.Severity)
	if err != nil {
		return nil, err
	}
	cfg.Severity = sev
	return &cfg, nil
}

// IsEmpty reports whether the config changes nothing — no disables, no
// allowlist, no severity overrides. Callers can use it to skip the filter
// pass entirely (and the allocation it implies) on the backward-compatible
// no-config path.
func (c Config) IsEmpty() bool {
	return len(c.Disable) == 0 && len(c.Allow) == 0 && len(c.Severity) == 0
}

// normIDs lowercases, trims, and drops empties from a rule-ID list so config
// matching is case- and whitespace-insensitive.
func normIDs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// normSeverity lowercases keys and values, drops empties, and validates each
// value against the accepted severity spellings. An invalid value is an error
// so a typo (e.g. "hig") fails the scan loudly instead of being ignored.
func normSeverity(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for k, v := range in {
		id := strings.ToLower(strings.TrimSpace(k))
		val := strings.ToLower(strings.TrimSpace(v))
		if id == "" || val == "" {
			continue
		}
		if !validSeverity[val] {
			return nil, fmt.Errorf("config: severity override for %q is %q (want low|medium|high)", id, val)
		}
		out[id] = val
	}
	return out, nil
}

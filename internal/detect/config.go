package detect

import (
	"strings"

	"github.com/SuperMarioYL/agentguard/internal/config"
)

// Suppress applies a project config to findings and returns the filtered
// slice. It is the wired-in step that runs after ScanAll and before the
// minimum-severity floor / reporter, so disabled rule IDs never reach the
// rendered report.
//
// For each finding it:
//  1. drops the finding if its rule ID is in cfg.Disable (disabled rules
//     produce no finding, regardless of severity);
//  2. when cfg.Allow is non-empty, drops the finding unless its rule ID is
//     on the allowlist (an empty allowlist means "no allowlist", not "allow
//     nothing");
//  3. applies any per-rule severity override from cfg.Severity, so a project
//     can downgrade a noisy high rule to low — the override takes effect
//     before the caller's minimum-severity floor.
//
// Rule-ID matching is case-insensitive. The returned slice is freshly
// allocated so the caller's input is never aliased; a no-op config (the
// backward-compatible default) returns the input slice unchanged.
func Suppress(findings []Finding, cfg config.Config) []Finding {
	if cfg.IsEmpty() {
		return findings
	}
	disabled := make(map[string]struct{}, len(cfg.Disable))
	for _, id := range cfg.Disable {
		disabled[id] = struct{}{}
	}
	allowAll := len(cfg.Allow) == 0
	allowed := make(map[string]struct{}, len(cfg.Allow))
	for _, id := range cfg.Allow {
		allowed[id] = struct{}{}
	}

	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		id := strings.ToLower(f.RuleID)
		if _, off := disabled[id]; off {
			continue
		}
		if !allowAll {
			if _, on := allowed[id]; !on {
				continue
			}
		}
		if sev, ok := cfg.Severity[id]; ok {
			// cfg.Severity values are validated by config.Load, so an
			// override always parses; a defensive no-op on parse error
			// keeps a finding visible rather than silently dropping it.
			if s, err := ParseSeverity(sev); err == nil {
				f.Severity = s
			}
		}
		out = append(out, f)
	}
	return out
}

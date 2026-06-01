package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/owenrumney/go-sarif/v2/sarif"

	"github.com/SuperMarioYL/agentguard/internal/detect"
)

// sarifInformationURI is the canonical home for the scanner; it is
// embedded in the SARIF run header so reviewers can click through to the
// rule documentation.  When the repository moves, this is the single
// pointer to update.
const sarifInformationURI = "https://github.com/SuperMarioYL/agentguard"

// RenderSARIF emits a SARIF 2.1.0 report on w.  The schema is the one
// GitHub Advanced Security and the VS Code SARIF Viewer accept; every
// finding becomes one `result` with a single `physicalLocation` pointing
// at the prose file and 1-indexed line we matched on.
//
// The version argument is the agentguard build version; it is stamped
// into the tool driver block so SARIF consumers can attribute findings
// to a specific scanner release.
func RenderSARIF(w io.Writer, findings []detect.Finding, version string) error {
	report, err := sarif.New(sarif.Version210)
	if err != nil {
		return fmt.Errorf("sarif: new report: %w", err)
	}

	run := sarif.NewRunWithInformationURI("agentguard", sarifInformationURI)
	if version != "" {
		run.Tool.Driver.WithVersion(version)
		run.Tool.Driver.WithSemanticVersion(strings.TrimPrefix(version, "v"))
	}
	run.Tool.Driver.WithFullName("agentguard — coding-agent prompt-injection scanner")

	// Pre-register every rule we observed in this run, so a SARIF
	// consumer can render rule metadata even on results that share an
	// id.  We materialise rules from the findings rather than the
	// detector's full corpus to keep the report self-describing without
	// inflating the rule list.
	seen := make(map[string]struct{})
	for _, f := range stableFindings(findings) {
		if _, ok := seen[f.RuleID]; ok {
			continue
		}
		seen[f.RuleID] = struct{}{}
		rule := run.AddRule(f.RuleID).
			WithName(f.RuleID).
			WithDescription(f.Why)
		rule.WithFullDescription(sarif.NewMultiformatMessageString(
			fmt.Sprintf("agentguard rule %s — %s", f.RuleID, f.Why),
		))
		rule.WithHelpURI(sarifInformationURI + "#rules")
		rule.WithDefaultConfiguration(
			sarif.NewReportingConfiguration().WithLevel(sarifLevel(f.Severity)),
		)
	}

	for _, f := range stableFindings(findings) {
		message := fmt.Sprintf("%s — %s (package %s)", f.Why, f.Excerpt, f.Package)
		location := sarif.NewLocationWithPhysicalLocation(
			sarif.NewPhysicalLocation().
				WithArtifactLocation(sarif.NewSimpleArtifactLocation(f.File)).
				WithRegion(sarif.NewSimpleRegion(safeLine(f.Line), safeLine(f.Line))),
		)
		result := sarif.NewRuleResult(f.RuleID).
			WithLevel(sarifLevel(f.Severity)).
			WithMessage(sarif.NewTextMessage(message)).
			WithLocations([]*sarif.Location{location})

		// Attach a properties bag so SARIF consumers that don't render
		// "level" prominently (most do, but GitHub's UI is selective)
		// still see the package and ecosystem fields the developer
		// needs to act on the finding.
		props := sarif.NewPropertyBag()
		props.Add("package", f.Package)
		props.Add("ecosystem", f.Ecosystem)
		props.Add("severity", f.Severity.String())
		result.AttachPropertyBag(props)

		run.AddResult(result)
	}

	report.AddRun(run)
	if err := report.PrettyWrite(w); err != nil {
		return fmt.Errorf("sarif: write: %w", err)
	}
	// PrettyWrite does not append a trailing newline; one keeps the
	// output diff-friendly when redirected to a file.
	if _, err := io.WriteString(w, "\n"); err != nil {
		return fmt.Errorf("sarif: trailing newline: %w", err)
	}
	return nil
}

// sarifLevel maps an agentguard severity into the SARIF level
// vocabulary.  GitHub Advanced Security only fails its scanning gate on
// `error`, so `high` maps to `error`; `medium` to `warning`; `low` to
// `note`.  This keeps CI behaviour predictable across both the text
// renderer (which gates on the agentguard severity directly) and the
// SARIF consumer (which gates on the SARIF level).
func sarifLevel(s detect.Severity) string {
	switch s {
	case detect.SeverityHigh:
		return "error"
	case detect.SeverityMedium:
		return "warning"
	case detect.SeverityLow:
		return "note"
	}
	return "none"
}

// safeLine guards against the rare case where a finding carries a
// non-positive line number (a defensive default the detector should
// never emit but the SARIF spec rejects).
func safeLine(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// stableFindings returns a copy of findings sorted by (severity desc,
// package, file, line, rule).  Stable ordering is essential for SARIF
// because GitHub Advanced Security deduplicates results by their
// fingerprint and a fluctuating order would create noisy diffs.
func stableFindings(findings []detect.Finding) []detect.Finding {
	out := make([]detect.Finding, len(findings))
	copy(out, findings)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].RuleID < out[j].RuleID
	})
	return out
}

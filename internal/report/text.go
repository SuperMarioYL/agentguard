// Package report renders detector findings into the formats the CLI
// supports: human-readable text (this file) and SARIF 2.1.0 (m3).
package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/fatih/color"

	"github.com/supermario-leo/agentguard/internal/detect"
)

// RenderText writes a colour-aware, human-readable report of the
// findings.  Findings are grouped by package and sorted by (severity
// desc, file, line) so the highest-priority hit lands at the top of the
// output.
//
// An empty slice produces a single "no findings" line so a quiet run is
// still self-documenting in CI logs.
func RenderText(w io.Writer, findings []detect.Finding) error {
	if len(findings) == 0 {
		green := color.New(color.FgGreen).SprintFunc()
		_, err := fmt.Fprintln(w, green("agentguard: no findings"))
		return err
	}

	sorted := make([]detect.Finding, len(findings))
	copy(sorted, findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Severity != sorted[j].Severity {
			return sorted[i].Severity > sorted[j].Severity
		}
		if sorted[i].Package != sorted[j].Package {
			return sorted[i].Package < sorted[j].Package
		}
		if sorted[i].File != sorted[j].File {
			return sorted[i].File < sorted[j].File
		}
		if sorted[i].Line != sorted[j].Line {
			return sorted[i].Line < sorted[j].Line
		}
		return sorted[i].RuleID < sorted[j].RuleID
	})

	headerBold := color.New(color.Bold).SprintFunc()
	pkgBold := color.New(color.FgCyan, color.Bold).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()

	fmt.Fprintln(w, headerBold("agentguard findings"))
	fmt.Fprintln(w, dim(strings.Repeat("─", 60)))

	counts := tallySeverity(sorted)
	fmt.Fprintf(w, "  %s   %s   %s\n",
		colourSeverity(detect.SeverityHigh, fmt.Sprintf("high: %d", counts[detect.SeverityHigh])),
		colourSeverity(detect.SeverityMedium, fmt.Sprintf("medium: %d", counts[detect.SeverityMedium])),
		colourSeverity(detect.SeverityLow, fmt.Sprintf("low: %d", counts[detect.SeverityLow])),
	)
	fmt.Fprintln(w)

	var lastPackage string
	for _, f := range sorted {
		if f.Package != lastPackage {
			fmt.Fprintf(w, "%s %s\n", pkgBold("package"), pkgBold(formatPackage(f)))
			lastPackage = f.Package
		}
		sevTag := colourSeverity(f.Severity, fmt.Sprintf("[%s]", strings.ToUpper(f.Severity.String())))
		loc := fmt.Sprintf("%s:%d", f.File, f.Line)
		fmt.Fprintf(w, "  %s %s  %s\n", sevTag, dim(loc), f.RuleID)
		fmt.Fprintf(w, "        %s\n", dim("why: "+f.Why))
		fmt.Fprintf(w, "        %s\n", f.Excerpt)
		fmt.Fprintln(w)
	}
	return nil
}

func formatPackage(f detect.Finding) string {
	if f.Ecosystem != "" && f.Ecosystem != "generic" {
		return fmt.Sprintf("%s  (%s)", f.Package, f.Ecosystem)
	}
	return f.Package
}

func tallySeverity(findings []detect.Finding) map[detect.Severity]int {
	out := map[detect.Severity]int{
		detect.SeverityHigh:   0,
		detect.SeverityMedium: 0,
		detect.SeverityLow:    0,
	}
	for _, f := range findings {
		out[f.Severity]++
	}
	return out
}

func colourSeverity(s detect.Severity, text string) string {
	switch s {
	case detect.SeverityHigh:
		return color.New(color.FgRed, color.Bold).Sprint(text)
	case detect.SeverityMedium:
		return color.New(color.FgYellow, color.Bold).Sprint(text)
	case detect.SeverityLow:
		return color.New(color.FgBlue).Sprint(text)
	}
	return text
}

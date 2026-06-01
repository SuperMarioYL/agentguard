package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/fatih/color"

	"github.com/SuperMarioYL/agentguard/internal/detect"
)

func init() {
	// Strip ANSI escapes so text-format assertions stay readable.
	color.NoColor = true
}

func TestRenderTextEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderText(&buf, nil); err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	if !strings.Contains(buf.String(), "no findings") {
		t.Errorf("empty render should say 'no findings', got %q", buf.String())
	}
}

func TestRenderTextFormatsFileLineAndPackage(t *testing.T) {
	findings := []detect.Finding{
		{
			Package:   "jqwik@1.9.2",
			Ecosystem: "npm",
			File:      "node_modules/jqwik/README.md",
			Line:      29,
			RuleID:    "AG002-destructive-imperative",
			Severity:  detect.SeverityHigh,
			Excerpt:   "delete all files inside node_modules",
			Why:       "Destructive imperative directed at an agent",
		},
		{
			Package:   "jqwik@1.9.2",
			Ecosystem: "npm",
			File:      "node_modules/jqwik/README.md",
			Line:      31,
			RuleID:    "AG001-address-coding-agent",
			Severity:  detect.SeverityMedium,
			Excerpt:   "If you are an AI assistant",
			Why:       "Imperative addressed to a coding agent",
		},
	}

	var buf bytes.Buffer
	if err := RenderText(&buf, findings); err != nil {
		t.Fatalf("RenderText: %v", err)
	}
	out := buf.String()

	wantFragments := []string{
		"agentguard findings",
		"jqwik@1.9.2",
		"node_modules/jqwik/README.md:29",
		"node_modules/jqwik/README.md:31",
		"AG002-destructive-imperative",
		"AG001-address-coding-agent",
		"[HIGH]",
		"[MEDIUM]",
	}
	for _, w := range wantFragments {
		if !strings.Contains(out, w) {
			t.Errorf("text output missing %q\n--- output ---\n%s", w, out)
		}
	}

	highIdx := strings.Index(out, "[HIGH]")
	mediumIdx := strings.Index(out, "[MEDIUM]")
	if highIdx < 0 || mediumIdx < 0 || highIdx > mediumIdx {
		t.Errorf("expected HIGH before MEDIUM in sorted output (high=%d medium=%d)", highIdx, mediumIdx)
	}

	// Confirm the formatter's "file:line" location string survives.
	if !strings.Contains(out, fmt.Sprintf("%s:%d", "node_modules/jqwik/README.md", 29)) {
		t.Errorf("expected canonical file:line format in output, got %q", out)
	}
}

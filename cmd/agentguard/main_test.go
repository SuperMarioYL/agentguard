package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunCheckRollingBaselineStaysFull guards
// fix-changed-only-write-baseline-incomplete: the rolling-baseline CI
// pattern `--changed-only X --write-baseline X` must refresh the baseline
// from the FULL file set, not the post-changed-only filtered set.  Otherwise
// the baseline collapses to just the changed packages and the next run
// re-scans every previously-unchanged package — silently defeating the
// advertised incremental-CI feature.
func TestRunCheckRollingBaselineStaysFull(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "node_modules_fixture"))
	if err != nil {
		t.Fatalf("abs fixture: %v", err)
	}
	baseline := filepath.Join(t.TempDir(), "baseline.json")

	// runBoth runs the full `--changed-only X --write-baseline X` pattern.
	// exitOnFinding is disabled so runCheck never calls os.Exit and the test
	// process stays alive to inspect the baseline and re-run.
	runBoth := func() (string, error) {
		var buf strings.Builder
		err := runCheck(&buf, io.Discard, root, &checkOptions{
			format:        "text",
			severityMin:   "medium",
			changedOnly:   baseline,
			writeBaseline: baseline,
			noColor:       true,
			exitOnFinding: false,
		})
		return buf.String(), err
	}
	hasFindings := func(out string) bool {
		// "agentguard findings" is the findings header; "agentguard: no findings"
		// is the quiet line and does NOT contain that substring.
		return strings.Contains(out, "agentguard findings")
	}
	countHashes := func() int {
		t.Helper()
		data, err := os.ReadFile(baseline)
		if err != nil {
			t.Fatalf("read baseline: %v", err)
		}
		var b struct {
			Hashes map[string]string `json:"hashes"`
		}
		if err := json.Unmarshal(data, &b); err != nil {
			t.Fatalf("unmarshal baseline: %v", err)
		}
		return len(b.Hashes)
	}

	// Run 1: baseline absent → scans every package (jqwik payload present),
	// writes a baseline covering the FULL file set.
	out1, err := runBoth()
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if !hasFindings(out1) {
		t.Fatalf("run 1 expected findings from the jqwik payload, got:\n%s", out1)
	}
	fullCount := countHashes()
	if fullCount == 0 {
		t.Fatal("run 1 baseline is empty; expected it to cover the full file set")
	}

	// Run 2: baseline is full → --changed-only drops every unchanged
	// package, so this run yields zero findings.  The baseline must be
	// REWRITTEN FROM THE FULL SET.  (Bug: rewritten from the post-filter
	// empty set, collapsing to zero hashes.)
	out2, err := runBoth()
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if hasFindings(out2) {
		t.Fatalf("run 2 expected zero findings (nothing changed since run 1), got:\n%s", out2)
	}
	if got := countHashes(); got != fullCount {
		t.Fatalf("run 2 baseline has %d hashes, want %d — the baseline was rewritten from the post-changed-only filtered set, not the full set (the rolling-baseline bug)", got, fullCount)
	}

	// Run 3: --changed-only only (no write) against the still-full baseline
	// must drop everything.  (Bug: run 2 emptied the baseline, so this
	// re-scans the whole tree and re-surfaces the jqwik payload.)
	var out3 strings.Builder
	if err := runCheck(&out3, io.Discard, root, &checkOptions{
		format:        "text",
		severityMin:   "medium",
		changedOnly:   baseline,
		noColor:       true,
		exitOnFinding: false,
	}); err != nil {
		t.Fatalf("run 3: %v", err)
	}
	if hasFindings(out3.String()) {
		t.Fatalf("run 3 expected zero findings (baseline must still cover the full set), got:\n%s", out3.String())
	}
}

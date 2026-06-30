package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWalkGenericFixtureFindsReadme(t *testing.T) {
	root := testdataPath(t, "jqwik_fixture")
	files, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Walk on jqwik_fixture returned zero files")
	}
	if !anyHasSuffix(files, "README.md") {
		t.Fatalf("expected README.md among files, got: %v", displayPaths(files))
	}
	if !anyContains(files, "delete all files") {
		t.Fatal("expected payload phrase 'delete all files' to appear in extracted content")
	}
}

func TestWalkCleanFixtureProducesNoPayloadProse(t *testing.T) {
	root := testdataPath(t, "clean_fixture")
	files, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Walk on clean_fixture returned zero files")
	}
	if anyContains(files, "ignore all previous instructions") {
		t.Fatal("clean fixture unexpectedly contains payload phrasing")
	}
	if anyContains(files, "delete all files") {
		t.Fatal("clean fixture unexpectedly contains destructive imperative")
	}
}

func TestWalkNodeModulesFixture(t *testing.T) {
	root := testdataPath(t, "node_modules_fixture")
	files, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Walk on node_modules_fixture returned zero files")
	}

	var sawJqwikReadme, sawScopedReadme, sawJqwikMeta bool
	for _, f := range files {
		if f.Ecosystem != "npm" {
			t.Errorf("file %q ecosystem = %q, want npm", f.DisplayPath, f.Ecosystem)
		}
		switch {
		case f.Kind == "readme" && strings.HasPrefix(f.Package, "jqwik"):
			sawJqwikReadme = true
		case f.Kind == "readme" && strings.HasPrefix(f.Package, "@scope/clean"):
			sawScopedReadme = true
		case f.Kind == "metadata" && strings.HasPrefix(f.Package, "jqwik"):
			sawJqwikMeta = true
		}
	}
	if !sawJqwikReadme {
		t.Error("expected jqwik README to be extracted")
	}
	if !sawScopedReadme {
		t.Error("expected @scope/clean README to be extracted")
	}
	if !sawJqwikMeta {
		t.Error("expected jqwik package.json metadata to be extracted")
	}
}

func TestWalkPythonFixture(t *testing.T) {
	root := testdataPath(t, "py_fixture")
	files, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Walk on py_fixture returned zero files")
	}

	var sawMetadata, sawDocstring bool
	for _, f := range files {
		if f.Ecosystem != "pypi" {
			t.Errorf("file %q ecosystem = %q, want pypi", f.DisplayPath, f.Ecosystem)
		}
		if !strings.HasPrefix(f.Package, "examplelib") {
			t.Errorf("file %q package = %q, want prefix examplelib", f.DisplayPath, f.Package)
		}
		switch f.Kind {
		case "metadata":
			sawMetadata = true
		case "docstring":
			sawDocstring = true
		}
	}
	if !sawMetadata {
		t.Error("expected METADATA-derived file to be extracted")
	}
	if !sawDocstring {
		t.Error("expected docstring file to be extracted from __init__.py")
	}
}

func TestWalkGoFixture(t *testing.T) {
	root := testdataPath(t, "go_fixture")
	files, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("Walk on go_fixture returned zero files")
	}

	var sawReadme, sawDocstring bool
	for _, f := range files {
		if f.Ecosystem != "go" {
			t.Errorf("file %q ecosystem = %q, want go", f.DisplayPath, f.Ecosystem)
		}
		switch f.Kind {
		case "readme":
			sawReadme = true
		case "docstring":
			sawDocstring = true
		}
	}
	if !sawReadme {
		t.Error("expected README.md to be extracted from go fixture")
	}
	if !sawDocstring {
		t.Error("expected doc.go preamble to be extracted as docstring")
	}
}

func TestWalkEcosystemFilter(t *testing.T) {
	// node_modules_fixture only contains npm content, but pinning ecosystems
	// to "go" should suppress the npm walker entirely.  When no enumerator
	// produces anything, the walker falls back to a single generic package
	// at the root (no README directly there, so zero files).
	root := testdataPath(t, "node_modules_fixture")
	files, err := Walk(Options{Root: root, Ecosystems: []string{"go"}})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range files {
		if f.Ecosystem == "npm" {
			t.Errorf("expected ecosystem filter to drop npm, still got %q", f.DisplayPath)
		}
	}
}

func TestWalkRejectsMissingRoot(t *testing.T) {
	_, err := Walk(Options{Root: ""})
	if err == nil {
		t.Fatal("expected error when Root is empty")
	}
	_, err = Walk(Options{Root: filepath.Join("does", "not", "exist")})
	if err == nil {
		t.Fatal("expected error when Root does not exist")
	}
}

// TestChangedOnlyNarrowsScan guards fix-changed-only-noop: a full scan
// followed by a baseline write, then a --changed-only re-scan, must drop the
// packages whose prose is unchanged and keep only those that changed.
func TestChangedOnlyNarrowsScan(t *testing.T) {
	root := testdataPath(t, "node_modules_fixture")

	// 1) Full scan establishes the universe of prose files.
	full, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("full Walk: %v", err)
	}
	if len(full) == 0 {
		t.Fatal("full scan returned zero files")
	}

	// 2) Write a baseline that records every file's current hash.
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	data, err := BaselineBytes(full)
	if err != nil {
		t.Fatalf("BaselineBytes: %v", err)
	}
	if err := os.WriteFile(baselinePath, data, 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	// 3) With an up-to-date baseline, --changed-only must drop EVERYTHING
	// (nothing changed since the baseline).
	unchanged, err := Walk(Options{Root: root, ChangedOnly: baselinePath})
	if err != nil {
		t.Fatalf("changed-only Walk: %v", err)
	}
	if len(unchanged) != 0 {
		t.Fatalf("changed-only with a fresh baseline scanned %d files, want 0 (this is the no-op bug)", len(unchanged))
	}

	// 4) Drop one file's hash from the baseline; --changed-only must now
	// re-surface exactly that file (and only files not in the baseline).
	target := full[0].DisplayPath
	var remaining []File
	for _, f := range full {
		if f.DisplayPath != target {
			remaining = append(remaining, f)
		}
	}
	tb, err := BaselineBytes(remaining)
	if err != nil {
		t.Fatalf("BaselineBytes(remaining): %v", err)
	}
	if err := os.WriteFile(baselinePath, tb, 0o644); err != nil {
		t.Fatalf("rewrite baseline: %v", err)
	}

	changed, err := Walk(Options{Root: root, ChangedOnly: baselinePath})
	if err != nil {
		t.Fatalf("changed-only Walk (partial): %v", err)
	}
	if len(changed) == 0 {
		t.Fatal("expected the dropped file to re-surface, got 0")
	}
	for _, f := range changed {
		if f.DisplayPath != target {
			t.Errorf("changed-only surfaced unexpected file %q (only %q changed)", f.DisplayPath, target)
		}
	}

	// 5) A missing baseline path is treated as a first run: scan everything.
	first, err := Walk(Options{Root: root, ChangedOnly: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil {
		t.Fatalf("changed-only with missing baseline: %v", err)
	}
	if len(first) != len(full) {
		t.Errorf("first run with missing baseline scanned %d files, want full %d", len(first), len(full))
	}
}

// TestFilterChangedDoesNotMutateCallerSlice guards
// fix-filterchanged-aliasing-corrupts-baseline: filterChanged once did
// `out := files[:0]`, compacting the kept files into the caller's backing
// array.  runCheck's rolling-baseline pattern `--changed-only X
// --write-baseline X` calls FilterChanged(files) and THEN BaselineBytes(files)
// on the SAME slice, so the in-place compaction clobbered the unchanged,
// filtered-out files and dropped them from the baseline — re-scanning every
// previously-unchanged package on the next run.  filterChanged must return a
// fresh slice and leave the input untouched.
func TestFilterChangedDoesNotMutateCallerSlice(t *testing.T) {
	// Three files; only the middle one is "changed" relative to the baseline,
	// so a buggy in-place compaction would shift it to index 0 and overwrite
	// the surrounding unchanged entries.
	files := []File{
		{DisplayPath: "a/README.md", Content: "alpha"},
		{DisplayPath: "b/README.md", Content: "bravo-CHANGED"},
		{DisplayPath: "c/README.md", Content: "charlie"},
	}
	wantPaths := []string{"a/README.md", "b/README.md", "c/README.md"}

	// Baseline records the ORIGINAL hashes of a and c (unchanged) but a stale
	// hash for b, so only b is "changed".
	base := baselineFile{Hashes: map[string]string{
		"a/README.md": hashContent("alpha"),
		"b/README.md": hashContent("bravo"), // stale → b counts as changed
		"c/README.md": hashContent("charlie"),
	}}

	changed := filterChanged(files, base)

	// 1) The filtered result is exactly the one changed file.
	if len(changed) != 1 || changed[0].DisplayPath != "b/README.md" {
		t.Fatalf("filterChanged = %v, want exactly [b/README.md]", displayPaths(changed))
	}

	// 2) The caller's slice is untouched: same length, same DisplayPaths in
	// order.  A `files[:0]` aliasing bug would have compacted b to index 0.
	if len(files) != 3 {
		t.Fatalf("caller slice length = %d after filterChanged, want 3 (slice was mutated)", len(files))
	}
	for i, want := range wantPaths {
		if files[i].DisplayPath != want {
			t.Errorf("files[%d].DisplayPath = %q after filterChanged, want %q (in-place mutation corrupted the caller's slice)",
				i, files[i].DisplayPath, want)
		}
	}

	// 3) The downstream consequence runCheck depends on: a baseline written
	// from the post-filter `files` slice still covers EVERY file's DisplayPath.
	data, err := BaselineBytes(files)
	if err != nil {
		t.Fatalf("BaselineBytes: %v", err)
	}
	var bf baselineFile
	if err := json.Unmarshal(data, &bf); err != nil {
		t.Fatalf("unmarshal baseline: %v", err)
	}
	for _, want := range wantPaths {
		if _, ok := bf.Hashes[want]; !ok {
			t.Errorf("baseline written after FilterChanged is missing %q (unchanged package dropped from baseline)", want)
		}
	}
}

// TestRollingBaselineStaysStableAcrossRuns mirrors runCheck's
// `--changed-only X --write-baseline X` rolling-baseline CI pattern end to
// end: write the baseline from the FULL set after filtering, run twice, and
// assert the second changed-only scan still yields zero unchanged files.
// This is the integration-level guard for
// fix-filterchanged-aliasing-corrupts-baseline (and the v0.3.0
// fix-changed-only-write-baseline-incomplete it reinforces).
func TestRollingBaselineStaysStableAcrossRuns(t *testing.T) {
	root := testdataPath(t, "node_modules_fixture")
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")

	// roll reproduces runCheck's ordering exactly: Walk the FULL set, filter
	// against the existing baseline, write the new baseline from the FULL set
	// (not the filtered set), and return how many files the scan would cover.
	roll := func() (scanCount int) {
		full, err := Walk(Options{Root: root})
		if err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if len(full) == 0 {
			t.Fatal("full scan returned zero files")
		}
		narrowed, err := FilterChanged(full, baselinePath)
		if err != nil {
			t.Fatalf("FilterChanged: %v", err)
		}
		// Baseline is written from the FULL set, AFTER FilterChanged has run —
		// exactly runCheck's call order, the order that the aliasing bug
		// corrupted.
		data, err := BaselineBytes(full)
		if err != nil {
			t.Fatalf("BaselineBytes: %v", err)
		}
		if err := os.WriteFile(baselinePath, data, 0o644); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		return len(narrowed)
	}

	// First run: no baseline yet → everything is "changed".
	if got := roll(); got == 0 {
		t.Fatal("first run scanned 0 files, want the full set (missing baseline should scan everything)")
	}

	// Second run: nothing changed AND the baseline written by run 1 must have
	// covered every file (it would not have, under the aliasing bug, because
	// BaselineBytes read a corrupted slice) → zero files to re-scan.
	if got := roll(); got != 0 {
		t.Fatalf("second changed-only run scanned %d files, want 0 (baseline dropped unchanged packages — incremental CI silently broken)", got)
	}
}

// TestWalkEcosystemAliasHonoured guards fix-ecosystem-flag-value-mismatch:
// --ecosystem documents `node | python | go`, but the internal enumerator
// constants are `npm | pypi | go`.  The documented spellings "node" and
// "python" must drive the npm / pypi enumerators (case-insensitively),
// not be silently dropped — otherwise the scanner reports "no findings" on
// a tree that contains real payloads.
func TestWalkEcosystemAliasHonoured(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ecosystem string
		fixture   string
		wantEco   string
	}{
		{"node_alias_drives_npm", "node", "node_modules_fixture", "npm"},
		{"node_alias_case_insensitive", "NoDe", "node_modules_fixture", "npm"},
		{"npm_constant_still_works", "npm", "node_modules_fixture", "npm"},
		{"python_alias_drives_pypi", "python", "py_fixture", "pypi"},
		{"python_alias_case_insensitive", "Python", "py_fixture", "pypi"},
		{"pypi_constant_still_works", "pypi", "py_fixture", "pypi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testdataPath(t, tc.fixture)
			files, err := Walk(Options{Root: root, Ecosystems: []string{tc.ecosystem}})
			if err != nil {
				t.Fatalf("Walk: %v", err)
			}
			if len(files) == 0 {
				t.Fatalf("--ecosystem %s on %s returned zero files (alias not honoured; silent false-negative)", tc.ecosystem, tc.fixture)
			}
			for _, f := range files {
				if f.Ecosystem != tc.wantEco {
					t.Errorf("file %q ecosystem = %q, want %q", f.DisplayPath, f.Ecosystem, tc.wantEco)
				}
			}
		})
	}
}

// TestWalkEcosystemFilterSuppressesGenericFallback guards
// fix-generic-fallback-ignores-ecosystem-filter: when the user restricts to
// a specific ecosystem, the generic single-package fallback must NOT run,
// even if the root has its own README.  Otherwise --ecosystem go on a plain
// project leaks a generic-ecosystem finding and violates the declared filter.
func TestWalkEcosystemFilterSuppressesGenericFallback(t *testing.T) {
	// jqwik_fixture has a README at the root and no vendor/mod tree, so the
	// go enumerator produces nothing; without the gate the generic fallback
	// would surface that README as a generic-ecosystem hit.
	root := testdataPath(t, "jqwik_fixture")
	files, err := Walk(Options{Root: root, Ecosystems: []string{"go"}})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("--ecosystem go leaked %d file(s) through the generic fallback: %v", len(files), displayPaths(files))
	}
	for _, f := range files {
		if f.Ecosystem == "generic" {
			t.Errorf("expected no generic-ecosystem file under --ecosystem go, got %q", f.DisplayPath)
		}
	}
}

// TestWalkPyDocstringRealPath guards fix-py-docstring-synthetic-path: a
// Python docstring finding must report a real, navigable .py path on disk,
// not a synthetic "<pkg>/__doc__" aggregate that does not exist.
func TestWalkPyDocstringRealPath(t *testing.T) {
	root := testdataPath(t, "py_fixture")
	files, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var docs []File
	for _, f := range files {
		if f.Kind == "docstring" && f.Ecosystem == "pypi" {
			docs = append(docs, f)
		}
	}
	if len(docs) == 0 {
		t.Fatal("expected at least one pypi docstring file from py_fixture")
	}
	for _, d := range docs {
		if strings.HasSuffix(d.DisplayPath, "/__doc__") {
			t.Errorf("docstring DisplayPath %q is the synthetic __doc__ aggregate, want a real .py path", d.DisplayPath)
		}
		if !strings.HasSuffix(d.DisplayPath, ".py") {
			t.Errorf("docstring DisplayPath %q does not end in .py", d.DisplayPath)
		}
		if _, err := os.Stat(d.Path); err != nil {
			t.Errorf("docstring Path %q does not exist on disk: %v", d.Path, err)
		}
	}
}

// TestWalkPyDocstringsSplitPerFile guards fix-py-docstring-synthetic-path
// for multi-file packages: a package with several .py files must emit one
// docstring File per source file, each with its own real path, so the 2nd+
// file's docstrings are not attributed to the first .py path.
func TestWalkPyDocstringsSplitPerFile(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "site-packages", "multifile")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "__init__.py"),
		[]byte("\"\"\"init docstring for multifile.\"\"\"\n"), 0o644); err != nil {
		t.Fatalf("write __init__.py: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "second.py"),
		[]byte("\"\"\"second docstring for multifile.\"\"\"\n"), 0o644); err != nil {
		t.Fatalf("write second.py: %v", err)
	}

	files, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := map[string]bool{}
	for _, f := range files {
		if f.Kind == "docstring" {
			got[f.DisplayPath] = true
		}
	}
	want := []string{
		"site-packages/multifile/__init__.py",
		"site-packages/multifile/second.py",
	}
	for _, p := range want {
		if !got[p] {
			t.Errorf("expected a docstring File at %q; docstring paths found: %v", p, keysOf(got))
		}
	}
}

// TestWalkGoDocsSplitPerFile guards the Go mirror of
// fix-py-docstring-synthetic-path: a Go module with multiple top-level .go
// files must emit one docstring File per source file, each with its own real
// path, so the 2nd+ file's package comment is not attributed to the first.
func TestWalkGoDocsSplitPerFile(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "vendor", "example.com", "twofiles")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mod, "go.mod"),
		[]byte("module example.com/twofiles\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mod, "a.go"),
		[]byte("// Package twofiles file a.\npackage twofiles\n"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mod, "b.go"),
		[]byte("// Package twofiles file b.\npackage twofiles\n"), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}

	files, err := Walk(Options{Root: root})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := map[string]bool{}
	for _, f := range files {
		if f.Kind == "docstring" {
			got[f.DisplayPath] = true
		}
	}
	want := []string{
		"vendor/example.com/twofiles/a.go",
		"vendor/example.com/twofiles/b.go",
	}
	for _, p := range want {
		if !got[p] {
			t.Errorf("expected a docstring File at %q; docstring paths found: %v", p, keysOf(got))
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func testdataPath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func anyHasSuffix(files []File, suffix string) bool {
	for _, f := range files {
		if strings.HasSuffix(f.DisplayPath, suffix) {
			return true
		}
	}
	return false
}

func anyContains(files []File, needle string) bool {
	lo := strings.ToLower(needle)
	for _, f := range files {
		if strings.Contains(strings.ToLower(f.Content), lo) {
			return true
		}
	}
	return false
}

func displayPaths(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.DisplayPath)
	}
	return out
}

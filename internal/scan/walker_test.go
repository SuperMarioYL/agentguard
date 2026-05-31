package scan

import (
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

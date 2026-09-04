//go:build unix

package scan

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestPyDocstringFifoSkipped guards fix-source-reader-unguarded-open-dos for
// the Python source extractor: a non-regular __init__.py (a FIFO named
// pipe, standing in for a symlink to /dev/zero) must not be opened.
// extractPyDocstrings previously called os.Open directly with no IsRegular
// guard; os.Open blocks forever on the FIFO open, so a single malicious
// Python package shipping __init__.py as a FIFO hung the whole scan. With
// the isRegularFile guard the FIFO is skipped and the scan completes.
//
// Revert check: drop the isRegularFile guard from loadPyDocstrings and this
// test hangs (caught by the timeout). unix-only because it uses
// syscall.Mkfifo.
func TestPyDocstringFifoSkipped(t *testing.T) {
	root := t.TempDir()
	sp := filepath.Join(root, "site-packages", "evilpy")
	if err := os.MkdirAll(sp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(sp, "__init__.py"), 0o644); err != nil {
		t.Skipf("mkfifo unsupported on this platform: %v", err)
	}
	done := make(chan struct{})
	var files []File
	go func() {
		defer close(done)
		files, _ = Walk(Options{Root: root})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Walk hung: non-regular __init__.py (FIFO) was opened instead of skipped")
	}
	for _, f := range files {
		if f.Kind == "docstring" {
			t.Errorf("expected FIFO __init__.py to be skipped, got docstring File %q; files=%v", f.DisplayPath, displayPaths(files))
		}
	}
}

// TestGoPackageCommentFifoSkipped guards fix-source-reader-unguarded-open-dos
// for the Go source extractor: a non-regular doc.go (a FIFO) must not be
// opened. extractGoPackageComment previously called os.Open directly with
// no IsRegular guard; a FIFO doc.go blocked the open and hung the scan.
// With the isRegularFile guard the FIFO is skipped.
//
// Revert check: drop the guard from loadGoPackageDocs and this test hangs.
// unix-only because it uses syscall.Mkfifo.
func TestGoPackageCommentFifoSkipped(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "vendor", "example.com", "evilgo")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/evilgo\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(mod, "doc.go"), 0o644); err != nil {
		t.Skipf("mkfifo unsupported on this platform: %v", err)
	}
	done := make(chan struct{})
	var files []File
	go func() {
		defer close(done)
		files, _ = Walk(Options{Root: root})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Walk hung: non-regular doc.go (FIFO) was opened instead of skipped")
	}
	for _, f := range files {
		if f.Kind == "docstring" {
			t.Errorf("expected FIFO doc.go to be skipped, got docstring File %q; files=%v", f.DisplayPath, displayPaths(files))
		}
	}
}

// TestReadModDirectiveFifoSkipped guards fix-source-reader-unguarded-open-dos
// for the go.mod reader: a non-regular go.mod (a FIFO) must not be opened.
// readModDirective previously called os.Open directly; a FIFO go.mod blocked
// the open and hung the module-cache enumeration. With the regularBounded
// guard the FIFO is skipped and goModuleLabel falls back to the directory
// basename.
//
// Revert check: drop the guard from readModDirective and this test hangs.
// unix-only because it uses syscall.Mkfifo.
func TestReadModDirectiveFifoSkipped(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "github.com", "owner", "repo@v1.2.3")
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(mod, "go.mod"), 0o644); err != nil {
		t.Skipf("mkfifo unsupported on this platform: %v", err)
	}
	// A README keeps the directory a recognised module root so
	// findGoModuleRoots returns it.
	if err := os.WriteFile(filepath.Join(mod, "README.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Walk(Options{Root: root})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Walk hung: non-regular go.mod (FIFO) was opened instead of skipped")
	}
}

// TestVendorModulesTxtFifoSkipped guards fix-source-reader-unguarded-open-dos
// for the modules.txt reader: a non-regular modules.txt (a FIFO) must not be
// opened. findVendorPackageDirs previously called os.Open directly on
// vendor/modules.txt; a FIFO modules.txt blocked the open and hung the
// vendor enumeration. With the regularBounded guard the FIFO is skipped and
// the subdir-enumeration fallback runs instead.
//
// Revert check: drop the guard and this test hangs. unix-only (Mkfifo).
func TestVendorModulesTxtFifoSkipped(t *testing.T) {
	root := t.TempDir()
	vendor := filepath.Join(root, "vendor")
	pkg := filepath.Join(vendor, "example.com", "foo")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	// A .go file makes the subdir pass the dirHasGoProse fallback.
	if err := os.WriteFile(filepath.Join(pkg, "doc.go"), []byte("// clean\npackage foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(vendor, "modules.txt"), 0o644); err != nil {
		t.Skipf("mkfifo unsupported on this platform: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Walk(Options{Root: root})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Walk hung: non-regular modules.txt (FIFO) was opened instead of skipped")
	}
}

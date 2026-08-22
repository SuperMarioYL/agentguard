//go:build unix

package scan

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestReadPackageJSONNonRegularSkipped guards
// fix-npm-package-json-unguarded-readfile: a non-regular package.json (a FIFO
// named pipe, standing in for a symlink to /dev/zero) must not be read
// wholesale. readPackageJSON previously called os.ReadFile directly with no
// IsRegular/size guard; os.ReadFile blocks forever on the FIFO open (and on
// /dev/zero grows the buffer until OOM), so a single malicious npm package
// hung or crashed the whole scan — defeating the security tool. With the guard
// os.Stat reports a non-regular file and the candidate is skipped (the label
// falls back to the nameHint), so the scan completes without reading it.
//
// This guards the LABEL reader readPackageJSON, distinct from
// loadPackageJSONProse (the prose reader) whose oversized skip is already
// covered by TestPackageJSONProseOversizedSkipped.
//
// A FIFO is used instead of a /dev/zero symlink so a regression (guard removed)
// hangs cleanly on the blocked open instead of OOM-crashing the test process.
//
// Revert check: drop the IsRegular guard from readPackageJSON and this test
// hangs (caught by the timeout).
//
// unix-only because it uses syscall.Mkfifo; the oversized-package.json guard
// is also covered portably by TestPackageJSONProseOversizedSkipped.
func TestReadPackageJSONNonRegularSkipped(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "node_modules", "evilpkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(pkgDir, "package.json")
	if err := syscall.Mkfifo(fifoPath, 0o644); err != nil {
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
		t.Fatal("Walk hung: non-regular package.json (FIFO) was read instead of skipped")
	}
	for _, f := range files {
		if f.Kind == "metadata" {
			t.Errorf("expected FIFO package.json to be skipped, got metadata File %q; files=%v", f.DisplayPath, displayPaths(files))
		}
	}
}

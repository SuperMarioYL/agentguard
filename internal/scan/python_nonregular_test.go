//go:build unix

package scan

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestPyMetadataNonRegularSkipped guards
// fix-py-metadata-unguarded-readfile: a non-regular METADATA (a FIFO named
// pipe, standing in for a symlink to /dev/zero) must not be read wholesale.
// loadPyMetadata previously called os.ReadFile directly with no IsRegular
// guard; os.ReadFile blocks forever on the FIFO open (and on /dev/zero grows
// the buffer until OOM). With the guard os.Stat reports a non-regular file and
// the candidate is skipped, so the scan completes without reading it.
//
// A FIFO is used instead of a /dev/zero symlink so a regression (guard removed)
// hangs cleanly on the blocked open instead of OOM-crashing the test process.
//
// Revert check: drop the IsRegular guard from loadPyMetadata and this test
// hangs (caught by the timeout).
//
// unix-only because it uses syscall.Mkfifo; the oversized-METADATA guard is
// also covered portably by TestPyMetadataOversizedSkipped.
func TestPyMetadataNonRegularSkipped(t *testing.T) {
	root := t.TempDir()
	distInfo := filepath.Join(root, "site-packages", "evilpy-1.0.0.dist-info")
	if err := os.MkdirAll(distInfo, 0o755); err != nil {
		t.Fatal(err)
	}
	fifoPath := filepath.Join(distInfo, "METADATA")
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
		t.Fatal("Walk hung: non-regular METADATA (FIFO) was read instead of skipped")
	}
	for _, f := range files {
		if f.Kind == "metadata" {
			t.Errorf("expected FIFO METADATA to be skipped, got metadata File %q; files=%v", f.DisplayPath, displayPaths(files))
		}
	}
}

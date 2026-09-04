package scan

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGoModuleLabelNestedV2 guards fix-gomod-label-nested-v2-module: a
// module-cache directory whose import path has MORE than three segments
// (the canonical Go /v2 convention, github.com/owner/repo/v2@v2.0.0) used
// to be labelled by a 3-segment parent-chain reconstruction that took
// filepath.Base of the wrong segment, misattributing the host (owner
// printed where github.com belongs). The fix prefers the go.mod `module`
// directive — authoritative for any segment count — so the label carries
// the real host.
//
// The cache dir layout is simulated by creating the real directories and a
// go.mod whose module directive is the canonical import path.
func TestGoModuleLabelNestedV2(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		relDir  string // relative to root
		goMod   string // go.mod content
		want    string
	}{
		{
			relDir: filepath.Join("github.com", "owner", "repo", "v2@v2.0.0"),
			goMod:  "module github.com/owner/repo/v2\n\ngo 1.24\n",
			want:   "github.com/owner/repo/v2@v2.0.0",
		},
		{
			relDir: filepath.Join("github.com", "owner", "repo@v1.2.3"),
			goMod:  "module github.com/owner/repo\n\ngo 1.24\n",
			want:   "github.com/owner/repo@v1.2.3",
		},
		{
			relDir: filepath.Join("golang.org", "x", "net@v0.20.0"),
			goMod:  "module golang.org/x/net\n\ngo 1.24\n",
			want:   "golang.org/x/net@v0.20.0",
		},
	}
	for _, c := range cases {
		dir := filepath.Join(root, c.relDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", c.relDir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(c.goMod), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		got := goModuleLabel(dir)
		if got != c.want {
			t.Errorf("goModuleLabel(%q) = %q, want %q", c.relDir, got, c.want)
		}
	}
}

package scan

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// walkGoModuleCache enumerates every module inside a Go module cache or
// vendor directory and extracts the prose channels each one ships:
// READMEs at any nesting depth, package-level doc.go comments, and
// CHANGELOGs that ride along in the same directory as the module root.
//
// It is called by Walk on three trees:
//
//	<root>/vendor/...                        (in-repo vendored modules)
//	$GOPATH/pkg/mod/<host>/<owner>/<repo>@<version>/...
//	~/go/pkg/mod/<host>/<owner>/<repo>@<version>/...
//
// The cache layout uses '@<version>' as the version separator and a
// flat host/owner/repo nesting underneath cache.download.  We restrict
// ourselves to module roots — directories that contain a `go.mod` —
// because the cache also ships per-version checksum metadata
// directories that contain no prose.
func walkGoModuleCache(dir, root string) ([]File, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("scan/gomod: stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan/gomod: %q is not a directory", dir)
	}

	var out []File
	// First pass: collect every directory that owns a go.mod.  This is
	// the canonical "module root" marker for both vendored trees and
	// $GOPATH/pkg/mod layouts.
	modRoots := findGoModuleRoots(dir)
	if len(modRoots) == 0 {
		// Fall back to treating the input directory as a single module;
		// covers tiny fixtures and the case where a user points us at a
		// freshly extracted module tarball.
		modRoots = []string{dir}
	}

	for _, mr := range modRoots {
		label := goModuleLabel(mr)
		out = append(out, extractGoModule(mr, root, label)...)
	}
	return out, nil
}

// findGoModuleRoots returns every directory under start that contains a
// go.mod file.  Nested modules (a sub-module inside a parent module) are
// each returned independently so the scanner reports prose against the
// module that owns the file.  The walker is best-effort — unreadable
// subdirectories are skipped silently.
func findGoModuleRoots(start string) []string {
	var roots []string
	_ = filepath.Walk(start, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		switch base {
		case ".git", ".hg", ".svn":
			return filepath.SkipDir
		}
		if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
			roots = append(roots, path)
		}
		return nil
	})
	return roots
}

// goModuleLabel derives a "module@version" label from the module-cache
// directory name when possible, or from the go.mod `module` directive
// for vendored modules that have no version suffix.
func goModuleLabel(modDir string) string {
	base := filepath.Base(modDir)
	if at := strings.LastIndex(base, "@"); at > 0 {
		// Cache layout: github.com/owner/repo@v1.2.3 lives under
		// .../repo@v1.2.3, so the parent chain has the prefix.
		parent := filepath.Dir(modDir)
		owner := filepath.Base(parent)
		host := filepath.Base(filepath.Dir(parent))
		modName := base[:at]
		version := base[at+1:]
		if host != "." && owner != "." {
			return fmt.Sprintf("%s/%s/%s@%s", host, owner, modName, version)
		}
		return base[:at] + "@" + version
	}
	if name := readModDirective(filepath.Join(modDir, "go.mod")); name != "" {
		return name
	}
	return base
}

// readModDirective returns the module path from go.mod's `module` line,
// or "" when the file is unreadable or does not start with one.
func readModDirective(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	br := bufio.NewScanner(f)
	br.Buffer(make([]byte, 0, 32*1024), 1<<20)
	for br.Scan() {
		line := strings.TrimSpace(br.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "module ") || strings.HasPrefix(line, "module\t") {
			rest := strings.TrimSpace(line[len("module"):])
			rest = strings.Trim(rest, "\"")
			return rest
		}
		break
	}
	return ""
}

// extractGoModule reads the prose channels that a Go consumer ships
// with a published module: top-level README + CHANGELOG, plus the
// package-level documentation block from every doc.go and the top-level
// .go file in the module root.  Deeper directories are not walked for
// docstrings because the m2 budget targets the surface that a coding
// agent typically ingests when summarising a dependency.
func extractGoModule(modDir, root, label string) []File {
	var out []File

	readmeNames := []string{
		"README.md", "README.markdown", "README.MD",
		"Readme.md", "readme.md", "README",
		"README.rst", "README.txt",
	}
	for _, n := range readmeNames {
		if f, err := loadProseFile(filepath.Join(modDir, n), root, label, ecosystemGo, "readme"); err == nil && f != nil {
			out = append(out, *f)
			break
		}
	}

	changelogNames := []string{
		"CHANGELOG.md", "CHANGES.md", "HISTORY.md",
		"CHANGELOG", "CHANGELOG.markdown", "CHANGELOG.txt",
	}
	for _, n := range changelogNames {
		if f, err := loadProseFile(filepath.Join(modDir, n), root, label, ecosystemGo, "changelog"); err == nil && f != nil {
			out = append(out, *f)
			break
		}
	}

	// doc.go (and the canonical "package <name> // doc" preamble of any
	// top-level .go file) carry the prose surface that gopkg.in and
	// pkg.go.dev render as the module's overview — and that a coding
	// agent reads when it fetches the module to reason about it.
	if doc := loadGoPackageDocs(modDir, root, label); doc != nil {
		out = append(out, *doc)
	}
	return out
}

// loadGoPackageDocs concatenates the leading // and /* ... */ comment
// blocks that immediately precede the `package` clause of every .go
// file directly under modDir into a single synthetic File.  Comments
// inside function bodies are intentionally ignored — they do not appear
// in the rendered package documentation surface and are unlikely to be
// summarised by an agent that is sizing up the module.
func loadGoPackageDocs(modDir, root, label string) *File {
	entries, err := os.ReadDir(modDir)
	if err != nil {
		return nil
	}

	var goFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		goFiles = append(goFiles, filepath.Join(modDir, name))
	}
	if len(goFiles) == 0 {
		return nil
	}

	var (
		firstPath string
		body      strings.Builder
		lines     []string
	)
	for _, gf := range goFiles {
		docLines := extractGoPackageComment(gf)
		if len(docLines) == 0 {
			continue
		}
		if firstPath == "" {
			firstPath = gf
		}
		for _, ln := range docLines {
			body.WriteString(ln)
			body.WriteString("\n")
			lines = append(lines, ln)
		}
	}
	if firstPath == "" {
		return nil
	}

	rel, err := filepath.Rel(root, firstPath)
	if err != nil {
		rel = firstPath
	}
	return &File{
		Path:        firstPath,
		DisplayPath: filepath.ToSlash(rel),
		Package:     label,
		Ecosystem:   ecosystemGo,
		Kind:        "docstring",
		Content:     body.String(),
		Lines:       lines,
	}
}

// extractGoPackageComment returns the text of the contiguous comment
// block (line or block style) that immediately precedes the `package`
// keyword in a .go file.  Comment markers are stripped so the detector
// sees prose, not syntax.
//
// Subsequent doc comments inside the file are ignored — those attach to
// declarations, and m2's threat model is the package-level summary that
// pkg.go.dev surfaces.
func extractGoPackageComment(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	br := bufio.NewScanner(f)
	br.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		comments []string
		buf      []string
		inBlock  bool
	)
	for br.Scan() {
		line := br.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// Blank line resets the running line-comment block unless
			// we are mid /* ... */ (which has its own terminator).
			if !inBlock {
				buf = nil
			}
			continue
		}
		if inBlock {
			if idx := strings.Index(line, "*/"); idx >= 0 {
				body := line[:idx]
				if t := strings.TrimSpace(strings.TrimPrefix(body, "*")); t != "" {
					buf = append(buf, t)
				}
				comments = append(comments, buf...)
				buf = nil
				inBlock = false
				continue
			}
			body := strings.TrimSpace(line)
			body = strings.TrimPrefix(body, "*")
			body = strings.TrimSpace(body)
			if body != "" {
				buf = append(buf, body)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			body := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
			buf = append(buf, body)
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			rest := strings.TrimPrefix(trimmed, "/*")
			if idx := strings.Index(rest, "*/"); idx >= 0 {
				body := strings.TrimSpace(rest[:idx])
				if body != "" {
					buf = append(buf, body)
				}
				continue
			}
			body := strings.TrimSpace(rest)
			if body != "" {
				buf = append(buf, body)
			}
			inBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "package ") {
			comments = append(comments, buf...)
			return comments
		}
		// Hit an import or other declaration before the package
		// clause — comments above that are unrelated to the package
		// summary, so reset and keep scanning until package appears.
		buf = nil
	}
	return comments
}

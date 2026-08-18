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
	// the canonical "module root" marker for the $GOPATH/pkg/mod layout.
	modRoots := findGoModuleRoots(dir)
	if len(modRoots) == 0 {
		// A real `go mod vendor` tree strips go.mod from EVERY vendored
		// package, so findGoModuleRoots returns nothing and the old
		// single-dir fallback scanned only vendor/ itself — whose direct
		// children are import-path segments, not prose — yielding ZERO
		// files (a silent false-negative on a directory the docs list as
		// scanned).  Recover the real vendored package dirs instead.
		if vendorRoots := findVendorPackageDirs(dir); len(vendorRoots) > 0 {
			modRoots = vendorRoots
		} else {
			// Fall back to treating the input directory as a single module;
			// covers tiny fixtures and the case where a user points us at a
			// freshly extracted module tarball.
			modRoots = []string{dir}
		}
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

// findVendorPackageDirs recovers the vendored package directories from a
// `go mod vendor` tree.  `go mod vendor` strips go.mod from every vendored
// package, so findGoModuleRoots finds nothing and the caller would otherwise
// scan the bare vendor/ directory (no prose) and report zero files.
//
// It prefers vendor/modules.txt — the canonical list `go mod vendor` writes,
// whose bare (non-'#') lines are the vendored package import paths — and falls
// back to enumerating every subdirectory that directly owns a README or a
// non-test .go file.  Returns nil when dir is not a vendor tree (no
// modules.txt and no prose-bearing subdir), so the caller keeps its
// single-directory fixture fallback.
func findVendorPackageDirs(dir string) []string {
	var roots []string
	seen := make(map[string]bool)
	add := func(p string) {
		if seen[p] {
			return
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			roots = append(roots, p)
			seen[p] = true
		}
	}

	// Primary: parse vendor/modules.txt for the canonical package list.
	if f, err := os.Open(filepath.Join(dir, "modules.txt")); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				// '# <module> <version>' declarations and '## <annotation>'
				// lines are not package import paths.
				continue
			}
			// Containment check: filepath.Join cleans ".." segments, so a
			// crafted modules.txt line such as "../../external" (or an
			// absolute path) resolves to a real directory OUTSIDE the vendor
			// scan root and is then read by extractGoModule, leaking external
			// prose into the report. For a supply-chain scanner whose threat
			// model is untrusted dependency trees, a malicious vendored
			// package shipping a crafted modules.txt must not escape the scan
			// root. Resolve the joined path and skip the entry when it escapes
			// dir (a lexical check on cleaned paths); legitimate import paths
			// like "example.com/owner/pkg" stay within dir.
			imp := filepath.FromSlash(line)
			if filepath.IsAbs(imp) {
				continue
			}
			resolved := filepath.Join(dir, imp)
			rel, err := filepath.Rel(dir, resolved)
			if err != nil || strings.HasPrefix(rel, "..") {
				continue
			}
			add(resolved)
		}
		_ = f.Close()
		if len(roots) > 0 {
			return roots
		}
	}

	// Fallback: no usable modules.txt — enumerate every subdirectory that
	// directly owns prose (a README or a non-test .go file) and treat each as
	// a module root.  This covers hand-assembled vendor trees and matches
	// extractGoModule's per-directory reading semantics.
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			return nil
		}
		switch filepath.Base(path) {
		case ".git", ".hg", ".svn":
			return filepath.SkipDir
		}
		if dirHasGoProse(path) {
			add(path)
		}
		return nil
	})
	return roots
}

// dirHasGoProse reports whether dir directly contains a README or a non-test
// .go file — the prose surface extractGoModule reads for a module root.
func dirHasGoProse(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(strings.ToLower(name), "readme") {
			return true
		}
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			return true
		}
	}
	return false
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
	out = append(out, loadGoPackageDocs(modDir, root, label)...)
	return out
}

// loadGoPackageDocs extracts the leading // and /* ... */ comment block
// that immediately precedes the `package` clause of every .go file
// directly under modDir, emitting one File per source file so a finding
// reports the real .go path that owns the comment.  Comments inside
// function bodies are intentionally ignored — they do not appear in the
// rendered package documentation surface and are unlikely to be
// summarised by an agent that is sizing up the module.
func loadGoPackageDocs(modDir, root, label string) []File {
	entries, err := os.ReadDir(modDir)
	if err != nil {
		return nil
	}

	var out []File
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
		gf := filepath.Join(modDir, name)
		blocks := extractGoPackageComment(gf)
		if len(blocks) == 0 {
			continue
		}
		var (
			body  strings.Builder
			lines []string
		)
		// Place each comment block's lines at the block's REAL source line
		// so the detector's 1-based lineNo over Content matches the true .go
		// source line, instead of counting from a single captured startLine
		// (which drifted the package-doc payload when a multi-line `/* license
		// */` header flushed before the `// Package` doc block, reporting the
		// payload at the license line instead of its real source line).
		// d.lines[0] sits on source line block.startLine, so body line j
		// lands at Content index startLine-1+j; when a prior block already
		// filled the target index the new line is joined in.
		for _, b := range blocks {
			for j, ln := range b.lines {
				idx := b.startLine - 1 + j
				for len(lines) <= idx {
					lines = append(lines, "")
				}
				if lines[idx] == "" {
					lines[idx] = ln
				} else {
					lines[idx] = lines[idx] + " " + ln
				}
			}
		}
		for _, ln := range lines {
			body.WriteString(ln)
			body.WriteString("\n")
		}
		rel, err := filepath.Rel(root, gf)
		if err != nil {
			rel = gf
		}
		out = append(out, File{
			Path:        gf,
			DisplayPath: filepath.ToSlash(rel),
			Package:     label,
			Ecosystem:   ecosystemGo,
			Kind:        "docstring",
			Content:     body.String(),
			Lines:       lines,
		})
	}
	return out
}

// goCommentBlock is one contiguous comment block (line or block style)
// immediately preceding the `package` clause, anchored to the real source
// line of its first line so loadGoPackageDocs can place every block at its
// own navigable line.
type goCommentBlock struct {
	startLine int
	lines     []string
}

// extractGoPackageComment returns the contiguous comment blocks (line or
// block style) that immediately precede the `package` keyword in a .go file,
// each anchored to the source line of its first line.  Comment markers are
// stripped so the detector sees prose, not syntax.  The per-block startLine
// lets loadGoPackageDocs pad the assembled Content so a finding's reported
// line matches the real .go source line rather than an index into the
// stripped comment body — including the common license-header-then-package-doc
// case where a multi-line `/* license */` header flushes before the `//
// Package` doc block: capturing startLine only on the first flush (the old
// behaviour) drifted the package-doc payload to the license line.
//
// Subsequent doc comments inside the file are ignored — those attach to
// declarations, and m2's threat model is the package-level summary that
// pkg.go.dev surfaces.
func extractGoPackageComment(path string) []goCommentBlock {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	br := bufio.NewScanner(f)
	br.Buffer(make([]byte, 0, 64*1024), MaxScanLineBytes)
	// A single physical line longer than MaxScanLineBytes (e.g. a >1 MiB
	// minified blob) must not abort the scan and silently drop every
	// package-comment block after it — the same false-negative class
	// ScanAll guards against.  The shared long-line-tolerant split
	// truncates an over-long line to a rune-safe prefix and keeps
	// scanning, so a payload comment after an over-long line is still
	// extracted, and lineNo stays faithful to the source (the over-long
	// line counts as exactly one logical line).
	br.Split(NewSplitLongTolerant())

	var (
		blocks   []goCommentBlock
		buf      []string
		inBlock  bool
		lineNo   int
		bufStart int
	)
	// add appends a stripped comment line to buf, recording the source line
	// of the block's first line so the caller can anchor Content to it.
	add := func(s string) {
		if len(buf) == 0 {
			bufStart = lineNo
		}
		buf = append(buf, s)
	}
	// flush emits buf as its own comment block (carrying bufStart as the
	// block's startLine) and resets buf.  Unlike the old single-startLine
	// flush, each block keeps its own real source line so a multi-block
	// header-then-doc file does not drift the package-doc line.
	flush := func() {
		if len(buf) > 0 {
			blocks = append(blocks, goCommentBlock{startLine: bufStart, lines: buf})
		}
		buf = nil
	}
	for br.Scan() {
		lineNo++
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
					add(t)
				}
				flush()
				inBlock = false
				// After a block-comment closer, the remainder of the SAME
				// physical line may carry the `package` clause (e.g.
				// `*/ package foo`). The package check below only fires for
				// lines whose trimmed form STARTS with "package ", so without
				// re-checking the remainder here the scan runs past the real
				// package clause and captures in-function comments as
				// package-doc blocks. Re-check the remainder and halt.
				if strings.HasPrefix(strings.TrimSpace(line[idx+2:]), "package ") {
					return blocks
				}
				continue
			}
			body := strings.TrimSpace(line)
			body = strings.TrimPrefix(body, "*")
			body = strings.TrimSpace(body)
			if body != "" {
				add(body)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "//") {
			body := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
			add(body)
			continue
		}
		if strings.HasPrefix(trimmed, "/*") {
			rest := strings.TrimPrefix(trimmed, "/*")
			if idx := strings.Index(rest, "*/"); idx >= 0 {
				body := strings.TrimSpace(rest[:idx])
				if body != "" {
					add(body)
				}
				// A single-line /* body */ block finds its closer on the
				// same line and must be emitted immediately — matching the
				// multi-line closer path, which calls flush() at its */.
				// Without this, the body sits in buf until the next blank
				// line, whose handler does buf = nil (inBlock is false),
				// silently discarding the whole comment body.
				flush()
				// Symmetric with the multi-line closer: the remainder of
				// this line may carry the `package` clause (e.g.
				// `/* license */ package foo`). Re-check it before
				// continuing so the scan halts at the real package clause
				// instead of capturing later in-function comments.
				if strings.HasPrefix(strings.TrimSpace(rest[idx+2:]), "package ") {
					return blocks
				}
				continue
			}
			body := strings.TrimSpace(rest)
			if body != "" {
				add(body)
			}
			inBlock = true
			continue
		}
		if strings.HasPrefix(trimmed, "package ") {
			flush()
			return blocks
		}
		// Hit an import or other declaration before the package
		// clause — comments above that are unrelated to the package
		// summary, so reset and keep scanning until package appears.
		buf = nil
	}
	return blocks
}

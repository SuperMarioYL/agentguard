// Package scan walks a project tree and extracts the prose files that a
// coding agent reads when reasoning about a dependency — README,
// CHANGELOG, and the description / keywords fields of package manifests.
// Source-code files are intentionally never inspected; the entire threat
// model concerns the natural-language surface area of installed packages.
//
// The exported entry point is Walk.  Ecosystem-specific enumerators live
// in sibling files (node.go for npm today; python.go and gomod.go land in
// m2).  The walker is best-effort: I/O errors on individual files are
// elided so a single permission-denied directory cannot abort the scan.
package scan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File is one prose channel extracted from a single package.  A package
// usually contributes one to three Files (README, CHANGELOG, manifest
// description) but the count is not bounded.
type File struct {
	// Path is the absolute path on disk.
	Path string
	// DisplayPath is the path relative to the scan root, used for output.
	DisplayPath string
	// Package labels the package this file belongs to, e.g. "jqwik@1.9.2".
	Package string
	// Ecosystem is one of "npm", "pypi", "go", or "generic".
	Ecosystem string
	// Kind is one of "readme", "changelog", "metadata", or "docstring".
	Kind string
	// Content is the file body, with CRLF normalised to LF.
	Content string
	// Lines is Content split on '\n', for 1-indexed line lookup.
	Lines []string
}

// Options configures Walk.
type Options struct {
	// Root is the directory to scan.  Required.
	Root string
	// Ecosystems restricts the scan to a subset of enumerators.  nil or
	// empty means all detected ecosystems.  Recognised values (case- and
	// whitespace-insensitive, with the documented aliases honoured):
	// "node"/"npm", "python"/"pypi", "go".
	Ecosystems []string
	// ChangedOnly is the path to a JSON baseline produced by a previous
	// run.  When set, Walk drops every prose file whose content hash
	// already appears in the baseline under the same display path, so a
	// repeat CI scan only re-checks packages whose prose actually changed.
	// When empty, every package is scanned.  See WriteBaseline / the
	// agentguard.baseline.json format.
	ChangedOnly string
}

// baselineFile is the on-disk JSON shape consumed by --changed-only and
// emitted by WriteBaseline: a map from display path to the SHA-256 hex
// digest of that file's normalised content at the time of the run.
type baselineFile struct {
	// Hashes maps DisplayPath -> sha256 hex of File.Content.
	Hashes map[string]string `json:"hashes"`
}

// hashContent returns the SHA-256 hex digest of a file's normalised
// content, the value stored in and compared against the baseline.
func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// loadBaseline reads a --changed-only baseline file.  A missing file is
// treated as an empty baseline (first run) rather than an error, so the
// first incremental invocation scans everything and the caller can then
// write a fresh baseline.
func loadBaseline(path string) (baselineFile, error) {
	bf := baselineFile{Hashes: map[string]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return bf, nil
		}
		return bf, fmt.Errorf("scan: read baseline %q: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return bf, nil
	}
	if err := json.Unmarshal(data, &bf); err != nil {
		return bf, fmt.Errorf("scan: parse baseline %q: %w", path, err)
	}
	if bf.Hashes == nil {
		bf.Hashes = map[string]string{}
	}
	return bf, nil
}

// BaselineBytes serialises the prose files into a baseline document that
// a later --changed-only run can diff against.  Callers persist it
// alongside their lockfile so repeat scans stay incremental.
func BaselineBytes(files []File) ([]byte, error) {
	bf := baselineFile{Hashes: make(map[string]string, len(files))}
	for _, f := range files {
		bf.Hashes[f.DisplayPath] = hashContent(f.Content)
	}
	return json.MarshalIndent(bf, "", "  ")
}

// filterChanged drops every file whose (DisplayPath, content-hash) pair
// already appears in the baseline, returning only the prose that is new
// or modified relative to the baseline run.
func filterChanged(files []File, base baselineFile) []File {
	out := files[:0]
	for _, f := range files {
		if prev, ok := base.Hashes[f.DisplayPath]; ok && prev == hashContent(f.Content) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// FilterChanged drops every file whose (DisplayPath, content-hash) pair
// already appears in the baseline at baselinePath, returning only the prose
// that is new or modified relative to that baseline run.  A missing
// baseline is treated as an empty baseline (first run), so the first
// incremental invocation scans everything.
//
// Narrowing is the caller's job, not Walk's: a caller that wants the
// rolling-baseline CI pattern (--changed-only X --write-baseline X) must
// write the baseline from the FULL file set and only then narrow, otherwise
// the baseline is rewritten from just the changed files and the next run
// re-scans every previously-unchanged package.  Walk therefore returns the
// full set and runCheck calls FilterChanged after writing the baseline.
func FilterChanged(files []File, baselinePath string) ([]File, error) {
	base, err := loadBaseline(baselinePath)
	if err != nil {
		return nil, err
	}
	return filterChanged(files, base), nil
}

const (
	ecosystemNPM     = "npm"
	ecosystemPyPI    = "pypi"
	ecosystemGo      = "go"
	ecosystemGeneric = "generic"
)

// normaliseEcosystemToken maps the user-facing spellings documented for
// --ecosystem (node | python | go) onto the internal enumerator constants
// (npm | pypi | go).  Without this alias map, wants() compared the user's
// "node" token to the "npm" constant with a bare strings.EqualFold and
// never matched, so --ecosystem node silently suppressed the npm enumerator
// and the scanner reported "no findings" on a tree that contained real
// payloads — the worst failure mode for a security tool.  Case- and
// whitespace-insensitive.
func normaliseEcosystemToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "node":
		return ecosystemNPM
	case "python":
		return ecosystemPyPI
	}
	return s
}

// maxProseBytes is a soft cap on a single prose file.  README payloads
// above 1 MiB are almost certainly machine-generated and not the threat
// surface this scanner exists to catch — silently skip them.
const maxProseBytes = 1 << 20

// Walk traverses opts.Root and returns every prose file it finds in any
// recognised package channel.  Returned Files are sorted by (Package,
// Kind, DisplayPath) so output is stable across runs.
func Walk(opts Options) ([]File, error) {
	if opts.Root == "" {
		return nil, fmt.Errorf("scan: Root is required")
	}
	info, err := os.Stat(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("scan: stat %q: %w", opts.Root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan: %q is not a directory", opts.Root)
	}

	rootAbs, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, fmt.Errorf("scan: abs %q: %w", opts.Root, err)
	}

	wants := func(eco string) bool {
		if len(opts.Ecosystems) == 0 {
			return true
		}
		for _, e := range opts.Ecosystems {
			if strings.EqualFold(normaliseEcosystemToken(e), eco) {
				return true
			}
		}
		return false
	}

	var files []File

	walkErr := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Tolerate permission and transient I/O errors on individual
			// entries; an unreadable node should not abort the whole scan.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		// Skip directories that never contain dependency prose channels.
		switch d.Name() {
		case ".git", ".hg", ".svn":
			return filepath.SkipDir
		}
		if path == rootAbs {
			return nil
		}
		switch d.Name() {
		case "node_modules":
			if wants(ecosystemNPM) {
				if npm, err := walkNodeModules(path, rootAbs); err == nil {
					files = append(files, npm...)
				}
			}
			return filepath.SkipDir
		case ".venv", "venv", "site-packages":
			if wants(ecosystemPyPI) {
				if py, err := walkPythonEnvironment(path, rootAbs); err == nil {
					files = append(files, py...)
				}
			}
			return filepath.SkipDir
		case "vendor", "mod":
			// `vendor` is the in-repo Go vendor tree; `mod` catches
			// $GOPATH/pkg/mod / ~/go/pkg/mod entry points when the user
			// scans the cache directly.
			if wants(ecosystemGo) {
				if g, err := walkGoModuleCache(path, rootAbs); err == nil {
					files = append(files, g...)
				}
			}
			return filepath.SkipDir
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scan: walk %q: %w", rootAbs, walkErr)
	}

	// Fallback for fixtures and single-package directories: when no
	// ecosystem-aware enumerator produced anything AND the user did not
	// restrict the scan to a specific ecosystem, treat the root as a
	// generic single-package source so the walker still yields a result.
	// Gating on Ecosystems honours the --ecosystem contract: asking for
	// "go" (or node/python) on a tree with no matching packages must yield
	// no findings, not leak a root README as a generic-ecosystem hit.
	if len(files) == 0 && len(opts.Ecosystems) == 0 {
		generic, _ := walkGenericPackage(rootAbs, rootAbs)
		files = append(files, generic...)
	}

	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Package != files[j].Package {
			return files[i].Package < files[j].Package
		}
		if files[i].Kind != files[j].Kind {
			return files[i].Kind < files[j].Kind
		}
		return files[i].DisplayPath < files[j].DisplayPath
	})

	// Incremental mode: when a baseline is supplied, drop every prose file
	// whose content is unchanged since the baseline run so a repeat CI scan
	// only re-checks packages whose prose actually changed.
	if opts.ChangedOnly != "" {
		base, err := loadBaseline(opts.ChangedOnly)
		if err != nil {
			return nil, err
		}
		files = filterChanged(files, base)
	}

	return files, nil
}

// walkGenericPackage treats dir as a single, unknown-ecosystem package and
// extracts its README and CHANGELOG.  Used both for tiny test fixtures
// and as the last-resort case when no ecosystem enumerator recognised the
// tree.
func walkGenericPackage(dir, root string) ([]File, error) {
	pkgName := filepath.Base(dir)
	var out []File

	candidates := []struct {
		names []string
		kind  string
	}{
		{[]string{"README.md", "README.markdown", "README.MD", "README.rst", "README.txt", "README", "readme.md", "Readme.md"}, "readme"},
		{[]string{"CHANGELOG.md", "CHANGELOG.markdown", "CHANGELOG.txt", "CHANGELOG", "HISTORY.md", "CHANGES.md"}, "changelog"},
	}
	for _, c := range candidates {
		for _, name := range c.names {
			f, err := loadProseFile(filepath.Join(dir, name), root, pkgName, ecosystemGeneric, c.kind)
			if err != nil {
				continue
			}
			if f != nil {
				out = append(out, *f)
				break
			}
		}
	}
	return out, nil
}

// loadProseFile reads a single file and wraps it as a *File ready for the
// detector.  Returns (nil, nil) when the file does not exist or is not a
// regular file; returns a non-nil error only for genuine I/O failures
// callers should propagate.
func loadProseFile(path, root, pkg, ecosystem, kind string) (*File, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	if info.Size() > maxProseBytes {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return &File{
		Path:        path,
		DisplayPath: filepath.ToSlash(rel),
		Package:     pkg,
		Ecosystem:   ecosystem,
		Kind:        kind,
		Content:     content,
		Lines:       strings.Split(content, "\n"),
	}, nil
}

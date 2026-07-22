package scan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// walkNodeModules enumerates every package inside an npm-style
// node_modules directory.  It is called by Walk when it lands on a
// directory literally named "node_modules"; the caller is responsible for
// not recursing further into the same tree.
//
// The traversal handles three layouts:
//
//	node_modules/<pkg>/...
//	node_modules/@scope/<pkg>/...
//	node_modules/<pkg>/node_modules/<dup>/...  (npm de-duplication leftover)
func walkNodeModules(dir, root string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scan/node: read %q: %w", dir, err)
	}

	var out []File
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			// node_modules/.bin, .cache, .package-lock.json …
			continue
		}
		pkgDir := filepath.Join(dir, name)
		if strings.HasPrefix(name, "@") {
			scoped, err := os.ReadDir(pkgDir)
			if err != nil {
				continue
			}
			for _, s := range scoped {
				if !s.IsDir() {
					continue
				}
				sub := filepath.Join(pkgDir, s.Name())
				out = append(out, extractNodePackage(sub, root, name+"/"+s.Name())...)
				if nested, err := nestedNodeModules(sub, root); err == nil {
					out = append(out, nested...)
				}
			}
			continue
		}
		out = append(out, extractNodePackage(pkgDir, root, name)...)
		if nested, err := nestedNodeModules(pkgDir, root); err == nil {
			out = append(out, nested...)
		}
	}
	return out, nil
}

func nestedNodeModules(pkgDir, root string) ([]File, error) {
	nm := filepath.Join(pkgDir, "node_modules")
	info, err := os.Stat(nm)
	if err != nil || !info.IsDir() {
		return nil, err
	}
	return walkNodeModules(nm, root)
}

// extractNodePackage reads the three prose channels an npm consumer ships
// with a published package: README, CHANGELOG, and the description /
// keywords fields of package.json.  Other channels (TypeScript .d.ts JSDoc
// comments, embedded markdown elsewhere) are out of scope for v0.1.
func extractNodePackage(pkgDir, root, nameHint string) []File {
	label := nameHint
	if pj, err := readPackageJSON(filepath.Join(pkgDir, "package.json")); err == nil {
		if pj.Name != "" {
			label = pj.Name
		}
		if pj.Version != "" {
			label = label + "@" + pj.Version
		}
	}

	var out []File

	readmeNames := []string{
		"README.md", "README.markdown", "README.MD",
		"Readme.md", "readme.md", "README", "readme",
		"README.rst", "README.txt",
	}
	for _, n := range readmeNames {
		if f, err := loadProseFile(filepath.Join(pkgDir, n), root, label, ecosystemNPM, "readme"); err == nil && f != nil {
			out = append(out, *f)
			break
		}
	}

	changelogNames := []string{
		"CHANGELOG.md", "CHANGES.md", "HISTORY.md",
		"CHANGELOG", "CHANGELOG.markdown", "CHANGELOG.txt",
	}
	for _, n := range changelogNames {
		if f, err := loadProseFile(filepath.Join(pkgDir, n), root, label, ecosystemNPM, "changelog"); err == nil && f != nil {
			out = append(out, *f)
			break
		}
	}

	if meta := loadPackageJSONProse(filepath.Join(pkgDir, "package.json"), root, label); meta != nil {
		out = append(out, *meta)
	}
	return out
}

// packageJSON is the minimum slice of package.json that prose-scanning
// cares about.  Scripts and dependencies are deliberately omitted — they
// belong to the executable surface area Snyk and friends already cover.
type packageJSON struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
}

func readPackageJSON(path string) (*packageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pj packageJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil, err
	}
	return &pj, nil
}

// loadPackageJSONProse renders the prose-bearing manifest fields
// (description, keywords) as a line-oriented synthetic File so a finding can
// report file:line in navigable terms.
//
// Each emitted prose line is anchored at its REAL package.json source line.
// The previous version emitted the description on synthetic line 1 and each
// keyword on lines 2, 3, … regardless of where those fields actually sat in
// the manifest, so a payload in the description was always reported at
// package.json:1 (and keywords at 2+) — an unnavigable location the developer
// could not open, undercutting the file:line value prop that the Python
// METADATA / docstring and Go doc-comment channels already honour. Anchoring
// to the field's real source line makes the npm channel consistent with them.
func loadPackageJSONProse(path, root, label string) *File {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pj packageJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil
	}
	if pj.Description == "" && len(pj.Keywords) == 0 {
		return nil
	}

	src := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	// place maps a 0-based source-line index to the synthetic prose emitted for
	// that line. Two prose channels that fall on the same physical line (e.g. a
	// compact single-line manifest) are joined rather than dropped, so no prose
	// channel — and therefore no finding — is lost relative to the old
	// always-emit behaviour.
	place := make(map[int]string)
	maxIdx := -1
	put := func(li int, s string) {
		if li < 0 {
			li = 0 // never drop a prose channel: fall back to the first line
		}
		if prev, ok := place[li]; ok {
			place[li] = prev + " " + s
		} else {
			place[li] = s
		}
		if li > maxIdx {
			maxIdx = li
		}
	}

	if pj.Description != "" {
		put(jsonKeyLine(src, "description", 0), "description: "+pj.Description)
	}
	// Anchor keyword search to the "keywords" array region so a keyword whose
	// text coincides with an earlier JSON key name is not mis-mapped, and scan
	// forward so repeated identical keywords map to successive occurrences.
	// The cursor carries a (line, byteOffset) position: after a match the next
	// search resumes strictly past that occurrence's bytes, so a second
	// identical keyword on a LATER physical line maps to its own real source
	// line (instead of re-matching and joining onto the first occurrence's
	// line), while a second identical keyword on the SAME physical line still
	// joins (no later match on that line -> it maps to that line).
	kwStart := jsonKeyLine(src, "keywords", 0)
	if kwStart < 0 {
		kwStart = 0
	}
	cursor := kwCursor{line: kwStart}
	for _, kw := range pj.Keywords {
		li, next := keywordLine(src, kw, cursor)
		if li < 0 {
			li = kwStart
		} else {
			cursor = next
		}
		put(li, "keyword: "+kw)
	}
	if maxIdx < 0 {
		return nil
	}

	lines := make([]string, maxIdx+1)
	for i := range lines {
		if s, ok := place[i]; ok {
			lines[i] = s
		}
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return &File{
		Path:        path,
		DisplayPath: filepath.ToSlash(rel),
		Package:     label,
		Ecosystem:   ecosystemNPM,
		Kind:        "metadata",
		Content:     strings.Join(lines, "\n") + "\n",
		Lines:       lines,
	}
}

// jsonKeyLine returns the 0-based source-line index of the first line at or
// after `from` that declares the JSON object key `key` (the quoted key
// followed by a ':' on the same line), or -1 when not found.
func jsonKeyLine(src []string, key string, from int) int {
	needle := `"` + key + `"`
	for i := from; i < len(src); i++ {
		idx := strings.Index(src[i], needle)
		if idx < 0 {
			continue
		}
		if strings.Contains(src[i][idx+len(needle):], ":") {
			return i
		}
	}
	return -1
}

// kwCursor is a (line, byteOffset) search position. After keywordLine matches
// an occurrence it returns a cursor positioned just past that occurrence, so a
// subsequent search resumes strictly after it instead of re-matching the same
// occurrence — the fix for repeated identical keywords on different physical
// lines of a multi-line "keywords" array.
type kwCursor struct {
	line int
	off  int // byte offset within src[line] to start searching from
}

// keywordLine returns the 0-based source-line index of the first line at or
// after c.line that contains the quoted keyword literal. On the starting
// line the search resumes strictly after c.off (past the previous match's
// bytes); on later lines the whole line is searched. It also returns a cursor
// positioned just past the matched occurrence so a subsequent call does not
// re-match it. Returns -1, kwCursor{} when not found.
//
// Resuming after the matched byte offset is what makes a second identical
// keyword on a LATER physical line map to its own real source line: once the
// starting line has no further match the search advances. A second identical
// keyword on the SAME physical line is still found there (a later match on
// that line) and so joins onto it, preserving the compact single-line
// duplicate behaviour. Distinct keywords are unaffected.
func keywordLine(src []string, kw string, c kwCursor) (int, kwCursor) {
	needle := `"` + kw + `"`
	for i := c.line; i < len(src); i++ {
		start := 0
		if i == c.line {
			start = c.off
		}
		line := src[i]
		if start > len(line) {
			continue
		}
		idx := strings.Index(line[start:], needle)
		if idx < 0 {
			continue
		}
		abs := start + idx
		return i, kwCursor{line: i, off: abs + len(needle)}
	}
	return -1, kwCursor{}
}

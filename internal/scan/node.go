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

// loadPackageJSONProse renders the prose-bearing manifest fields as a
// line-oriented synthetic File so a finding can report file:line in terms
// the developer can navigate ("line 2 of package.json description").
func loadPackageJSONProse(path, root, label string) *File {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	pj, err := readPackageJSON(path)
	if err != nil {
		return nil
	}
	var (
		b     strings.Builder
		lines []string
	)
	add := func(s string) {
		b.WriteString(s)
		b.WriteString("\n")
		lines = append(lines, s)
	}
	if pj.Description != "" {
		add("description: " + pj.Description)
	}
	for _, kw := range pj.Keywords {
		add("keyword: " + kw)
	}
	if len(lines) == 0 {
		return nil
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
		Content:     b.String(),
		Lines:       lines,
	}
}

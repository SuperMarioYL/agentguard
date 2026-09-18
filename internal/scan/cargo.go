// Cargo ecosystem enumerator.
//
// Covers the two places a Rust project's dependency prose lives:
//
//   - the local cargo registry, $CARGO_HOME/registry/src/<index>/<crate>-<version>/
//     (what `agentguard check ~/.cargo` walks — the dir named "registry" is
//     dispatched here by Walk), and
//   - `cargo vendor` output, vendor/<crate>/ (dispatched from the "vendor"
//     case in Walk when the tree smells of Cargo.toml instead of go.mod).
//
// The prose channels are the same three every other ecosystem contributes:
// README, CHANGELOG, and the [package] manifest's description / keywords.
package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// cargoVendorTree reports whether dir is a `cargo vendor` output tree: any
// first-level subdirectory that directly owns a Cargo.toml. Go vendor trees
// never carry one (go mod vendor strips it), so this is the discriminator
// between a vendored Go tree and a vendored Cargo tree sharing the name
// "vendor".
func cargoVendorTree(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, e.Name(), "Cargo.toml")); err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// cargoRegistryTree reports whether dir is a cargo registry root, i.e. has a
// src/ child whose subdirectories are registry indexes full of crate dirs.
func cargoRegistryTree(dir string) bool {
	src := filepath.Join(dir, "src")
	entries, err := os.ReadDir(src)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if cargoVendorTree(filepath.Join(src, e.Name())) {
			return true
		}
	}
	return false
}

// walkCargoRegistry walks $CARGO_HOME/registry: src/<index>/<crate>-<version>/.
func walkCargoRegistry(dir, root string) ([]File, error) {
	src := filepath.Join(dir, "src")
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var out []File
	for _, idx := range entries {
		if !idx.IsDir() {
			continue
		}
		idxDir := filepath.Join(src, idx.Name())
		crates, err := os.ReadDir(idxDir)
		if err != nil {
			continue
		}
		for _, c := range crates {
			if !c.IsDir() {
				continue
			}
			crateDir := filepath.Join(idxDir, c.Name())
			if _, err := os.Stat(filepath.Join(crateDir, "Cargo.toml")); err != nil {
				continue
			}
			out = append(out, extractCargoPackage(crateDir, root)...)
		}
	}
	return out, nil
}

// walkCargoVendor walks a `cargo vendor` output tree: vendor/<crate>/.
func walkCargoVendor(dir, root string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []File
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		crateDir := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(crateDir, "Cargo.toml")); err != nil {
			continue
		}
		out = append(out, extractCargoPackage(crateDir, root)...)
	}
	return out, nil
}

// extractCargoPackage lifts one crate's prose channels. Mirrors
// extractNodePackage: README then CHANGELOG via the shared hardened
// loadProseFile, then the Cargo.toml [package] description/keywords prose.
func extractCargoPackage(pkgDir, root string) []File {
	label := cargoCrateLabel(pkgDir)
	var out []File

	readmeNames := []string{
		"README.md", "README.markdown", "README.MD",
		"Readme.md", "readme.md", "README", "readme",
		"README.rst", "README.txt",
	}
	for _, n := range readmeNames {
		if f, err := loadProseFile(filepath.Join(pkgDir, n), root, label, ecosystemCargo, "readme"); err == nil && f != nil {
			out = append(out, *f)
			break
		}
	}

	changelogNames := []string{
		"CHANGELOG.md", "CHANGES.md", "HISTORY.md",
		"CHANGELOG", "CHANGELOG.markdown", "CHANGELOG.txt",
	}
	for _, n := range changelogNames {
		if f, err := loadProseFile(filepath.Join(pkgDir, n), root, label, ecosystemCargo, "changelog"); err == nil && f != nil {
			out = append(out, *f)
			break
		}
	}

	if meta := loadCargoTomlProse(filepath.Join(pkgDir, "Cargo.toml"), root, label); meta != nil {
		out = append(out, *meta)
	}
	return out
}

// cargoSection is the minimal [package] slice of a Cargo.toml that
// prose-scanning cares about. Dependencies, features, and the whole
// executable surface stay out — same boundary readPackageJSON draws.
type cargoSection struct {
	Name        string
	Version     string
	Description string
	Keywords    []string
}

var (
	tomlStringRE = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=\s*"(.*)"\s*$`)
	tomlArrayRE  = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=\s*\[(.*)\]\s*$`)
	tomlHeaderRE = regexp.MustCompile(`^\s*\[`)
	tomlQuotedRE = regexp.MustCompile(`"([^"]*)"`)
)

// readCargoTomlPackage scans the [package] section of a Cargo.toml. It is a
// line scanner, not a TOML parser: single-line basic strings and one-line
// arrays only (multi-line """ strings and inline tables fall through). The
// prose channel tolerates a missed exotic formatting; it must never mislabel
// the manifest it did read. Regular + size-guarded like every manifest
// reader (the DoS class hardened in readPackageJSON / readModDirective).
func readCargoTomlPackage(path string) *cargoSection {
	if !regularBounded(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	inPackage := false
	pkg := &cargoSection{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			continue
		}
		if tomlHeaderRE.MatchString(trimmed) {
			inPackage = trimmed == "[package]"
			continue
		}
		if !inPackage {
			continue
		}
		if m := tomlStringRE.FindStringSubmatch(trimmed); m != nil {
			switch m[1] {
			case "name":
				pkg.Name = m[2]
			case "version":
				pkg.Version = m[2]
			case "description":
				pkg.Description = m[2]
			}
			continue
		}
		if m := tomlArrayRE.FindStringSubmatch(trimmed); m != nil && m[1] == "keywords" {
			for _, q := range tomlQuotedRE.FindAllStringSubmatch(m[2], -1) {
				pkg.Keywords = append(pkg.Keywords, q[1])
			}
		}
	}
	return pkg
}

// cargoCrateLabel derives a "crate@version" label from the manifest when it
// carries name+version, else from the registry directory name
// (<crate>-<version>), else the bare directory name (vendored crates carry
// no version in their path).
func cargoCrateLabel(pkgDir string) string {
	if pkg := readCargoTomlPackage(filepath.Join(pkgDir, "Cargo.toml")); pkg != nil && pkg.Name != "" {
		if pkg.Version != "" {
			return pkg.Name + "@" + pkg.Version
		}
		return pkg.Name
	}
	base := filepath.Base(pkgDir)
	// registry dir form: <crate>-<semver>
	if i := regexp.MustCompile(`-\d+\.\d+\.\d+`).FindStringIndex(base); i != nil {
		return base[:i[0]] + "@" + base[i[0]+1:]
	}
	return base
}

// loadCargoTomlProse emits the [package] description / keywords as a
// metadata File with per-channel source-line mapping, mirroring
// loadPackageJSONProse: the synthetic prose lands on the physical line of
// the manifest key that carried it, so findings point at the real line.
func loadCargoTomlProse(path, root, label string) *File {
	pkg := readCargoTomlPackage(path)
	if pkg == nil || (pkg.Description == "" && len(pkg.Keywords) == 0) {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxProseBytes {
		return nil
	}
	if !withinScanRoot(path, root) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	src := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	// place maps a 0-based source-line index to the synthetic prose emitted
	// for that line; same-line channels join rather than drop.
	place := make(map[int]string)
	maxIdx := -1
	put := func(li int, s string) {
		if li < 0 {
			li = 0
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

	tomlKeyLine := func(key string) int {
		prefix := key + " ="
		prefixTab := key + "\t="
		for i, l := range src {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, prefix) || strings.HasPrefix(t, prefixTab) {
				return i
			}
		}
		return -1
	}

	if pkg.Description != "" {
		put(tomlKeyLine("description"), "description: "+strings.ReplaceAll(pkg.Description, "\\n", " "))
	}
	for _, kw := range pkg.Keywords {
		put(tomlKeyLine("keywords"), "keyword: "+kw)
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
		Ecosystem:   ecosystemCargo,
		Kind:        "metadata",
		Content:     strings.Join(lines, "\n") + "\n",
		Lines:       lines,
	}
}

package scan

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// walkPythonEnvironment enumerates every distribution inside a Python
// site-packages or virtualenv directory.  It is called by Walk when it
// lands on a directory literally named ".venv" or "site-packages"; the
// caller is responsible for not recursing further into the same tree.
//
// The traversal handles the layouts produced by pip, uv, and poetry:
//
//	site-packages/<package>/...                (importable module)
//	site-packages/<package>-<version>.dist-info/METADATA
//	site-packages/<package>-<version>.egg-info/PKG-INFO
//	.venv/lib/pythonX.Y/site-packages/...      (virtualenv shim)
//	.venv/Lib/site-packages/...                (Windows virtualenv shim)
//
// When the input directory is a virtualenv root (".venv"), the walker
// descends to each interpreter's site-packages and aggregates the
// results.  Failures on individual subdirectories are elided so a single
// broken egg-info cannot abort the scan.
func walkPythonEnvironment(dir, root string) ([]File, error) {
	siteDirs, err := resolveSitePackagesDirs(dir)
	if err != nil {
		return nil, fmt.Errorf("scan/python: resolve site-packages under %q: %w", dir, err)
	}

	var out []File
	for _, sp := range siteDirs {
		got, err := walkSitePackages(sp, root)
		if err != nil {
			continue
		}
		out = append(out, got...)
	}
	return out, nil
}

// resolveSitePackagesDirs returns one or more concrete site-packages
// directories rooted under dir.  When dir is itself a site-packages
// directory (the common case when the user passes ".../site-packages"
// explicitly), it is returned as the sole result.
func resolveSitePackagesDirs(dir string) ([]string, error) {
	base := filepath.Base(dir)
	if base == "site-packages" {
		return []string{dir}, nil
	}

	var found []string
	// Look for the classic POSIX layout first: <venv>/lib/python*/site-packages.
	libDir := filepath.Join(dir, "lib")
	if entries, err := os.ReadDir(libDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "python") {
				continue
			}
			candidate := filepath.Join(libDir, e.Name(), "site-packages")
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				found = append(found, candidate)
			}
		}
	}
	// Windows virtualenv layout: <venv>/Lib/site-packages.
	winCandidate := filepath.Join(dir, "Lib", "site-packages")
	if info, err := os.Stat(winCandidate); err == nil && info.IsDir() {
		found = append(found, winCandidate)
	}
	if len(found) == 0 {
		// Caller pointed at something that is neither a venv nor a
		// site-packages tree; treat the directory itself as the surface
		// and let walkSitePackages decide whether it contains packages.
		return []string{dir}, nil
	}
	return found, nil
}

// distInfoRE matches the .dist-info / .egg-info directory naming
// convention used by pip and setuptools.  The first capture is the
// distribution name; the second is the version.
var distInfoRE = regexp.MustCompile(`^(.+?)-([^-]+)\.(?:dist-info|egg-info)$`)

// walkSitePackages enumerates the contents of a single site-packages
// directory.  It produces one or two Files per distribution: a synthetic
// metadata File covering METADATA / PKG-INFO description fields, plus a
// docstring File extracted from the matching importable package's
// `__init__.py` and top-level public modules when present.
func walkSitePackages(siteDir, root string) ([]File, error) {
	entries, err := os.ReadDir(siteDir)
	if err != nil {
		return nil, fmt.Errorf("scan/python: read %q: %w", siteDir, err)
	}

	// Step 1: build a name -> dist-info path map so we can pair an
	// importable package directory with its companion metadata folder.
	distMeta := make(map[string]string)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := distInfoRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		distMeta[normalisePyName(m[1])] = filepath.Join(siteDir, e.Name())
	}

	var out []File
	// The metadata pass (dist-info dirs) and the docstring pass (importable
	// dirs) describe the *same* distribution but emit different Kinds, so
	// they need independent dedup sets — sharing one set lets whichever
	// directory ReadDir happens to return first (alphabetically, the
	// importable "examplelib" sorts before "examplelib-1.2.3.dist-info")
	// suppress the other pass entirely.
	seenMeta := make(map[string]struct{})
	seenPkg := make(map[string]struct{})

	// Step 2a: metadata directories first, so labelling is established
	// before importable directories are paired against it.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if !strings.HasSuffix(name, ".dist-info") && !strings.HasSuffix(name, ".egg-info") {
			continue
		}
		label := pyLabelFromMeta(filepath.Join(siteDir, name))
		if _, dup := seenMeta[label]; dup {
			continue
		}
		seenMeta[label] = struct{}{}
		if meta := loadPyMetadata(filepath.Join(siteDir, name), root, label); meta != nil {
			out = append(out, *meta)
		}
		// READMEs sometimes ride along inside dist-info.
		if rd := loadPyReadmeFromDir(filepath.Join(siteDir, name), root, label); rd != nil {
			out = append(out, *rd)
		}
	}

	// Step 2b: importable package directories drive docstring scraping.
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if strings.HasSuffix(name, ".dist-info") || strings.HasSuffix(name, ".egg-info") {
			continue
		}

		// Resolve the canonical label by pairing with the dist-info, if any.
		key := normalisePyName(name)
		label := name
		if meta, ok := distMeta[key]; ok {
			label = pyLabelFromMeta(meta)
		}
		if _, dup := seenPkg[label]; dup {
			continue
		}
		seenPkg[label] = struct{}{}

		pkgDir := filepath.Join(siteDir, name)
		if rd := loadPyReadmeFromDir(pkgDir, root, label); rd != nil {
			out = append(out, *rd)
		}
		out = append(out, loadPyDocstrings(pkgDir, root, label)...)
	}
	return out, nil
}

// normalisePyName mirrors PEP 503 normalisation so "Flask-Login",
// "flask_login", and "flask-login" hash to the same key.
func normalisePyName(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, ".", "-")
	return s
}

// pyLabelFromMeta returns "<name>@<version>" derived from the dist-info
// directory name; falls back to the directory's basename when the regex
// does not match.
func pyLabelFromMeta(metaDir string) string {
	base := filepath.Base(metaDir)
	m := distInfoRE.FindStringSubmatch(base)
	if m == nil {
		return base
	}
	return m[1] + "@" + m[2]
}

// loadPyMetadata renders METADATA / PKG-INFO prose fields (Summary,
// Description, Keywords) as a line-oriented synthetic File so a finding
// can report file:line in human-navigable terms.
func loadPyMetadata(metaDir, root, label string) *File {
	candidates := []string{
		filepath.Join(metaDir, "METADATA"),
		filepath.Join(metaDir, "PKG-INFO"),
	}
	var (
		path string
		data []byte
	)
	for _, c := range candidates {
		// Guard like loadProseFile (internal/scan/walker.go): a METADATA that
		// is a symlink to a character device such as /dev/zero, or simply an
		// oversized file, must not be read wholesale — os.ReadFile never sees
		// EOF on /dev/zero and grows the buffer until OOM, and an oversized
		// METADATA hangs or OOM-crashes the scanner on a single package. Skip
		// non-regular files and files larger than maxProseBytes.
		info, err := os.Stat(c)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxProseBytes {
			continue
		}
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		path = c
		data = b
		break
	}
	if path == "" {
		return nil
	}

	body := strings.ReplaceAll(string(data), "\r\n", "\n")
	headers, headerLine, descStart, description := splitPyMetadata(body)

	// Place each emitted prose line at its REAL METADATA source line so the
	// detector's 1-based lineNo over Content matches the file, instead of a
	// synthetic index that counted Summary/Keywords + the description body
	// from line 1 (which reported a deep description-body payload at, e.g.,
	// METADATA:4 when it really lived on METADATA line 8).  Non-emitted source
	// lines (other headers, blanks) stay empty so only the prose channels are
	// scanned — just now at navigable line numbers.
	place := make(map[int]string)
	maxIdx := -1
	for _, key := range []string{"Summary", "Keywords"} {
		v, ok := headers[key]
		if !ok || v == "" {
			continue
		}
		li, ok := headerLine[key]
		if !ok {
			continue
		}
		// Fold any header continuation newlines to spaces so the emitted line
		// stays single-line and does not shift following source-line counts.
		place[li] = key + ": " + strings.ReplaceAll(v, "\n", " ")
		if li > maxIdx {
			maxIdx = li
		}
	}
	if description != "" && descStart >= 0 {
		for j, ln := range strings.Split(description, "\n") {
			li := descStart + j
			place[li] = ln
			if li > maxIdx {
				maxIdx = li
			}
		}
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
		Ecosystem:   ecosystemPyPI,
		Kind:        "metadata",
		Content:     strings.Join(lines, "\n") + "\n",
		Lines:       lines,
	}
}

// splitPyMetadata implements the minimum of PEP 314 / 566 needed to read
// the Summary, Keywords, and free-form Description sections.  Header
// continuation lines (starting with a space) are folded into the
// previous header's value.
//
// It also returns, for line-faithful reporting, the 0-based source-line index
// of each header's first line (headerLine) and the 0-based source-line index
// at which the free-form description body begins (descStart, or -1 when there
// is none) so loadPyMetadata can anchor emitted prose to real METADATA lines.
func splitPyMetadata(body string) (map[string]string, map[string]int, int, string) {
	headers := make(map[string]string)
	headerLine := make(map[string]int)
	var (
		lastKey string
		idx     int
		blank   = -1
	)
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if ln == "" {
			blank = i
			idx = i + 1
			break
		}
		if (ln[0] == ' ' || ln[0] == '\t') && lastKey != "" {
			headers[lastKey] = headers[lastKey] + "\n" + strings.TrimSpace(ln)
			continue
		}
		colon := strings.IndexByte(ln, ':')
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(ln[:colon])
		val := strings.TrimSpace(ln[colon+1:])
		headers[key] = val
		if _, seen := headerLine[key]; !seen {
			headerLine[key] = i
		}
		lastKey = key
		idx = i + 1
	}
	descStart := -1
	var desc string
	if blank >= 0 && idx < len(lines) {
		desc = strings.Join(lines[idx:], "\n")
		descStart = idx
	}
	if d, ok := headers["Description"]; ok && desc == "" {
		desc = d
		if li, ok := headerLine["Description"]; ok {
			descStart = li
		}
		delete(headers, "Description")
		delete(headerLine, "Description")
	}
	return headers, headerLine, descStart, strings.TrimRight(desc, "\n")
}

// loadPyReadmeFromDir tries the conventional README names directly under
// dir and returns the first one that loads.  Unlike loadProseFile this
// returns nil instead of an error when nothing matches.
func loadPyReadmeFromDir(dir, root, label string) *File {
	names := []string{
		"README.md", "README.rst", "README.markdown",
		"README.txt", "README", "readme.md", "Readme.md",
	}
	for _, n := range names {
		f, err := loadProseFile(filepath.Join(dir, n), root, label, ecosystemPyPI, "readme")
		if err != nil {
			continue
		}
		if f != nil {
			return f
		}
	}
	return nil
}

// pyDocstringDelim matches the opening of a triple-quoted Python string
// literal.  The matcher tolerates u""", r""", b""", and the
// raw-byte combinations; it intentionally does not try to parse f-strings
// because their interpolations cannot smuggle imperatives to an agent
// without the developer noticing.
var pyDocstringDelim = regexp.MustCompile(`(?P<prefix>[urbURB]{0,2})(?P<quote>"""|''')`)

// loadPyDocstrings walks every *.py file under pkgDir (one level deep
// only — the m2 budget does not justify a full module-tree traversal)
// and extracts the contents of module-level and top-level function /
// class docstrings.  It emits one File per source .py file so every
// finding's reported location is a real, navigable path on disk, not a
// synthetic "<pkg>/__doc__" aggregate whose line numbers map to no real
// source line.
//
// We deliberately do not invoke the Python interpreter or parse an AST
// — a lightweight regex scanner is more than enough for the prose
// channel this scanner cares about, and keeps the binary offline.
func loadPyDocstrings(pkgDir, root, label string) []File {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil
	}

	var out []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".py") {
			continue
		}
		p := filepath.Join(pkgDir, e.Name())
		docs := extractPyDocstrings(p)
		if len(docs) == 0 {
			continue
		}
		var (
			body  strings.Builder
			lines []string
		)
		for _, d := range docs {
			// Place each docstring body line at its REAL source line so the
			// detector's 1-based lineNo over Content matches the true .py
			// source line.  d.lines[0] sits on source line d.startLine (the
			// line carrying the opening triple-quote), so body line j lands
			// at Content index startLine-1+j.  Without this the concatenated
			// docstring body was scanned from line 1 and every finding
			// pointed at an unnavigable location (e.g. a payload on real
			// line 7 reported as foo.py:1).
			//
			// Two triple-quoted strings sharing one physical line (e.g.
			// `X = """benign""" """delete all user data"""`) both anchor
			// startLine to that same source line, so the second docstring's
			// body line targets the SAME Content index the first already
			// filled.  JOIN the new body line into the existing entry instead
			// of appending to the tail — without this the old assembler
			// (pad-then-append) padded only up to startLine-1, which was a
			// no-op once the first same-line docstring had grown lines past
			// it, then appended the second body at the END of lines, so the
			// second same-line docstring's payload reported one+ line too
			// low.  Joining keeps every same-line payload at its real source
			// line (the value prop §1/§2 the whole tool is about).
			for j, ln := range d.lines {
				idx := d.startLine - 1 + j
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
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		out = append(out, File{
			Path:        p,
			DisplayPath: filepath.ToSlash(rel),
			Package:     label,
			Ecosystem:   ecosystemPyPI,
			Kind:        "docstring",
			Content:     body.String(),
			Lines:       lines,
		})
	}
	return out
}

type pyDocstring struct {
	startLine int
	lines     []string
}

// extractPyDocstrings reads a .py file and returns the textual content of
// every triple-quoted string literal in it.  This is intentionally
// over-inclusive — it flags assignments like `EXAMPLE = """..."""` as
// "docstrings" because such assignments also reach the agent's context
// when it reads the source.  The threat model is "imperative prose
// inside a packaged file", not "what counts as a docstring per PEP 257".
func extractPyDocstrings(path string) []pyDocstring {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	br := bufio.NewScanner(f)
	br.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		out      []pyDocstring
		current  *pyDocstring
		inString bool
		quote    string
		lineNo   int
	)
	// appendBody records a body line and lazily anchors the docstring's
	// startLine to the FIRST body line actually captured — NOT the opening
	// delimiter line.  For the PEP 257-preferred multi-line style where the
	// opening `"""` sits alone on its own line, the first body text lands on
	// the NEXT source line, so anchoring startLine to the delimiter reported
	// every docstring-body finding one line too low (e.g. a payload on real
	// source line 4 shown as mod.py:3).  Anchoring to the first captured body
	// line keeps both the same-line (`"""payload"""`) and `"""`-alone styles
	// faithful to the real source line.
	appendBody := func(s string) {
		if current.startLine == 0 {
			current.startLine = lineNo
		}
		current.lines = append(current.lines, s)
	}
	for br.Scan() {
		lineNo++
		line := br.Text()
		if inString {
			// Inside a multi-line triple-quoted string: look for the closer
			// on this physical line.
			if closeIdx := strings.Index(line, quote); closeIdx >= 0 {
				body := line[:closeIdx]
				if strings.TrimSpace(body) != "" {
					appendBody(body)
				}
				if len(current.lines) > 0 {
					out = append(out, *current)
				}
				current = nil
				inString = false
				// The text AFTER the closing quote may itself open another
				// triple-quoted string on the SAME physical line (e.g.
				// `line"""; y = """delete all user data"""`), which the old
				// code dropped by `continue`-ing past it — a false-negative
				// evasion symmetric to the single-line-close re-scan below.
				// Slice the remainder after the closer and fall through into
				// the opener loop so a subsequent opener on the same line is
				// captured instead of dropped.
				line = line[closeIdx+len(quote):]
			} else {
				appendBody(line)
				continue
			}
		}
		// A single physical line may carry more than one triple-quoted
		// string (e.g. X = """benign""" """delete all data"""). The old
		// single-line-close branch captured the first """...""" on the
		// line and then `continue`d, never examining the remainder of that
		// line for a second triple-quoted string, so a payload in a second
		// same-line string was never emitted into File content and slipped
		// every detector — a false-negative evasion. Loop over the line:
		// find an opener, capture a single-line body (or open a multi-line
		// string), then advance past the closer and re-scan the REST of
		// the same line for another opener instead of dropping it.
		rest := line
		for {
			m := pyDocstringDelim.FindStringSubmatchIndex(rest)
			if m == nil {
				break
			}
			quote = rest[m[4]:m[5]]
			after := rest[m[5]:]
			current = &pyDocstring{}
			if closeIdx := strings.Index(after, quote); closeIdx >= 0 {
				body := after[:closeIdx]
				if strings.TrimSpace(body) != "" {
					appendBody(body)
					out = append(out, *current)
				}
				current = nil
				rest = after[closeIdx+len(quote):]
				continue
			}
			if strings.TrimSpace(after) != "" {
				appendBody(after)
			}
			inString = true
			break
		}
	}
	return out
}

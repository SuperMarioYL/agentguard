# Changelog

All notable changes are recorded here. Versions follow [Semantic Versioning](https://semver.org/).
Dates are ISO 8601 (`YYYY-MM-DD`).

## [Unreleased]

### Planned (future)

- Cargo and RubyGems ecosystem walkers.
- `action.yml` GitHub Action wrapper so `agentguard` runs natively in workflows.
- Per-project rule disable list at `.agentguard.yaml` (allowlists, severity overrides).
- Hosted team policy server (central corpus updates + per-org allowlists).
- SARIF → Jira pipe for security teams that triage outside GitHub Advanced Security.

## [0.7.0] — 2026-07-14

Correctness release. No new detector rules, ecosystems, or CLI surface — one
source-audit fix that restores the navigable `file:line` value prop on the last
prose channel that still reported a synthetic line number: the npm `package.json`
manifest.

### Fixed

- **`package.json` findings now report the real manifest source line, not a
  synthetic index** (`internal/scan/node.go`). The manifest reader emitted the
  `description` on line 1 and each keyword on lines 2, 3, … regardless of where
  those fields actually sat in the file, so an injection payload hidden in a
  `description` was always reported at `package.json:1` (and keyword payloads at
  `:2+`) by both the text and SARIF reporters — an unnavigable location the
  developer could not open. Each prose line is now anchored at its real source
  line (a `description` payload on physical line 6 is reported at line 6, not 1).
  Channels that share a physical line (compact single-line manifests) are joined
  rather than dropped, so no finding is lost. This brings the npm channel in line
  with the Python docstring/METADATA and Go doc-comment channels, whose
  real-source-line reporting was fixed in 0.5.0 and 0.6.0. Regression tests:
  `TestPackageJSONProseReportsRealSourceLine` and
  `TestPackageJSONProseNeverDropsChannel`.

## [0.6.0] — 2026-07-11

Correctness release. No new detector rules, ecosystems, or CLI surface — three
source-audit fixes: one silent false-negative on a scanned surface, and two that
restore the navigable `file:line` value prop in paths that reported the wrong line.

### Fixed

- **Vendored Go packages produced by `go mod vendor` are no longer silently
  scanned as zero files** (`internal/scan/gomod.go`). `go mod vendor` strips
  `go.mod` from every vendored package, so `findGoModuleRoots` found nothing and
  the walker fell back to scanning the bare `vendor/` directory — whose direct
  children are import-path segments, not prose — yielding **zero** files. A
  vendored dependency whose README carried a payload was silently missed
  (`exit 0`, "no findings") on a directory the README hero explicitly lists as
  scanned — the worst failure mode for a security tool. The scanner now recovers
  the real vendored package directories from `vendor/modules.txt` (the canonical
  list `go mod vendor` writes) and, when that is absent, enumerates every
  prose-bearing package subtree directly. Guarded by regression tests over a
  realistic go.mod-stripped vendor tree (with and without `modules.txt`).
- **Python multi-line docstrings whose opening `"""` sits alone on its line now
  report the real source line** (`internal/scan/python.go`). `extractPyDocstrings`
  anchored `startLine` to the delimiter line, but for the PEP 257-preferred style
  where `"""` sits alone the first body text lands on the *next* source line, so
  every such docstring-body finding was reported one line too low (a payload on
  real line 5 shown as `mod.py:4`). `startLine` is now captured lazily at the
  first body line actually appended, keeping both the same-line and `"""`-alone
  styles faithful. Guarded by a regression test.
- **Python `METADATA` / `PKG-INFO` description-body findings now report the real
  source line** (`internal/scan/python.go`). `loadPyMetadata` built a synthetic
  `Content` of the Summary/Keywords headers followed by the description body and
  counted `lineNo` from 1 over it, so a payload deep in the description was
  reported at a synthetic index (a payload on real line 9 shown as `METADATA:4`).
  The Summary/Keywords headers and every description line are now placed at their
  real `METADATA` source line (recovered via `splitPyMetadata`'s blank-line
  separator), mirroring the v0.5.0 docstring fix for the metadata channel.
  Guarded by a regression test.

## [0.5.0] — 2026-07-04

Correctness release. No new detector rules, ecosystems, or CLI surface — two
source-audit fixes that both restore the navigable `file:line` value prop in the
paths that previously reported the wrong line.

### Fixed

- **An over-long (>1 MiB) prose line no longer shifts the line number of every
  following finding** (`internal/detect/patterns.go`). `splitLongTolerant`
  emitted a rune-safe truncated prefix for a line larger than the scanner buffer
  but advanced only `len(data)`, so `bufio` re-buffered the physical line's tail
  and emitted it as a *second* token — `ScanAll`'s `lineNo` over-counted and
  every finding after such a line was reported one (or more) lines too high (a
  payload on real line 2 reported at line 3). The split function is now a stateful
  closure (`newSplitLongTolerant`) that, after emitting the prefix, silently
  consumes the rest of that physical line up to and including the next `\n`, so a
  single over-long line counts as exactly one logical line. Guarded by a
  regression test asserting a payload immediately after a >1 MiB line reports
  line 2.
- **Python docstring and Go package-comment findings now report the real source
  line** (`internal/scan/python.go`, `internal/scan/gomod.go`). `loadPyDocstrings`
  and `loadGoPackageDocs` built each `File`'s `Content` from only the concatenated
  docstring / comment body and discarded the captured `startLine`, so `ScanAll`
  counted `lineNo` from 1 over the stripped body — a payload on real line 7 was
  reported as `foo.py:1`. v0.3.0 fixed the docstring *path* but left the *line*
  wrong; the body is now padded with empty lines up to the real start line (and
  `extractGoPackageComment` reports the comment block's start line) so each finding
  points at its true `.py`/`.go` source line. Guarded by two regression tests
  through the real `scan.Walk` → `detect.ScanAll` path.

### Changed

- project `VERSION` → `0.5.0`.

## [0.4.0] — 2026-07-01

Correctness release. No new detector rules, ecosystems, or CLI surface — one
source-audit fix that hardens the incremental-CI rolling-baseline path the
v0.3.0 release set out to make honest.

### Fixed

- **`--changed-only X --write-baseline X` no longer drops unchanged packages
  from the rolling baseline** (`internal/scan/walker.go`). `filterChanged` did
  `out := files[:0]`, compacting the kept (changed) files into the *same*
  backing array as the caller's slice. `runCheck` calls
  `scan.FilterChanged(files, X)` and then `scan.BaselineBytes(files)` on that
  very slice, so `BaselineBytes` read a slice the filter had overwritten in
  place: every package whose prose was *unchanged* (and therefore filtered out)
  was clobbered by the compaction and never written to the baseline. On the
  next run those packages were absent from the baseline, treated as new, and
  re-scanned — reintroducing the exact incremental-CI regression v0.3.0's
  baseline-decoupling fix removed. `filterChanged` now allocates a fresh result
  slice (`make([]File, 0, len(files))`) and never mutates the caller's backing
  array. Guarded by two regression tests: one asserts the post-`FilterChanged`
  slice is untouched and the baseline still covers every `DisplayPath`, and one
  runs the full `--changed-only X --write-baseline X` rolling pattern twice and
  asserts the second scan covers zero unchanged files.

### Changed

- project `VERSION` → `0.4.0`.

## [0.3.0] — 2026-06-28

Correctness release. No new detector rules, ecosystems, or CLI surface — four
source-audit fixes that make already-advertised behaviour honest and the
file:line value prop true for every channel.

### Fixed

- **`--ecosystem node` and `--ecosystem python` no longer silently suppress the
  scan** (`internal/scan/walker.go`). `--help` and both READMEs document
  `--ecosystem node | python | go`, but the internal enumerator constants are
  `npm` / `pypi` / `go` and `wants()` compared the user's token to the constant
  with a bare `strings.EqualFold` and no alias map, so `node` never matched
  `npm` and `python` never matched `pypi` — both quietly dropped the npm/python
  enumerators and the scan reported "no findings" (exit 0) even when real
  payloads were present. A security tool silently reporting "clean" on a
  documented flag is the worst failure mode. `wants()` now normalises aliases
  (`node`→`npm`, `python`→`pypi`, case- and whitespace-insensitive; `go`
  unchanged) before comparing.
- **`--changed-only X --write-baseline X` no longer collapses the baseline**
  (`cmd/agentguard/main.go`, `internal/scan/walker.go`). `Walk` applied
  `filterChanged` internally, so the `files` slice `runCheck` handed to
  `BaselineBytes` was already the narrowed (only-changed) set; the rolling
  baseline was rewritten from just the changed files, and on the next run every
  previously-unchanged package was absent from the baseline, treated as new,
  and re-scanned — silently defeating the incremental-CI feature. Baseline
  emission is now decoupled from the scan filter: `Walk` returns the full set,
  `runCheck` narrows with the newly exported `scan.FilterChanged` against the
  pre-existing baseline, then writes the baseline from the full set, then scans
  the narrowed set.
- **`--ecosystem` no longer leaks a generic-fallback finding**
  (`internal/scan/walker.go`). When no ecosystem enumerator produced files,
  `Walk` unconditionally fell back to `walkGenericPackage(root)` with no check
  on `opts.Ecosystems`, so `--ecosystem go` (or `node`/`python`) on a project
  whose root has its own README surfaced that README as a `generic`-ecosystem
  finding, violating the declared filter. The fallback is now gated on
  `len(opts.Ecosystems) == 0` — it still runs for bare fixtures and
  single-package dirs where the user did not restrict to an ecosystem.
- **Python docstring and Go package-doc findings report real source paths**
  (`internal/scan/python.go`, `internal/scan/gomod.go`). `loadPyDocstrings`
  built one composite `File` per package whose `DisplayPath` was
  `<pkg>/__doc__` — a path that does not exist on disk — so a finding was
  reported as `site-packages/<pkg>/__doc__:N`, a location the developer could
  not open or navigate to. It now emits one `File` per source `.py` file with
  the real relative path. `loadGoPackageDocs` gets the same treatment so
  multi-file Go modules do not attribute the 2nd+ file's package comment to the
  first `.go` path.

### Changed

- project `VERSION` → `0.3.0`.

## [0.2.0] — 2026-06-22

Credibility-restoring maintenance release. No new detector ecosystem; four
source-audit fixes that make already-advertised features honest and the corpus
claim true.

### Fixed

- **`--changed-only` now actually narrows the scan** (`internal/scan/walker.go`).
  Previously the flag was parsed and plumbed into `scan.Options.ChangedOnly` but
  the walker never read it, so the advertised incremental-CI mode was a silent
  no-op and every package was scanned unconditionally. `Walk` now loads a JSON
  baseline and drops every prose file whose `(display-path, content-hash)` pair
  is unchanged since the baseline run. A missing baseline is treated as a first
  run (full scan).
- **A single over-long prose line no longer aborts the whole scan**
  (`internal/detect/patterns.go`). `ScanAll` previously capped the line scanner
  at 1 MiB and returned `bufio.ErrTooLong` on a longer line, after which the CLI
  printed no report and discarded every finding already collected from other
  files. `ScanAll` now uses a tolerant `bufio.SplitFunc` that rune-safely
  truncates any over-long line and continues, preserving accumulated findings.
- **Excerpts truncate on a rune boundary** (`internal/detect/patterns.go`).
  `truncateExcerpt` byte-sliced the matched line, cutting mid-rune for non-ASCII
  content (the primary zh locale) and emitting invalid UTF-8 in the text report
  and the SARIF result message. It now counts runes and slices on a rune
  boundary, so multibyte excerpts stay valid UTF-8.

### Added

- **`--write-baseline <path>`** flag on `check` — writes a baseline JSON of every
  scanned prose file's content hash, for a later `--changed-only` run.
- **Corpus expanded from 12 to 30 rules** (`corpus/payloads.yaml`). New rules
  `AG013`–`AG030` cover dependency/typosquat injection, backdoor/reverse-shell,
  disabling security controls, secret relocation, crypto-wallet swap, silent
  unrelated-file edits, hidden Unicode (Trojan-Source) payloads, privilege
  escalation, cloud/database destruction, manufactured urgency, system-prompt
  extraction, jailbreak personas, auto-approve/skip-confirmation, malicious
  editor/browser extensions, network redirection (hosts/proxy/DNS), cryptominers,
  log/history tampering, and safety/content-policy override. This makes the
  long-standing "30-rule corpus" claim in the READMEs and architecture diagram
  true. The clean fixture still produces zero findings.

### Changed

- `corpus/payloads.yaml` version bumped to `0.2.0`; project `VERSION` → `0.2.0`.

## [0.1.0] — 2026-06-01

Initial public release. Covers the three milestones (m1–m3) in the README roadmap.

### Added

- **CLI surface** (`cmd/agentguard/main.go`)
  - `agentguard check [path]` — scan a directory and report findings.
  - `agentguard corpus` — print embedded corpus version, rule count, last-updated date.
  - Flags: `--format text|sarif`, `--severity low|medium|high`, `--changed-only <baseline.json>`,
    `--ecosystem node|python|go` (repeatable), `--output <path>`, `--no-color`, `--exit-on-finding`.
- **Walker** (`internal/scan/`)
  - `walker.go` — root traversal, ecosystem dispatch, per-file size cap (1 MiB), CRLF normalisation.
  - `node.go` — `node_modules/` enumeration with `@scope/` and dedup-nesting support; pulls
    `README*`, `CHANGELOG*`, `package.json` `description` + `keywords`.
  - `python.go` — `.venv/` / `venv/` / `site-packages/` enumeration; regex docstring extraction
    that does not require a Python runtime.
  - `gomod.go` — `vendor/` and `~/go/pkg/mod` cache enumeration; extracts `doc.go` and
    module-root `*.md`.
- **Detector** (`internal/detect/`)
  - `patterns.go` — YAML corpus loader, case-insensitive compiled regex pool, per-line
    `(file, line, rule)` dedup, configurable severity filter.
  - `heuristics.go` — `H001-proximity-imperative` (destructive verb × agent-address within 120
    chars) and `H002-conditional-agent-reader` (`if you are an AI, do X`).
- **Corpus** (`corpus/payloads.yaml`)
  - 12 hand-curated rules (`AG001`–`AG012`) covering: direct agent address,
    destructive imperatives, exfiltration imperatives, `ignore previous instructions`,
    suppress-from-user directives, and the conditional-agent-reader shape.
    (Expanded to 30 rules in v0.2.0.)
  - Embedded via `//go:embed` (`corpus/embed.go`) — no runtime file dependency.
- **Reporters** (`internal/report/`)
  - `text.go` — colour-aware grouped output with high/medium/low tallies.
  - `sarif.go` — SARIF 2.1.0 output via `github.com/owenrumney/go-sarif/v2`, ready for GitHub
    Advanced Security and the VS Code SARIF Viewer.
- **Test fixtures**
  - `testdata/jqwik_fixture/` — reproduces the public May 2026 jqwik payload as a single-package
    fixture. Must produce at least one high-severity finding.
  - `testdata/clean_fixture/` — benign README with words like `delete` in non-imperative
    contexts. Must produce zero findings of any severity.
- **Docs and packaging**
  - Bilingual README: `README.md` (Simplified Chinese, primary), `README.en.md` (English),
    `README.zh-CN.md` pointer.
  - Apache 2.0 license.
  - GitHub Actions workflow at `.github/workflows/ci.yml` running `go vet`, `go build`,
    `go test` on Ubuntu and macOS for Go 1.24.
  - `assets/demo.tape` — VHS script that renders the canonical jqwik demo as a 30-second cast.

### Threat-model notes

- The matching engine is intentionally regex + YAML corpus. No LLM-as-classifier; the binary
  is offline, reproducible, and runs in any CI image without API keys.
- Source files are never opened — the scanner walks only prose channels a coding agent
  ingests as context.

[Unreleased]: https://github.com/SuperMarioYL/agentguard/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/SuperMarioYL/agentguard/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/SuperMarioYL/agentguard/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/SuperMarioYL/agentguard/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/SuperMarioYL/agentguard/releases/tag/v0.1.0

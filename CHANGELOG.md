# Changelog

All notable changes are recorded here. Versions follow [Semantic Versioning](https://semver.org/).
Dates are ISO 8601 (`YYYY-MM-DD`).

## [Unreleased]

### Planned for v0.3.0

- Cargo and RubyGems ecosystem walkers.
- `action.yml` GitHub Action wrapper so `agentguard` runs natively in workflows.
- Per-project rule disable list at `.agentguard.yaml` (allowlists, severity overrides).

### Planned for v0.4.0

- Hosted team policy server (central corpus updates + per-org allowlists).
- SARIF → Jira pipe for security teams that triage outside GitHub Advanced Security.

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
  - MIT license.
  - GitHub Actions workflow at `.github/workflows/ci.yml` running `go vet`, `go build`,
    `go test` on Ubuntu and macOS for Go 1.24.
  - `assets/demo.tape` — VHS script that renders the canonical jqwik demo as a 30-second cast.

### Threat-model notes

- The matching engine is intentionally regex + YAML corpus. No LLM-as-classifier; the binary
  is offline, reproducible, and runs in any CI image without API keys.
- Source files are never opened — the scanner walks only prose channels a coding agent
  ingests as context.

[Unreleased]: https://github.com/SuperMarioYL/agentguard/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/SuperMarioYL/agentguard/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/SuperMarioYL/agentguard/releases/tag/v0.1.0

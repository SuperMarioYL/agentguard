# Changelog

All notable changes are recorded here. Versions follow [Semantic Versioning](https://semver.org/).
Dates are ISO 8601 (`YYYY-MM-DD`).

## [Unreleased]

### Planned for v0.2.0

- Cargo and RubyGems ecosystem walkers.
- `action.yml` GitHub Action wrapper so `agentguard` runs natively in workflows.
- Per-project rule disable list at `.agentguard.yaml` (allowlists, severity overrides).

### Planned for v0.3.0

- Hosted team policy server (central corpus updates + per-org allowlists).
- SARIF → Jira pipe for security teams that triage outside GitHub Advanced Security.

## [0.1.0] — 2026-06-01

Initial public release. Covers the three milestones in `mvp_plan.md` §5.

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
  - 30 hand-curated rules across `AG001` through `AG00x` covering: direct agent address,
    destructive imperatives, exfiltration imperatives, `ignore previous instructions`,
    suppress-from-user directives, and the conditional-agent-reader shape.
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
  - `BUILD_SETUP_NEXT_STEPS.md` — checklist for pushing the repo public.

### Threat-model notes

- The matching engine is intentionally regex + YAML corpus. No LLM-as-classifier; the binary
  is offline, reproducible, and runs in any CI image without API keys.
- Source files are never opened — the scanner walks only prose channels a coding agent
  ingests as context.

[Unreleased]: https://github.com/supermario-leo/agentguard/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/supermario-leo/agentguard/releases/tag/v0.1.0

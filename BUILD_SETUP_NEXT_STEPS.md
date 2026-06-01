# Next steps after this build

`agentguard` is scaffolded and committed locally. Before pushing it public, run through this short
checklist — most steps are once-only.

## 1. Push to GitHub

```bash
# from inside this directory
git remote add origin git@github.com:SuperMarioYL/agentguard.git
git push -u origin main
```

If the GitHub repo does not exist yet:

```bash
gh repo create SuperMarioYL/agentguard --public --source=. --remote=origin --push
```

## 2. Set the discoverability topics

The README positioning relies on visitors finding the repo through the topic facets:

```bash
gh repo edit \
  --add-topic coding-agent \
  --add-topic claude-code \
  --add-topic cursor \
  --add-topic agentic \
  --add-topic prompt-injection \
  --add-topic supply-chain \
  --add-topic dependency-scanner \
  --add-topic sarif \
  --add-topic mcp \
  --add-topic go
```

## 3. Render the demo asset

The `assets/demo.tape` script is written for [VHS](https://github.com/charmbracelet/vhs).
Render it before linking it from the README so the asciinema / GIF lands on first visit.

```bash
# install vhs once (macOS)
brew install vhs

# from repo root
vhs assets/demo.tape
# -> writes assets/demo.gif and assets/demo.cast
git add assets/demo.gif assets/demo.cast
git commit -m "docs: add rendered demo cast"
git push
```

If you prefer asciinema directly (no ttyrec recorder needed):

```bash
asciinema rec assets/demo.cast \
  -c "bash -c 'agentguard check ./testdata/jqwik_fixture; \
                agentguard check ./testdata/jqwik_fixture --format sarif | jq .runs[0].results | head -40'"
```

Then upload to <https://asciinema.org> and update the README placeholder ID in `README.md`
and `README.en.md`.

## 4. Tag the first release

```bash
git tag -a v0.1.0 -m "v0.1.0 — first public release"
git push origin v0.1.0
```

GitHub will pick up the tag and surface it under Releases. The README badge that currently shows
`release-WIP` can be swapped to a live shields.io release badge:

```markdown
<a href="https://github.com/SuperMarioYL/agentguard/releases/latest">
  <img alt="Latest release" src="https://img.shields.io/github/v/release/SuperMarioYL/agentguard">
</a>
```

## 5. Run the day-zero distribution checklist (from `go_to_market.md`)

Pull these from the `go_to_market.md` of the plan; this checklist is just the operational shape:

- [ ] **Hour 0–2**: confirm `README.md` (zh) renders correctly on GitHub; demo cast embeds.
- [ ] **Hour 2–3**: post the reply-of-record on the nesbitt.io HN thread.
- [ ] **Hour 24 (Tue 8:00 ET / 20:00 北京)**: `Show HN: agentguard – catch hidden agent-instructions
      in your npm/pip/go dependencies`.
- [ ] **Hour 24–28**: publish 掘金 long-form 「我给 Claude Code 加了个供应链扫描器」 with the demo cast.
- [ ] **Hour 28–30**: 6-tweet thread, tag @simonw / @swyx / @dotey individually.
- [ ] **Hour 30–32**: r/LocalLLaMA reply naming the tool.
- [ ] **Hour 32–36**: PRs to `awesome-claude-code`, `awesome-mcp`, `socket.dev` related-tools page.
- [ ] **Hour 36–48**: answer every HN comment, every GitHub issue, every Twitter reply personally.

## 6. CI verification

After the first push, confirm the **build + test** workflow on `.github/workflows/ci.yml` passes
on both `ubuntu-latest` and `macos-latest`. The smoke step on the jqwik fixture must exit 1; the
smoke step on the clean fixture must exit 0. If either flips, **stop and triage the corpus / walker
regression before announcing anywhere** — a false-positive on the clean fixture in week one will
burn the launch.

## 7. Kill criteria (from `mvp_plan.md` §8)

Set a calendar reminder for **2026-07-01** (T+30 days). If the repo has fewer than 50 stars **and**
zero organic issues (issues not opened by you or the five interviewees), trigger the kill criterion
and archive the repo — do not pour another month into m4 features hoping the distribution lands.

Additional architectural kill: if Anthropic, Cursor, or OpenAI ships native dependency-prose
filtering inside the agent runtime during this window, freeze m2/m3 and re-position the project
as an auditable reporter rather than a gatekeeper (the `gitleaks` ↔ GitHub secret-scanning
coexistence model).

## 8. What this build deliberately did NOT do

These are listed in `mvp_plan.md` §6 as out-of-scope for v0.1; do not let scope creep through PRs:

- Web UI / dashboard
- LLM-based classifier
- Real-time file-watch or IDE / MCP plugin
- Auto-fix or payload stripping
- Cargo / arbitrary Git-pulled dependency support (v0.2)
- Hosted scanning service / team policy server (v0.3)
- Multi-user accounts, SaaS billing, paid tier
- SBOM emission, license auditing, CVE correlation

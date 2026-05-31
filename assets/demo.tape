# agentguard — 30-second demo script for charmbracelet/vhs.
#
# Render with:   vhs assets/demo.tape
# Outputs:       assets/demo.gif   (animated)
#                assets/demo.cast  (asciinema cast — used by README embeds)
#
# Goal: show the jqwik fixture being caught, SARIF output piped through jq, and
# the --changed-only incremental mode against a baseline lockfile.  Total target
# duration is ~30 seconds so the cast embeds cleanly in a Show HN reply.

Output assets/demo.gif
Output assets/demo.cast

Set FontSize 16
Set Width 1280
Set Height 720
Set Theme "Catppuccin Mocha"
Set Padding 24
Set TypingSpeed 45ms
Set PlaybackSpeed 1.0

# ── Scene 1 — first scan catches the jqwik payload ─────────────────────────
Type "# 1) scan the jqwik fixture — should fire a HIGH finding and exit 1"
Enter
Sleep 600ms
Type "agentguard check ./testdata/jqwik_fixture"
Enter
Sleep 4s

# ── Scene 2 — SARIF output for CI upload ──────────────────────────────────
Type "# 2) same scan, --format sarif for GitHub code-scanning"
Enter
Sleep 600ms
Type "agentguard check ./testdata/jqwik_fixture --format sarif --output /tmp/out.sarif"
Enter
Sleep 2s
Type "jq '.runs[0].results[0] | {ruleId, level, locations}' /tmp/out.sarif"
Enter
Sleep 3s

# ── Scene 3 — clean fixture stays at zero findings (no FP) ────────────────
Type "# 3) clean fixture must produce zero findings"
Enter
Sleep 600ms
Type "agentguard check ./testdata/clean_fixture"
Enter
Sleep 2s

# ── Scene 4 — incremental --changed-only mode for CI ──────────────────────
Type "# 4) --changed-only skips packages whose hash matches a baseline"
Enter
Sleep 600ms
Type "agentguard check ./testdata/jqwik_fixture --changed-only baseline.json"
Enter
Sleep 3s

# ── Scene 5 — corpus introspection ────────────────────────────────────────
Type "# 5) which corpus version did we just run?"
Enter
Sleep 400ms
Type "agentguard corpus"
Enter
Sleep 2s

Sleep 1500ms

# agentguard — terminal demo script for charmbracelet/vhs.
#
# Render with:   vhs docs/demo.tape
# Output:        assets/demo.gif
#
# Happy path: scan the bundled jqwik fixture (HIGH finding, exit 1), pipe the
# SARIF output through jq for GitHub code-scanning, confirm the clean fixture
# stays at zero findings, then print the embedded corpus version.

Output assets/demo.gif

Set FontSize 16
Set Width 1280
Set Height 720
Set Theme "Catppuccin Mocha"
Set Padding 24
Set TypingSpeed 45ms
Set PlaybackSpeed 1.0

# ── Scene 1 — first scan catches the jqwik payload ─────────────────────────
Type "# 1) scan the jqwik fixture — fires HIGH findings and exits 1"
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

# ── Scene 4 — corpus introspection ────────────────────────────────────────
Type "# 4) which corpus version did we just run?"
Enter
Sleep 400ms
Type "agentguard corpus"
Enter
Sleep 2s

Sleep 1500ms

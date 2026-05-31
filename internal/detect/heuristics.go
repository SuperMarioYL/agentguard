package detect

import (
	"regexp"
	"strings"
)

// A heuristic is a synthetic rule expressed in code rather than YAML.
// They backstop the curated corpus on the compound shapes the YAML can't
// express cleanly — most importantly the "destructive verb within an
// agent-address window" jqwik shape, and the "if you are an AI, do X"
// conditional that has no payload of its own.
type heuristic struct {
	id       string
	title    string
	severity Severity
	match    func(line string) bool
}

func defaultHeuristics() []heuristic {
	return []heuristic{
		{
			id:       "H001-proximity-imperative",
			title:    "Destructive imperative within agent-address window",
			severity: SeverityHigh,
			match:    proximityImperative,
		},
		{
			id:       "H002-conditional-agent-reader",
			title:    "Conditional payload that only fires for an AI reader",
			severity: SeverityMedium,
			match:    conditionalAgentReader,
		},
	}
}

// agentRefRE matches the tokens a coding-agent payload uses to address
// its target reader.  Kept narrow on purpose — bare "you" alone explodes
// the false-positive surface on benign READMEs.
var agentRefRE = regexp.MustCompile(`(?i)\b(ai|llm|language model|coding agent|coding assistant|assistant|chatbot|claude|cursor|copilot|codex|agent)\b`)

// destructiveVerbRE captures the verbs that escalate an imperative from
// "advice to humans" to "footgun aimed at an agent".  The list is the
// intersection of the jqwik payload, the protestware writeup, and the
// AG002/AG003/AG006 corpus rules — kept duplicated here so the heuristic
// survives a corpus rewrite without a sync hazard.
var destructiveVerbRE = regexp.MustCompile(`(?i)\b(delete|remove|wipe|nuke|drop|destroy|erase|truncate|purge|exfiltrate|leak|send|upload|post|paste|format|reformat|shred|unlink|rm\s+-rf)\b`)

// conditionalAgentReaderRE matches the structural "if/when you are an
// AI" guard.  Used by the H002 heuristic, which additionally requires an
// imperative on the same line so pure tagging ("this README is for the
// AI reader too") does not trip it.
var conditionalAgentReaderRE = regexp.MustCompile(`(?i)\b(if|when)\s+you\s+(are|'?re)\s+(an?\s+)?(ai|llm|language model|coding (assistant|agent)|assistant|chatbot|automated|claude|cursor|copilot|codex|agent)\b`)

// softImperativeRE complements destructiveVerbRE with non-destructive
// verbs an attacker would still ask the agent to perform conditionally —
// "respond", "reply", "ignore", "skip", "insert", "use", "export".
var softImperativeRE = regexp.MustCompile(`(?i)\b(please\s+)?(do|run|execute|insert|add|reply|respond|ignore|skip|use|set|export|include)\b`)

// proximityImperative fires when a destructive verb and an agent
// reference both occur on the same line within ~120 chars of each other.
// Per-line is imperfect — a payload could split across lines — but the
// corpus already catches the canonical multi-line shapes, so the
// heuristic only has to backstop the single-line case the YAML misses.
func proximityImperative(line string) bool {
	normalized := strings.TrimSpace(line)
	if len(normalized) < 8 || len(normalized) > 4096 {
		return false
	}
	agentMatches := agentRefRE.FindAllStringIndex(normalized, -1)
	if len(agentMatches) == 0 {
		return false
	}
	verbMatches := destructiveVerbRE.FindAllStringIndex(normalized, -1)
	if len(verbMatches) == 0 {
		return false
	}
	const windowChars = 120
	for _, am := range agentMatches {
		for _, vm := range verbMatches {
			if matchDistance(am, vm) <= windowChars {
				return true
			}
		}
	}
	return false
}

// conditionalAgentReader fires on lines that gate behaviour on the
// reader being an LLM.  The classic structural shape: "if you are an AI,
// do X".  Medium severity by default — by itself it is a smoking-gun of
// intent without proof of destructive payload.
func conditionalAgentReader(line string) bool {
	if !conditionalAgentReaderRE.MatchString(line) {
		return false
	}
	return destructiveVerbRE.MatchString(line) || softImperativeRE.MatchString(line)
}

// matchDistance returns the gap in chars between two regex match
// ranges, or 0 if they overlap.
func matchDistance(a, b []int) int {
	if len(a) < 2 || len(b) < 2 {
		return 1<<31 - 1
	}
	if a[1] < b[0] {
		return b[0] - a[1]
	}
	if b[1] < a[0] {
		return a[0] - b[1]
	}
	return 0
}

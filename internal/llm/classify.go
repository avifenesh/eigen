package llm

import "strings"

// Classify infers a task's routing profile from the prompt text and whether
// images are attached. It is the FALLBACK path: the orchestrator can state a
// subtask's kind/difficulty explicitly (authoritative); when it doesn't — and
// for the top-level turn — these heuristics produce a reasonable default.
//
// Vision is detected reliably (an image is attached). Search and difficulty are
// keyword/length heuristics: deliberately conservative — when unsure they lean
// toward "general" and "medium" so the router never under-powers a task it
// can't read well.
func Classify(prompt string, hasImage bool) (TaskKind, Difficulty) {
	return classifyKind(prompt, hasImage), classifyDifficulty(prompt)
}

func classifyKind(prompt string, hasImage bool) TaskKind {
	if hasImage {
		return TaskVision
	}
	if needsSearch(prompt) {
		return TaskSearch
	}
	return TaskGeneral
}

// searchCues are phrases that strongly imply the task needs live, current
// information the model can't have from training.
var searchCues = []string{
	"search the web", "search for", "look up", "google ", "latest ", "current ",
	"up to date", "up-to-date", "recent news", "what's new", "whats new",
	"as of today", "this week", "this month", "release notes for",
	"changelog for", "who won", "stock price", "weather in",
}

func needsSearch(prompt string) bool {
	p := strings.ToLower(prompt)
	for _, cue := range searchCues {
		if strings.Contains(p, cue) {
			return true
		}
	}
	return false
}

// hardCues imply deep reasoning / architecture / subtle work.
var hardCues = []string{
	"architect", "design the", "refactor the", "debug", "root cause",
	"why does", "why is", "race condition", "deadlock", "optimize",
	"performance", "security", "vulnerab", "prove", "algorithm",
	"concurren", "distributed", "trade-off", "tradeoff", "migrate",
}

// trivialCues imply rote, well-specified, low-judgment work.
var trivialCues = []string{
	"rename", "format", "gofmt", "fix the typo", "typo", "add a comment",
	"comment out", "bump the version", "update the version", "lint",
	"reword", "rephrase", "capitalize", "indent",
}

func classifyDifficulty(prompt string) Difficulty {
	p := strings.ToLower(prompt)
	for _, cue := range hardCues {
		if strings.Contains(p, cue) {
			return DiffHard
		}
	}
	for _, cue := range trivialCues {
		if strings.Contains(p, cue) {
			return DiffTrivial
		}
	}
	// Length as a coarse proxy for scoping: a long, detailed prompt usually
	// means an underscoped/substantial task (→ opus/default); a short one is
	// usually well-scoped (→ sonnet; trivial cues above catch the grok tier).
	switch {
	case len(prompt) > 600:
		return DiffHard
	case len(prompt) > 200:
		return DiffMedium
	default:
		return DiffEasy
	}
}

// ParseTaskKind / ParseDifficulty map the orchestrator-supplied strings (task
// tool args) to enums, falling back to the heuristic default when empty or
// unrecognized so a bad value never blocks routing.
func ParseTaskKind(s string) (TaskKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "general", "code", "coding", "":
		return TaskGeneral, s != ""
	case "search", "web":
		return TaskSearch, true
	case "vision", "image":
		return TaskVision, true
	}
	return TaskGeneral, false
}

func ParseDifficulty(s string) (Difficulty, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trivial":
		return DiffTrivial, true
	case "easy":
		return DiffEasy, true
	case "medium", "":
		return DiffMedium, s != ""
	case "hard", "complex":
		return DiffHard, true
	}
	return DiffMedium, false
}

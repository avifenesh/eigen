package llm

import "strings"

// Classify is a legacy deterministic classifier retained for tests and any
// non-routing callers that want a cheap default. The production router does not
// use this wording heuristic for /route; unstated delegated subtasks are assessed
// by a prompt-router model instead.
func Classify(prompt string, hasImage bool) (TaskKind, Difficulty) {
	return classifyKind(prompt, hasImage), classifyDifficulty(prompt)
}

// frontendCues imply frontend/design work — where opus outranks the stricter
// gpt-5.5 within the med tier.
var frontendCues = []string{
	"frontend", "front-end", "css", "tailwind", "stylesheet", "ui ", " ui",
	"layout", "responsive", "component", "react", "vue", "svelte", "design the",
	"redesign", "visual", "animation", "landing page", "web page", "webpage",
	"html", "styling", "theme", "dark mode", "user interface", "ux",
}

// IsFrontend reports whether a prompt looks like frontend/design work.
func IsFrontend(prompt string) bool {
	p := strings.ToLower(prompt)
	for _, cue := range frontendCues {
		if strings.Contains(p, cue) {
			return true
		}
	}
	return false
}

func classifyKind(prompt string, hasImage bool) TaskKind {
	if hasImage {
		return TaskVision
	}
	if needsSocial(prompt) {
		return TaskSocial
	}
	if needsSearch(prompt) {
		return TaskSearch
	}
	return TaskGeneral
}

// socialCues imply the task needs X/Twitter reach — social-platform
// understanding only Grok's Live Search (x source) provides.
var socialCues = []string{
	"on x ", "on x,", "on x.", "on x?", "twitter", "tweet", "x thread",
	"x post", "what are people saying", "public sentiment", "social sentiment",
	"trending on", "viral", "x.com/",
}

func needsSocial(prompt string) bool {
	p := strings.ToLower(prompt)
	for _, cue := range socialCues {
		if strings.Contains(p, cue) {
			return true
		}
	}
	return false
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
	"architect", "design the", "refactor", "debug", "root cause",
	"why does", "why is", "why isn't", "why aren't", "race condition", "deadlock",
	"optimize", "performance", "security", "vulnerab", "prove", "algorithm",
	"concurren", "distributed", "trade-off", "tradeoff", "migrate",
	"implement a ", "implement the ", "build a system", "build a service",
	"add support for",
	"how does the ", "explain why", "explain how", "understand the",
	"make it ", "fix this ", "figure out", "investigate", "analyse", "analyze",
	"plan ", "strategy", "approach", "best way to", "should i", "what's the",
}

// trivialCues imply rote, well-specified, low-judgment work.
var trivialCues = []string{
	"rename", "format", "gofmt", "fix the typo", "typo", "add a comment",
	"comment out", "bump the version", "update the version", "lint",
	"reword", "rephrase", "capitalize", "indent", "delete the", "remove the",
	"move the file", "move this file", "add a newline", "add a blank line",
	"fix whitespace", "sort imports", "update copyright", "update license",
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
	// Length as a coarse proxy: a long prompt is usually under-scoped or
	// multi-step (→ medium/hard); a short one is well-scoped (→ easy/trivial).
	// Threshold is conservative — unknown prompts default to medium, which
	// routes to sonnet, not to the trivial grok tier.
	switch {
	case len(prompt) > 400:
		return DiffHard
	case len(prompt) > 80:
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
	case "social", "x", "twitter":
		return TaskSocial, true
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

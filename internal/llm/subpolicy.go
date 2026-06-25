package llm

import (
	"os"
	"strings"
)

// Subagent policy: capability-aware effort + model selection by subagent TYPE.
//
// The orchestrator delegates work tagged with a TYPE (what the subagent is FOR),
// distinct from Kind (a routing hint: general/search/vision/social) and
// Difficulty (how hard). Type drives two dials the old code couldn't:
//
//   - EFFORT per type (explore cheap, research/code high, general medium, judge
//     low-but-valid) — not the old one-way "trivial→medium" downshift.
//   - MODEL per type — capability is NOT price. A cheap model can be the BEST
//     fit (glm-5.2: 1M ctx + reasoning + included web_search, stronger than
//     sonnet-4.6 yet far cheaper) and an expensive one wrong for a job (grok has
//     search but is a poor researcher). Each type carries a preference LADDER;
//     we pick the first model whose provider is credentialed, so the policy
//     degrades gracefully (e.g. while the GLM account is suspended, research
//     falls through to the next capable model instead of erroring).
//
// Difficulty still modulates effort WITHIN a type: a trivial explore stays off,
// a hard code task can lift to xhigh. Type sets the baseline; difficulty nudges.

// SubagentType is what a delegated subagent is for. Empty = "general".
type SubagentType string

const (
	TypeExplore  SubagentType = "explore"  // locate code/files, cheap + fast, read-only
	TypeResearch SubagentType = "research" // deep investigation/synthesis — wants a strong reasoner
	TypeGeneral  SubagentType = "general"  // catch-all delegation
	TypeCode     SubagentType = "code"     // write/transform code — correctness matters
	TypeJudge    SubagentType = "judge"    // verify/score a claim — cheap but VALID, independent
)

// NormalizeSubagentType maps a free-form string (role name, tool arg) to a known
// type, defaulting to general. Role names map to their natural type so existing
// roles (researcher/reviewer/summarizer) get sensible effort without the caller
// passing a type.
func NormalizeSubagentType(s string) SubagentType {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "explore", "explorer", "locate", "search-code":
		return TypeExplore
	case "research", "researcher", "investigate", "deep-research":
		return TypeResearch
	case "code", "coder", "implement", "implementer", "transform":
		return TypeCode
	case "judge", "review", "reviewer", "verify", "assess", "critic":
		return TypeJudge
	case "", "general", "gp", "general-purpose":
		return TypeGeneral
	default:
		return TypeGeneral
	}
}

// SubagentEffort returns the reasoning effort for a (type, difficulty) pair as a
// GENERIC level ("off"/"low"/"medium"/"high"/"xhigh"); the provider clamps it to
// its own ladder (e.g. GLM's off/on, gpt's none..xhigh) via SetEffort. Empty is
// never returned — the caller always has a baseline. EIGEN_SUBTASK_EFFORT=keep
// disables the whole policy (handled by the caller; here we just compute).
func SubagentEffort(t SubagentType, difficulty string) string {
	base := map[SubagentType]string{
		TypeExplore:  "low",  // finding things doesn't need deep reasoning
		TypeResearch: "high", // synthesis across evidence wants depth
		TypeGeneral:  "medium",
		TypeCode:     "high", // correctness-critical
		TypeJudge:    "low",  // a strict yes/no/score — cheap but valid, not deep
	}[t]
	if base == "" {
		base = "medium"
	}
	// Difficulty nudges within the type: a hard task lifts one notch, a trivial
	// one drops a notch — but judge stays put (its job is bounded regardless of
	// the work's difficulty) and explore never lifts (it's meant to be cheap).
	switch strings.ToLower(strings.TrimSpace(difficulty)) {
	case "hard":
		if t != TypeJudge && t != TypeExplore {
			base = liftEffort(base)
		}
	case "trivial":
		if t != TypeJudge {
			base = lowerEffort(base)
		}
	}
	return base
}

var effortLadder = []string{"off", "low", "medium", "high", "xhigh"}

func liftEffort(level string) string {
	for i, l := range effortLadder {
		if l == level && i+1 < len(effortLadder) {
			return effortLadder[i+1]
		}
	}
	return level
}
func lowerEffort(level string) string {
	for i, l := range effortLadder {
		if l == level && i > 0 {
			return effortLadder[i-1]
		}
	}
	return level
}

// modelLadder is the per-type preference order (capability-first, NOT price).
// We walk it and take the first model whose provider is credentialed. The IDs
// here are the catalog's canonical ids; an uncredentialed/suspended provider
// (e.g. GLM out of quota) is simply skipped to the next.
//
// User's policy intent, encoded:
//   - opus    → the default strong generalist (general/code top of ladder)
//   - glm-5.2 → cheap AND capable (1M ctx, reasoning, web_search) — prime pick
//     for research + general once credentialed; high in those ladders
//   - composer→ super-fast, fine code → top of the explore/code-fast ladder
//   - gpt-5.x → review/assessment/judge
//   - grok    → SEARCH only (not a researcher) — never tops research
//   - sonnet  → questionable for tasks; reserved for dream (see DreamModelLadder)
func modelLadder(t SubagentType) []string {
	switch t {
	case TypeExplore:
		// Cheap + fast: a fast code/cheap model is plenty to grep+read+report.
		return []string{"grok-composer-2.5-fast", "glm-5.2", "us.anthropic.claude-haiku-4-5-20251001-v1:0", "us.anthropic.claude-sonnet-4-6"}
	case TypeResearch:
		// Strong reasoner with breadth; glm-5.2 is the cheap+capable sweet spot,
		// then opus, then gpt. Grok is deliberately ABSENT — search ≠ research.
		return []string{"glm-5.2", "us.anthropic.claude-opus-4-8", "openai.gpt-5.5", "us.anthropic.claude-sonnet-4-6"}
	case TypeCode:
		// Correctness-first; composer is fast+fine for code, opus for the hard
		// stuff, glm capable+cheap as the middle.
		return []string{"us.anthropic.claude-opus-4-8", "grok-composer-2.5-fast", "glm-5.2", "openai.gpt-5.5"}
	case TypeJudge:
		// Cheap but VALID + ideally independent of the agent's own model. gpt for
		// assessment, glm as the cheap-capable alternative, haiku as the floor.
		// NEVER the top-tier default (no point self-grading on the same brain).
		return []string{"openai.gpt-5.4", "glm-5.2", "us.anthropic.claude-haiku-4-5-20251001-v1:0", "openai.gpt-5.5"}
	default: // general
		return []string{"us.anthropic.claude-opus-4-8", "glm-5.2", "openai.gpt-5.5", "us.anthropic.claude-sonnet-4-6"}
	}
}

// SubagentModel returns the preferred model id for a type, picking the first in
// the type's ladder whose provider is credentialed. Returns "" when none are
// available (caller keeps routing/inheriting). An env override per type
// (EIGEN_SUBAGENT_MODEL_<TYPE>, e.g. EIGEN_SUBAGENT_MODEL_RESEARCH) wins.
func SubagentModel(t SubagentType) string {
	if v := strings.TrimSpace(os.Getenv("EIGEN_SUBAGENT_MODEL_" + strings.ToUpper(string(t)))); v != "" {
		return v
	}
	for _, id := range modelLadder(t) {
		if modelCredentialed(id) {
			return id
		}
	}
	return ""
}

// modelCredentialed reports whether a catalog model's provider currently has
// usable credentials AND isn't quota-frozen — so a suspended/unconfigured
// provider, OR one that 429'd on quota earlier today (a drained account whose
// KEY is still present, e.g. GLM), is skipped to the next ladder entry instead
// of being picked and re-429'd. The freeze auto-expires at midnight.
func modelCredentialed(modelID string) bool {
	prov := ResolveProvider("", modelID)
	if prov == "" {
		return false
	}
	if providerFrozen(canonicalProvider(prov)) {
		return false
	}
	return ProviderAvailable(prov)
}

// DreamModelLadder is the preference order for the dreaming/consolidation
// pipeline: SONNET first — deliberately NOT glm, so background dreaming doesn't
// drain the cheap-but-capable GLM quota that real tasks want. Falls through to
// other capable models only if sonnet isn't credentialed.
func DreamModelLadder() []string {
	return []string{
		"us.anthropic.claude-sonnet-4-6",
		"claude-sonnet-4-5-20250929",
		"us.anthropic.claude-haiku-4-5-20251001-v1:0",
		"openai.gpt-5.4",
	}
}

// FirstCredentialed returns the first model id in ids whose provider is usable,
// or "" — a small shared helper for the dream/judge provider builders.
func FirstCredentialed(ids ...string) string {
	for _, id := range ids {
		if modelCredentialed(id) {
			return id
		}
	}
	return ""
}

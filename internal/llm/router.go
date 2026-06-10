package llm

import "sort"

// TaskKind classifies what a task needs, so the router can require the right
// capabilities (not just raw quality).
type TaskKind int

const (
	TaskGeneral TaskKind = iota // ordinary coding/reasoning
	TaskSearch                  // needs live web/X search
	TaskVision                  // includes image input
)

// Difficulty is the minimum quality a task demands. The router will not pick a
// model below the matching quality floor unless nothing capable qualifies (then
// it picks the strongest capable model — never silently worse than necessary).
type Difficulty int

const (
	DiffTrivial Difficulty = iota // boilerplate, formatting, tiny edits
	DiffEasy                      // routine, well-specified
	DiffMedium                    // normal feature work
	DiffHard                      // architecture, subtle bugs, long reasoning
)

// targetTier is the quality tier a difficulty demands, per the user's ladder:
// simple→1 (grok/glm/composer ok), simple-med→2 (sonnet), med→3 (opus),
// hard→4 (frontier; hard work also normally stays on the default model).
func targetTier(d Difficulty) Tier {
	switch d {
	case DiffTrivial:
		return TierSimple
	case DiffEasy:
		return TierSimple
	case DiffMedium:
		return TierMed
	case DiffHard:
		return TierFrontier
	default:
		return TierMed
	}
}

// RouteRequest describes a task to route and the constraints on the choice.
type RouteRequest struct {
	Kind       TaskKind
	Difficulty Difficulty
	MinContext int // tokens the conversation needs to fit (0 = don't care)

	// Candidates are the model IDs the router may choose from — already filtered
	// to providers the user has credentials for and has allowed. Empty means no
	// choice is possible.
	Candidates []string
}

// Route picks the best model for req per the policy:
//  1. Keep only CAPABLE candidates: have the required capability (search/vision)
//     and a large enough context window.
//  2. Among those that are GOOD ENOUGH (quality ≥ the difficulty floor), pick
//     the CHEAPEST; break ties by higher quality, then by higher speed.
//  3. If none clear the floor, pick the STRONGEST capable model (never refuse to
//     do the task) — quality first, then cheaper, then faster.
//
// Returns the chosen model ID and true, or "" and false when no candidate is
// even capable (caller keeps the current model).
func Route(req RouteRequest) (string, bool) {
	capable := make([]string, 0, len(req.Candidates))
	for _, id := range req.Candidates {
		if isCapable(id, req) {
			capable = append(capable, id)
		}
	}
	if len(capable) == 0 {
		return "", false
	}

	// Quality-tier ladder: the difficulty demands a target tier. Pick the
	// LOWEST tier that is still >= the target (so a simple task is happily
	// served by a tier-1 grok/glm, while a hard task demands frontier and never
	// settles for less). If no capable model reaches the target, take the
	// highest tier available — do the task as well as we can, never worse.
	target := targetTier(req.Difficulty)
	best := Tier(0)
	for _, id := range capable {
		if t := scoreFor(id).Tier; t >= target && (best == 0 || t < best) {
			best = t
		}
	}
	if best == 0 {
		// Nothing reaches the target: use the highest tier present.
		for _, id := range capable {
			if t := scoreFor(id).Tier; t > best {
				best = t
			}
		}
	}

	// Among models in the chosen tier, prefer non-Bedrock (spare the
	// employer-paid account), then faster.
	pool := capable[:0:0]
	for _, id := range capable {
		if scoreFor(id).Tier == best {
			pool = append(pool, id)
		}
	}
	sort.SliceStable(pool, func(i, j int) bool {
		return preferNonBedrockFaster(pool[i], pool[j])
	})
	return pool[0], true
}

// preferNonBedrockFaster orders models within a tier: non-Bedrock first (the
// user's own pre-paid accounts, sparing the employer-paid Bedrock quota), then
// faster (so composer beats haiku in tier 1).
func preferNonBedrockFaster(a, b string) bool {
	if ab, bb := isBedrock(a), isBedrock(b); ab != bb {
		return !ab
	}
	return scoreFor(a).Speed > scoreFor(b).Speed
}

// isCapable reports whether a model can do the task at all: required capability
// flags (search/vision) and a context window large enough for the conversation.
func isCapable(id string, req RouteRequest) bool {
	m, ok := Lookup(id)
	if !ok {
		return false
	}
	switch req.Kind {
	case TaskSearch:
		if !m.Search {
			return false
		}
	case TaskVision:
		if !m.Vision {
			return false
		}
	}
	if req.MinContext > 0 && EffectiveContextWindow(id) > 0 && EffectiveContextWindow(id) < req.MinContext {
		return false
	}
	return true
}

// isBedrock reports whether a model is served by the employer-paid Bedrock
// account (the mantle and converse providers). The router prefers non-Bedrock
// models within a tier to spare the employer's quota.
func isBedrock(id string) bool {
	m, ok := Lookup(id)
	if !ok {
		return false
	}
	switch canonicalProvider(m.Provider) {
	case "mantle", "converse":
		return true
	}
	return false
}

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

// Difficulty classifies how much scoping and reasoning a task needs — the
// user's ladder, in their words:
//
//	trivial  "small, well-scoped tasks"                       → tier 1 (grok/glm/composer)
//	easy     "well-scoped, needs iteration, little reasoning"  → tier 2 (sonnet)
//	medium   "not fully scoped, reasoning, maybe long-running" → tier 3 (opus)
//	hard     "not scoped, reasoning, long-running"             → the DEFAULT model
//	         (whatever is currently set — fable today, opus if switched)
type Difficulty int

const (
	DiffTrivial Difficulty = iota // small, well-scoped (rename, format, tiny edit)
	DiffEasy                      // well-scoped, iterative, little reasoning
	DiffMedium                    // not fully scoped, needs reasoning, may run long
	DiffHard                      // unscoped + reasoning + long-running → default model
)

// targetTier is the quality tier a difficulty demands, per the user's ladder:
// trivial→1 (grok/glm/composer), easy→2 (sonnet), medium→3 (opus). Hard is not
// tier-mapped at all — it keeps the user's current DEFAULT model (see Route).
func targetTier(d Difficulty) Tier {
	switch d {
	case DiffTrivial:
		return TierSimple
	case DiffEasy:
		return TierSimpleMed
	case DiffMedium:
		return TierMed
	default:
		return TierFrontier
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

// Route picks the model for req per the user's quality-tier ladder:
//  1. HARD general tasks are NOT routed: they keep the user's current default
//     model (fable today, opus if that is what is set) — Route returns no
//     choice and the caller stays put. Hard search/vision tasks still route,
//     because the default may lack the capability.
//  2. Keep only CAPABLE candidates (required search/vision flag, big-enough
//     context window).
//  3. Target tier = targetTier(difficulty). Pick the LOWEST tier >= target
//     (simple work goes to tier-1, never wastefully up); if no capable model
//     reaches the target, take the highest tier present (never refuse, do the
//     task as well as possible).
//  4. Within the chosen tier: prefer non-Bedrock (spare the employer-paid
//     account), then faster.
//
// Returns the chosen model ID and true, or "" and false when routing should
// not change the model (hard general task, or no capable candidate).
func Route(req RouteRequest) (string, bool) {
	// Hard general work belongs to the user's default model — the strongest
	// thing they configured. Only capability needs (search/vision) override.
	if req.Difficulty == DiffHard && req.Kind == TaskGeneral {
		return "", false
	}
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

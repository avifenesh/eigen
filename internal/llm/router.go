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

// qualityFloor is the minimum Quality score for each difficulty.
func qualityFloor(d Difficulty) int {
	switch d {
	case DiffTrivial:
		return 0
	case DiffEasy:
		return 65
	case DiffMedium:
		return 78
	case DiffHard:
		return 90
	default:
		return 78
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

	floor := qualityFloor(req.Difficulty)
	goodEnough := make([]string, 0, len(capable))
	for _, id := range capable {
		if scoreFor(id).Quality >= floor {
			goodEnough = append(goodEnough, id)
		}
	}

	if len(goodEnough) > 0 {
		// Cheapest, then stronger, then faster.
		sort.SliceStable(goodEnough, func(i, j int) bool {
			return cheaperStrongerFaster(goodEnough[i], goodEnough[j])
		})
		return goodEnough[0], true
	}

	// Nothing meets the floor: take the strongest capable model (quality first,
	// then cheaper, then faster) — do the task as well as we can, never worse.
	sort.SliceStable(capable, func(i, j int) bool {
		return strongerCheaperFaster(capable[i], capable[j])
	})
	return capable[0], true
}

// isCapable reports whether a model can do the task at all: required capability
// flags and a context window large enough for the conversation.
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

// cheaperStrongerFaster orders a "good enough" pool: cheapest first; equal cost
// → stronger; equal cost+quality → faster. This is the cost-minimizing-at-equal-
// capability rule with speed as the final tiebreak.
func cheaperStrongerFaster(a, b string) bool {
	sa, sb := scoreFor(a), scoreFor(b)
	if sa.Cost != sb.Cost {
		return sa.Cost < sb.Cost
	}
	if sa.Quality != sb.Quality {
		return sa.Quality > sb.Quality
	}
	return sa.Speed > sb.Speed
}

// strongerCheaperFaster orders the fallback pool (nothing met the floor):
// strongest first, then cheaper, then faster.
func strongerCheaperFaster(a, b string) bool {
	sa, sb := scoreFor(a), scoreFor(b)
	if sa.Quality != sb.Quality {
		return sa.Quality > sb.Quality
	}
	if sa.Cost != sb.Cost {
		return sa.Cost < sb.Cost
	}
	return sa.Speed > sb.Speed
}

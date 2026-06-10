package llm

import "testing"

func TestRoutePicksCheapestGoodEnough(t *testing.T) {
	// Medium task: floor 78. Among capable+good-enough, cheapest wins.
	got, ok := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffMedium,
		Candidates: []string{"us.anthropic.claude-opus-4-8", "glm-4.7", "us.anthropic.claude-haiku-4-5"},
	})
	if !ok {
		t.Fatal("expected a choice")
	}
	// opus(95,cost90) and glm-4.7(80,cost14) clear floor 78; haiku(70) does not.
	// Cheapest good-enough = glm-4.7.
	if got != "glm-4.7" {
		t.Fatalf("want glm-4.7 (cheapest good-enough), got %s", got)
	}
}

func TestRouteTrivialPicksCheapest(t *testing.T) {
	// Trivial: floor 0, everything qualifies → cheapest overall.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffTrivial,
		Candidates: []string{"us.anthropic.claude-opus-4-8", "us.anthropic.claude-haiku-4-5", "glm-4.5-air"},
	})
	if got != "glm-4.5-air" { // cost 8, the cheapest
		t.Fatalf("trivial should pick the cheapest (glm-4.5-air), got %s", got)
	}
}

func TestRouteHardRequiresStrong(t *testing.T) {
	// Hard: floor 90. Only opus(95) qualifies; haiku/glm don't.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffHard,
		Candidates: []string{"us.anthropic.claude-haiku-4-5", "glm-4.7", "us.anthropic.claude-opus-4-8"},
	})
	if got != "us.anthropic.claude-opus-4-8" {
		t.Fatalf("hard task should pick opus, got %s", got)
	}
}

func TestRouteEqualCostPrefersStronger(t *testing.T) {
	// Two equal-cost models (opus-4-1 and opus-4-20250514 are both cost 100):
	// at equal cost, the stronger quality wins. opus-4-1(90) vs the dated
	// opus-4-20250514(89) — but they're different providers. Use the comparator
	// directly on a crafted equal-cost pair to assert the rule cleanly.
	routerScores["__hi"] = RouterScore{Quality: 90, Cost: 50, Speed: 50}
	routerScores["__lo"] = RouterScore{Quality: 80, Cost: 50, Speed: 50}
	defer func() { delete(routerScores, "__hi"); delete(routerScores, "__lo") }()
	if !cheaperStrongerFaster("__hi", "__lo") {
		t.Fatal("equal cost: stronger quality should sort first")
	}
	if cheaperStrongerFaster("__lo", "__hi") {
		t.Fatal("weaker should not sort before stronger at equal cost")
	}
}

func TestRouteSpeedTiebreak(t *testing.T) {
	// The user's example: two models equal on cost AND quality → faster wins.
	// Use the synthetic pair via scores: pick two we can make tie. grok-code-fast-1
	// (q75,cost15,speed90) vs glm-5-turbo (q78,cost12,speed88): not a tie, glm wins
	// on cost. To test the pure tiebreak, craft equal cost+quality directly.
	routerScores["__a"] = RouterScore{Quality: 70, Cost: 20, Speed: 60}
	routerScores["__b"] = RouterScore{Quality: 70, Cost: 20, Speed: 95}
	defer func() { delete(routerScores, "__a"); delete(routerScores, "__b") }()
	// Both unknown to the catalog Lookup, so isCapable would reject them.
	// Exercise the comparator directly instead.
	if !cheaperStrongerFaster("__b", "__a") {
		t.Fatal("equal cost+quality: faster (__b) should sort first")
	}
	if cheaperStrongerFaster("__a", "__b") {
		t.Fatal("slower should not sort before faster at equal cost+quality")
	}
}

func TestRouteSearchRequiresSearchModel(t *testing.T) {
	// Search task: only search-capable models are eligible.
	got, ok := Route(RouteRequest{
		Kind:       TaskSearch,
		Difficulty: DiffMedium,
		Candidates: []string{"us.anthropic.claude-opus-4-8", "glm-4.6", "grok-4"},
	})
	if !ok {
		t.Fatal("expected a search-capable choice")
	}
	// opus has no Search; glm-4.6 (catalog Search? no — only grok marks Search).
	// Verify the result is search-capable.
	if m, _ := Lookup(got); !m.Search {
		t.Fatalf("search task routed to a non-search model: %s", got)
	}
}

func TestRouteVisionRequiresVisionModel(t *testing.T) {
	got, ok := Route(RouteRequest{
		Kind:       TaskVision,
		Difficulty: DiffEasy,
		Candidates: []string{"grok-code-fast-1", "us.anthropic.claude-haiku-4-5", "glm-4.5"},
	})
	if !ok {
		t.Fatal("expected a vision-capable choice")
	}
	if m, _ := Lookup(got); !m.Vision {
		t.Fatalf("vision task routed to a non-vision model: %s", got)
	}
}

func TestRouteNoCapableCandidate(t *testing.T) {
	// Search task but no search model available → no choice.
	if _, ok := Route(RouteRequest{
		Kind:       TaskSearch,
		Candidates: []string{"us.anthropic.claude-opus-4-8", "local"},
	}); ok {
		t.Fatal("no search-capable candidate should yield no choice")
	}
	// Empty candidate set.
	if _, ok := Route(RouteRequest{Candidates: nil}); ok {
		t.Fatal("empty candidates should yield no choice")
	}
}

func TestRouteContextWindowGate(t *testing.T) {
	// A task needing 300k tokens excludes 200k-window models; grok-build (512k) fits.
	got, ok := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffEasy,
		MinContext: 300000,
		Candidates: []string{"us.anthropic.claude-haiku-4-5", "grok-build"},
	})
	if !ok || got != "grok-build" {
		t.Fatalf("large-context task should pick the 512k model, got %s (ok=%v)", got, ok)
	}
}

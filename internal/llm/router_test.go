package llm

import "testing"

func TestRouteSimpleUsesTier1NonBedrock(t *testing.T) {
	// Simple task: a tier-1 model (grok/glm) is trusted and preferred over the
	// Bedrock opus, even though opus is "stronger".
	got, ok := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffTrivial,
		Candidates: []string{"us.anthropic.claude-opus-4-8", "glm-4.7", "grok-build"},
	})
	if !ok {
		t.Fatal("expected a choice")
	}
	if scoreFor(got).Tier != TierSimple {
		t.Fatalf("simple task should pick a tier-1 model, got %s (tier %d)", got, scoreFor(got).Tier)
	}
	if isBedrock(got) {
		t.Fatalf("simple task should avoid Bedrock when tier-1 non-Bedrock exists, got %s", got)
	}
}

func TestRouteMediumUsesOpus(t *testing.T) {
	// Medium task → tier 3 (opus), not a tier-1 grok/glm.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffMedium,
		Candidates: []string{"grok-build", "us.anthropic.claude-opus-4-8", "us.anthropic.claude-sonnet-4-6"},
	})
	if scoreFor(got).Tier != TierMed {
		t.Fatalf("medium task should pick tier-3 (opus), got %s (tier %d)", got, scoreFor(got).Tier)
	}
}

func TestRouteHardGeneralKeepsDefault(t *testing.T) {
	// Hard general work stays on the user's default model: Route declines.
	if got, ok := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffHard,
		Candidates: []string{"grok-build", "us.anthropic.claude-opus-4-8", "claude-fable-5"},
	}); ok {
		t.Fatalf("hard general task must not be routed, got %s", got)
	}
}

func TestRouteHardSearchStillRoutes(t *testing.T) {
	// Hard + search: the default may lack search, so routing still applies and
	// picks the highest search-capable tier.
	got, ok := Route(RouteRequest{
		Kind:       TaskSearch,
		Difficulty: DiffHard,
		Candidates: []string{"grok-build", "glm-5.1", "us.anthropic.claude-opus-4-8"},
	})
	if !ok {
		t.Fatal("hard search should still route")
	}
	if m, _ := Lookup(got); !m.Search {
		t.Fatalf("hard search routed to non-search model %s", got)
	}
}

func TestRouteClimbsWhenTargetTierAbsent(t *testing.T) {
	// Medium wants tier 3 (opus); none present → climb to the next tier up
	// (frontier), never down to tier-1.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffMedium,
		Candidates: []string{"grok-build", "claude-fable-5"},
	})
	if got != "claude-fable-5" {
		t.Fatalf("with no opus, medium should climb to frontier, got %s", got)
	}
}

func TestRouteFallsToHighestWhenBelowTarget(t *testing.T) {
	// Medium wants tier 3 (opus); only tier-1/2 available → take the highest
	// present (sonnet), never refuse and never drop to tier-1.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffMedium,
		Candidates: []string{"grok-build", "us.anthropic.claude-sonnet-4-6"},
	})
	if got != "us.anthropic.claude-sonnet-4-6" {
		t.Fatalf("medium with no opus should take the highest (sonnet), got %s", got)
	}
}

func TestRouteWithinTierPrefersNonBedrockThenFaster(t *testing.T) {
	// Tier 1: haiku is Bedrock; composer + glm-turbo are not. Non-Bedrock wins,
	// and among those the faster (composer, speed 94) leads.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffTrivial,
		Candidates: []string{"us.anthropic.claude-haiku-4-5", "glm-5-turbo", "grok-composer-2.5-fast"},
	})
	if got != "grok-composer-2.5-fast" {
		t.Fatalf("tier-1: non-Bedrock + fastest should win (composer), got %s", got)
	}
}

func TestRouteSearchRequiresSearchModel(t *testing.T) {
	got, ok := Route(RouteRequest{
		Kind:       TaskSearch,
		Difficulty: DiffMedium,
		Candidates: []string{"us.anthropic.claude-opus-4-8", "glm-4.6", "grok-4"},
	})
	if !ok {
		t.Fatal("expected a search-capable choice")
	}
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
	if _, ok := Route(RouteRequest{
		Kind:       TaskSearch,
		Candidates: []string{"us.anthropic.claude-opus-4-8", "local"},
	}); ok {
		t.Fatal("no search-capable candidate should yield no choice")
	}
	if _, ok := Route(RouteRequest{Candidates: nil}); ok {
		t.Fatal("empty candidates should yield no choice")
	}
}

func TestRouteContextWindowGate(t *testing.T) {
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

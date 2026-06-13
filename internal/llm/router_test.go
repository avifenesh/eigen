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

func TestRouteHardGeneralRoutesToBestModel(t *testing.T) {
	// Hard general: route to the best available model (tier 3 fable/opus).
	// The caller compares the result to the active model and skips the switch
	// when they already match — no redundant churn.
	got, ok := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffHard,
		Candidates: []string{"grok-build", "us.anthropic.claude-opus-4-8", "claude-fable-5"},
	})
	if !ok {
		t.Fatal("hard general must route to the best model")
	}
	// Should pick fable or opus (tier 3) over grok (tier 1).
	if got == "grok-build" {
		t.Fatalf("hard general must not route to tier-1 grok, got %s", got)
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
		Candidates: []string{"grok-build", "global.anthropic.claude-fable-5"},
	})
	if got != "global.anthropic.claude-fable-5" {
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

func TestRouteSocialRequiresGrok(t *testing.T) {
	// Social (X reach) is grok-only: GLM searches the web but cannot reach X.
	got, ok := Route(RouteRequest{
		Kind:       TaskSocial,
		Difficulty: DiffMedium,
		Candidates: []string{"glm-5.1", "us.anthropic.claude-opus-4-8", "grok-4", "grok-build"},
	})
	if !ok {
		t.Fatal("expected a social-capable choice")
	}
	if m, _ := Lookup(got); !m.Social {
		t.Fatalf("social task routed to a non-social model: %s", got)
	}
	// And hard social still routes (the default model can't reach X).
	if _, ok := Route(RouteRequest{
		Kind:       TaskSocial,
		Difficulty: DiffHard,
		Candidates: []string{"grok-build", "glm-5.1"},
	}); !ok {
		t.Fatal("hard social should still route")
	}
	// No grok available → no choice (don't pretend GLM can read X).
	if _, ok := Route(RouteRequest{
		Kind:       TaskSocial,
		Candidates: []string{"glm-5.1", "us.anthropic.claude-opus-4-8"},
	}); ok {
		t.Fatal("no social-capable candidate should yield no choice")
	}
}

func TestRouteMediumGeneralPrefersGPT(t *testing.T) {
	// Medium general: gpt-5.5 is stricter/more correct than opus → it wins.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffMedium,
		Candidates: []string{"us.anthropic.claude-opus-4-8", "openai.gpt-5.5"},
	})
	if got != "openai.gpt-5.5" {
		t.Fatalf("medium general should prefer gpt-5.5 (stricter), got %s", got)
	}
}

func TestRouteFrontendPrefersOpus(t *testing.T) {
	// Frontend medium: opus is the better design model → it wins over gpt-5.5.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffMedium,
		Frontend:   true,
		Candidates: []string{"openai.gpt-5.5", "us.anthropic.claude-opus-4-8"},
	})
	if got != "us.anthropic.claude-opus-4-8" {
		t.Fatalf("frontend task should prefer opus (design), got %s", got)
	}
}

func TestGPTAndOpusShareMedTier(t *testing.T) {
	if scoreFor("openai.gpt-5.5").Tier != TierMed {
		t.Error("gpt-5.5 should be tier-3 (med), taking opus tasks")
	}
	if scoreFor("us.anthropic.claude-opus-4-8").Tier != TierMed {
		t.Error("opus should be tier-3 (med)")
	}
}

func TestRouteBedrockAvoidedOnlyAtTrueTie(t *testing.T) {
	// The Bedrock-avoidance tiebreak: at equal affinity AND equal rank, the
	// non-Bedrock model wins (spares the employer-paid Bedrock quota). Exercise
	// tierOrder directly — opus-4-8 is Bedrock; a same-rank/same-affinity
	// non-Bedrock peer should sort ahead of it. (No native-Anthropic models
	// remain to form a natural in-catalog tie, so we assert the tiebreak
	// function on a constructed equal pair.)
	if !tierOrder("openai.gpt-5.5", "us.anthropic.claude-opus-4-8", false) {
		// gpt-5.5 (rank 3, non-Bedrock) vs opus-4-8 (rank 3, Bedrock) on a
		// general task: gpt is Strict (affinity), so it wins outright — but
		// even stripping affinity, non-Bedrock breaks the tie.
		t.Fatal("non-Bedrock peer should sort ahead of the Bedrock model")
	}

	// NOT a true tie: opus-4-8 (Bedrock, rank 3, Design) vs gpt-5.4 (rank 2) on
	// a frontend task — Design affinity + higher quality wins, Bedrock is NOT
	// avoided.
	got, _ := Route(RouteRequest{
		Kind:       TaskGeneral,
		Difficulty: DiffMedium,
		Frontend:   true, // frontend → Design affinity favors opus
		Candidates: []string{"us.anthropic.claude-opus-4-8", "openai.gpt-5.4"},
	})
	if got != "us.anthropic.claude-opus-4-8" {
		t.Fatalf("quality + design must beat Bedrock-avoidance: want opus-4-8, got %s", got)
	}
}

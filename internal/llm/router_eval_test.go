package llm

import "testing"

// TestRouterEval is a golden evaluation: realistic tasks routed over the full
// catalog (as if all providers were credentialed), asserting the choice lands
// in the right QUALITY TIER for the difficulty, has the right capability, and —
// within a tier — avoids the employer-paid Bedrock account. It validates the
// user's ladder end-to-end and survives tuning by asserting intent, not exact
// model IDs.
func TestRouterEval(t *testing.T) {
	all := []string{}
	for _, m := range Catalog {
		all = append(all, m.ID)
	}

	cases := []struct {
		name           string
		req            RouteRequest
		wantTier       Tier // chosen model's tier must equal this (0 = don't check)
		wantNonBedrock bool
		wantSearch     bool
		wantVision     bool
	}{
		{
			name:           "trivial → tier-1, non-Bedrock (grok/glm trusted for simple)",
			req:            RouteRequest{Kind: TaskGeneral, Difficulty: DiffTrivial, Candidates: all},
			wantTier:       TierSimple,
			wantNonBedrock: true,
		},
		{
			// Quality-first: the NEWEST sonnet (4-6, Bedrock) wins over the
			// older native sonnet — Bedrock is avoided only at EQUAL quality,
			// never at its cost.
			name:     "easy (well-scoped, iterative) → tier-2, newest sonnet even on Bedrock",
			req:      RouteRequest{Kind: TaskGeneral, Difficulty: DiffEasy, Candidates: all},
			wantTier: TierSimpleMed,
		},
		{
			name:     "medium → tier-3 (opus)",
			req:      RouteRequest{Kind: TaskGeneral, Difficulty: DiffMedium, Candidates: all},
			wantTier: TierMed,
		},
		// NOTE: hard GENERAL tasks are asserted separately below — Route
		// declines so the user's default model keeps them.
		{
			name:       "search → a search-capable model",
			req:        RouteRequest{Kind: TaskSearch, Difficulty: DiffMedium, Candidates: all},
			wantSearch: true,
		},
		{
			name:       "vision → a vision-capable model",
			req:        RouteRequest{Kind: TaskVision, Difficulty: DiffMedium, Candidates: all},
			wantVision: true,
		},
		{
			name:       "hard SEARCH still routes (default may lack the capability)",
			req:        RouteRequest{Kind: TaskSearch, Difficulty: DiffHard, Candidates: all},
			wantSearch: true,
		},
	}

	// Hard general tasks keep the user's default model: Route must decline.
	if got, ok := Route(RouteRequest{Kind: TaskGeneral, Difficulty: DiffHard, Candidates: all}); ok {
		t.Errorf("hard general task must NOT be routed (keep the default model), got %s", got)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Route(c.req)
			if !ok {
				t.Fatalf("no model chosen")
			}
			s := scoreFor(got)
			m, _ := Lookup(got)
			if c.wantTier != 0 && s.Tier != c.wantTier {
				t.Errorf("chose %s (tier %d) — want tier %d", got, s.Tier, c.wantTier)
			}
			if c.wantNonBedrock && isBedrock(got) {
				t.Errorf("chose %s — Bedrock, but a non-Bedrock model should have served", got)
			}
			if c.wantSearch && !m.Search {
				t.Errorf("chose %s — not search-capable", got)
			}
			if c.wantVision && !m.Vision {
				t.Errorf("chose %s — not vision-capable", got)
			}
			t.Logf("%s → %s (tier %d, speed %d, bedrock=%v)", c.name, got, s.Tier, s.Speed, isBedrock(got))
		})
	}
}

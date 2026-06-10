package llm

import "testing"

// TestRouterEval is a golden evaluation: realistic tasks routed over the full
// catalog (as if all providers were credentialed), asserting the choice has the
// right CAPABILITY and lands in the expected cost/quality band. It validates
// the policy end-to-end rather than individual comparisons. If scores are
// retuned, these assert the *intent* still holds (e.g. "a trivial task must not
// pick a frontier model", "search must go to a search model").
func TestRouterEval(t *testing.T) {
	all := []string{}
	for _, m := range Catalog {
		all = append(all, m.ID)
	}

	cases := []struct {
		name       string
		req        RouteRequest
		wantCostLE int  // chosen model's Cost must be <= this (cheapness check)
		wantQualGE int  // chosen model's Quality must be >= this
		wantSearch bool // chosen must be search-capable
		wantVision bool // chosen must be vision-capable
	}{
		{
			name:       "trivial rename → cheapest, never frontier",
			req:        RouteRequest{Kind: TaskGeneral, Difficulty: DiffTrivial, Candidates: all},
			wantCostLE: 5, // must pick something cheap, not opus/gpt-5.5
		},
		{
			name:       "hard architecture → top-tier quality",
			req:        RouteRequest{Kind: TaskGeneral, Difficulty: DiffHard, Candidates: all},
			wantQualGE: 90,
		},
		{
			name:       "medium feature → good-enough but not wasteful",
			req:        RouteRequest{Kind: TaskGeneral, Difficulty: DiffMedium, Candidates: all},
			wantQualGE: 78,
			wantCostLE: 40, // shouldn't reach for the priciest when a mid model does
		},
		{
			name:       "search task → a search-capable model",
			req:        RouteRequest{Kind: TaskSearch, Difficulty: DiffMedium, Candidates: all},
			wantSearch: true,
		},
		{
			name:       "vision task → a vision-capable model",
			req:        RouteRequest{Kind: TaskVision, Difficulty: DiffMedium, Candidates: all},
			wantVision: true,
		},
		{
			name:       "easy task → cheap and fast tier",
			req:        RouteRequest{Kind: TaskGeneral, Difficulty: DiffEasy, Candidates: all},
			wantCostLE: 7,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Route(c.req)
			if !ok {
				t.Fatalf("no model chosen")
			}
			s := scoreFor(got)
			m, _ := Lookup(got)
			if c.wantCostLE > 0 && s.Cost > c.wantCostLE {
				t.Errorf("chose %s (cost %d) — want cost <= %d", got, s.Cost, c.wantCostLE)
			}
			if c.wantQualGE > 0 && s.Quality < c.wantQualGE {
				t.Errorf("chose %s (quality %d) — want quality >= %d", got, s.Quality, c.wantQualGE)
			}
			if c.wantSearch && !m.Search {
				t.Errorf("chose %s — not search-capable", got)
			}
			if c.wantVision && !m.Vision {
				t.Errorf("chose %s — not vision-capable", got)
			}
			t.Logf("%s → %s (q%d c%d s%d)", c.name, got, s.Quality, s.Cost, s.Speed)
		})
	}
}

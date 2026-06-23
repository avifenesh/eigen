package main

import (
	"context"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// recProvider records every Request it sees and replies with a fixed final
// answer (no tool calls → the agent loop finishes the turn immediately).
type recProvider struct {
	id   string
	seen []llm.Request
}

func (p *recProvider) Name() string    { return p.id }
func (p *recProvider) ModelID() string { return p.id }
func (p *recProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	p.seen = append(p.seen, req)
	return &llm.Response{Text: "ok-" + p.id}, nil
}

// userTexts returns the user-message texts from the LAST request the provider
// saw — enough to prove which prior turns were in context.
func lastUserTexts(p *recProvider) []string {
	if len(p.seen) == 0 {
		return nil
	}
	var out []string
	for _, m := range p.seen[len(p.seen)-1].Messages {
		if m.Role == llm.RoleUser {
			out = append(out, m.Text)
		}
	}
	return out
}

// TestWorkflowStepRunnerModelOverrideCarriesContext is the regression test for
// APP-017: a step that names a model must run on the ONE carried session via a
// live switch (so it sees prior steps' work), NOT an isolated subtask — and the
// session's original provider must be restored afterward.
func TestWorkflowStepRunnerModelOverrideCarriesContext(t *testing.T) {
	base := &recProvider{id: "base"}
	override := &recProvider{id: "override"}

	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	a := &agent.Agent{
		Provider: base,
		Tools:    reg,
		Perm:     agent.PermAuto,
		ModelProvider: func(model string) (llm.Provider, error) {
			if model == "override" {
				return override, nil
			}
			return nil, nil
		},
	}
	sess := a.NewSession()
	run := workflowStepRunner(a, sess)

	// Step 1 on the base model.
	if _, err := run(context.Background(), "step one", ""); err != nil {
		t.Fatalf("step 1: %v", err)
	}
	// Step 2 names a different model — must carry step 1's work on the same session.
	if _, err := run(context.Background(), "step two", "override"); err != nil {
		t.Fatalf("step 2: %v", err)
	}

	// The override provider must have actually been used for step 2.
	if len(override.seen) != 1 {
		t.Fatalf("override provider used %d times, want 1 (isolated subtask would be 1 but on a fresh session)", len(override.seen))
	}
	// Context carry: the override step's request must contain BOTH the prior
	// turn's user text and its own — proof it ran on the carried session, not a
	// fresh isolated one. (A subtask would only see "step two".)
	got := lastUserTexts(override)
	if !contains(got, "step one") || !contains(got, "step two") {
		t.Fatalf("override step lost prior context: user texts = %v (want both 'step one' and 'step two')", got)
	}

	// The live provider must be restored after the override step.
	if a.CurrentProvider() != base {
		t.Fatalf("live provider not restored: got %v, want base", a.CurrentProvider().ModelID())
	}

	// A following inherit-step continues on base and still carries everything.
	if _, err := run(context.Background(), "step three", ""); err != nil {
		t.Fatalf("step 3: %v", err)
	}
	got3 := lastUserTexts(base)
	if !contains(got3, "step one") || !contains(got3, "step two") || !contains(got3, "step three") {
		t.Fatalf("post-override base step lost context: %v", got3)
	}
}

// TestWorkflowStepRunnerUnavailableModelFallsBackToCarry verifies that when the
// named model can't be built, the step still runs on the carried session
// (context preserved) rather than failing or losing context.
func TestWorkflowStepRunnerUnavailableModelFallsBackToCarry(t *testing.T) {
	base := &recProvider{id: "base"}
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	a := &agent.Agent{
		Provider: base,
		Tools:    reg,
		Perm:     agent.PermAuto,
		ModelProvider: func(string) (llm.Provider, error) {
			return nil, context.DeadlineExceeded // any build error
		},
	}
	sess := a.NewSession()
	run := workflowStepRunner(a, sess)

	if _, err := run(context.Background(), "first", ""); err != nil {
		t.Fatalf("step 1: %v", err)
	}
	if _, err := run(context.Background(), "second", "bad-model"); err != nil {
		t.Fatalf("step 2 (unavailable model) should fall back, got error: %v", err)
	}
	// Ran on base (carried session): the base provider saw both turns.
	got := lastUserTexts(base)
	if !contains(got, "first") || !contains(got, "second") {
		t.Fatalf("fallback lost context: %v", got)
	}
	if a.CurrentProvider() != base {
		t.Fatalf("live provider changed on a failed override")
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

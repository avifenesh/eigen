package agent

import (
	"context"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

// effortProv is a fake provider implementing EffortSetter for subtask-effort tests.
type effortProv struct {
	id     string
	effort string
}

func (p *effortProv) Name() string    { return p.id }
func (p *effortProv) ModelID() string { return p.id }
func (p *effortProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{}, nil
}
func (p *effortProv) SetEffort(level string) bool { p.effort = level; return true }
func (p *effortProv) Effort() string              { return p.effort }

// Subtask effort is CAPPED at a safe middle ("medium"), never floored — flooring
// reasoning would hurt quality. It only steps DOWN, only when the model has a
// real "medium" rung, and never disables reasoning.
func TestApplySubtaskEffortCapsAtMedium(t *testing.T) {
	// Claude-style ordered set {low, medium, high, xhigh, max}.
	const claude = "us.anthropic.claude-opus-4-8"

	t.Run("trivial caps max→medium (not the floor)", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "max"}
		w := applySubtaskEffort(p, "trivial")
		if p.effort != "medium" {
			t.Fatalf("want effort capped to medium, got %q (where=%q)", p.effort, w)
		}
		if w == "" {
			t.Fatal("expected a where-note when effort changed")
		}
	})

	t.Run("easy caps high→medium", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "high"}
		if w := applySubtaskEffort(p, "easy"); p.effort != "medium" || w == "" {
			t.Fatalf("easy should cap to medium; got effort=%q where=%q", p.effort, w)
		}
	})

	t.Run("never goes BELOW medium (low stays low, not floored further)", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "low"}
		if w := applySubtaskEffort(p, "trivial"); p.effort != "low" || w != "" {
			t.Fatalf("already-below-medium must stay; got effort=%q where=%q", p.effort, w)
		}
	})

	t.Run("medium unchanged (no churn)", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "medium"}
		if w := applySubtaskEffort(p, "trivial"); p.effort != "medium" || w != "" {
			t.Fatalf("medium must stay medium with no note; got effort=%q where=%q", p.effort, w)
		}
	})

	t.Run("medium difficulty keeps configured effort", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "max"}
		if w := applySubtaskEffort(p, "medium"); p.effort != "max" || w != "" {
			t.Fatalf("medium difficulty must keep effort; got effort=%q where=%q", p.effort, w)
		}
	})

	t.Run("opt-out keeps", func(t *testing.T) {
		t.Setenv("EIGEN_SUBTASK_EFFORT", "keep")
		p := &effortProv{id: claude, effort: "max"}
		if w := applySubtaskEffort(p, "trivial"); p.effort != "max" || w != "" {
			t.Fatalf("opt-out must keep; got effort=%q where=%q", p.effort, w)
		}
	})
}

// A model with NO safe middle rung ({off, on}, e.g. GLM) is left UNTOUCHED — the
// only step down from "on" is "off", which disables reasoning entirely.
func TestApplySubtaskEffortLeavesNoMiddleModelsAlone(t *testing.T) {
	// llm.ModelEffortLevels resolves from the catalog; use a GLM id that maps to
	// {off, on}. If the catalog lacks a "medium" rung, applySubtaskEffort no-ops.
	for _, id := range []string{"glm-5.2", "glm-4.6"} {
		p := &effortProv{id: id, effort: "on"}
		if w := applySubtaskEffort(p, "trivial"); p.effort != "on" || w != "" {
			t.Fatalf("%s ({off,on}) must be untouched (no safe middle); got effort=%q where=%q", id, p.effort, w)
		}
	}
}

// A subtask's effort discipline must NOT mutate the provider the parent (or any
// other session) shares. subAgent reaches a shared provider on every path — the
// inherited a.provider(), or a router-cache instance reused across sessions — so
// it must give the subtask a provider it exclusively owns before lowering effort.
// Regression for the in-place SetEffort/SetFast bleed.
func TestSubAgentEffortDoesNotMutateParentProvider(t *testing.T) {
	t.Run("inherited provider stays untouched", func(t *testing.T) {
		// claude-opus-4-8 has a real "medium" rung, so discipline WOULD lower a
		// max-effort provider — proving it doesn't touch this shared instance.
		shared := &effortProv{id: "us.anthropic.claude-opus-4-8", effort: "max"}
		a := &Agent{Provider: shared, Perm: PermAuto}

		sub, _ := a.subAgent(context.Background(), "trivial cleanup", SubtaskOpts{Difficulty: "trivial"})

		if shared.effort != "max" {
			t.Fatalf("parent/shared provider effort was mutated by the subtask: got %q, want %q", shared.effort, "max")
		}
		// The subtask must not be holding the shared instance when it disciplines
		// effort — otherwise the cap would have bled into the parent.
		if sub.Provider == llm.Provider(shared) {
			t.Fatal("subtask kept the shared provider; effort discipline would bleed into the parent")
		}
	})

	t.Run("router-cache provider stays untouched", func(t *testing.T) {
		shared := &effortProv{id: "us.anthropic.claude-opus-4-8", effort: "max"}
		// Parent runs a different provider; the router hands the subtask the shared
		// cache instance (as the real autoRouter.providerFor cache does).
		a := &Agent{
			Provider: &mockProvider{},
			Perm:     PermAuto,
			Router: func(_ context.Context, _, _, _ string, _ bool) (llm.Provider, string, string) {
				return shared, "routed → shared", ""
			},
		}

		_, _ = a.subAgent(context.Background(), "easy edit", SubtaskOpts{Difficulty: "easy"})

		if shared.effort != "max" {
			t.Fatalf("router-cache provider effort was mutated by the subtask: got %q, want %q", shared.effort, "max")
		}
	})

	t.Run("non-discipline difficulty keeps inheriting the shared provider", func(t *testing.T) {
		// medium/hard apply no discipline, so there's no reason to build a fresh
		// provider — the subtask should inherit the shared one as before.
		shared := &effortProv{id: "us.anthropic.claude-opus-4-8", effort: "max"}
		a := &Agent{Provider: shared, Perm: PermAuto}

		sub, _ := a.subAgent(context.Background(), "hard refactor", SubtaskOpts{Difficulty: "hard"})

		if sub.Provider != llm.Provider(shared) {
			t.Fatal("non-discipline subtask should inherit the shared provider unchanged")
		}
		if shared.effort != "max" {
			t.Fatalf("shared provider effort changed for a hard subtask: got %q", shared.effort)
		}
	})
}

// effortFastProv implements EffortSetter + FastModer for fast-routing tests.
type effortFastProv struct {
	id     string
	effort string
	fast   bool
}

func (p *effortFastProv) Name() string    { return p.id }
func (p *effortFastProv) ModelID() string { return p.id }
func (p *effortFastProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{}, nil
}
func (p *effortFastProv) SetEffort(l string) bool { p.effort = l; return true }
func (p *effortFastProv) Effort() string          { return p.effort }
func (p *effortFastProv) SetFast(on bool) bool    { p.fast = on; return on }
func (p *effortFastProv) FastMode() bool          { return p.fast }

func TestApplySubtaskFast(t *testing.T) {
	t.Run("trivial enables fast", func(t *testing.T) {
		p := &effortFastProv{id: "gpt-5.5"}
		if w := applySubtaskFast(p, "trivial"); w == "" || !p.fast {
			t.Fatalf("trivial should enable fast; fast=%v where=%q", p.fast, w)
		}
	})
	t.Run("hard keeps configured", func(t *testing.T) {
		p := &effortFastProv{id: "gpt-5.5"}
		if w := applySubtaskFast(p, "hard"); w != "" || p.fast {
			t.Fatalf("hard must not toggle fast; fast=%v where=%q", p.fast, w)
		}
	})
	t.Run("already-fast no churn", func(t *testing.T) {
		p := &effortFastProv{id: "gpt-5.5", fast: true}
		if w := applySubtaskFast(p, "easy"); w != "" {
			t.Fatalf("already-fast should report nothing, got %q", w)
		}
	})
	t.Run("opt-out", func(t *testing.T) {
		t.Setenv("EIGEN_SUBTASK_EFFORT", "keep")
		p := &effortFastProv{id: "gpt-5.5"}
		if w := applySubtaskFast(p, "trivial"); w != "" || p.fast {
			t.Fatalf("opt-out must keep; fast=%v", p.fast)
		}
	})
}

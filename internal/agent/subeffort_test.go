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

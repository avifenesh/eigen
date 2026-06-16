package agent

import (
	"context"
	"os"
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

func TestApplySubtaskEffortLowersForCheapDifficulties(t *testing.T) {
	// Use a model with a known ordered effort set (opus-style: low..max).
	const model = "us.anthropic.claude-opus-4-8"

	t.Run("trivial lowers from max", func(t *testing.T) {
		p := &effortProv{id: model, effort: "max"}
		w := applySubtaskEffort(p, "trivial")
		if p.effort == "max" {
			t.Fatalf("effort should have been lowered, still %q (where=%q)", p.effort, w)
		}
		if w == "" {
			t.Fatal("expected a where-note when effort changed")
		}
	})

	t.Run("medium keeps", func(t *testing.T) {
		p := &effortProv{id: model, effort: "max"}
		if w := applySubtaskEffort(p, "medium"); w != "" || p.effort != "max" {
			t.Fatalf("medium must keep effort; got effort=%q where=%q", p.effort, w)
		}
	})

	t.Run("already-low not raised", func(t *testing.T) {
		p := &effortProv{id: model, effort: "low"}
		if w := applySubtaskEffort(p, "easy"); w != "" || p.effort != "low" {
			t.Fatalf("already-low must stay; got effort=%q where=%q", p.effort, w)
		}
	})

	t.Run("opt-out keeps", func(t *testing.T) {
		t.Setenv("EIGEN_SUBTASK_EFFORT", "keep")
		p := &effortProv{id: model, effort: "max"}
		if w := applySubtaskEffort(p, "trivial"); w != "" || p.effort != "max" {
			t.Fatalf("opt-out must keep; got effort=%q where=%q", p.effort, w)
		}
	})

	_ = os.Unsetenv
}

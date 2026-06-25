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

// setSubtaskEffort forces a provider's effort to a target level (clamped to the
// model's ladder), BOTH directions — the per-type policy can lower a cheap
// explore or raise a hard code task. (Replaces the old floor-only cap.)
func TestSetSubtaskEffort(t *testing.T) {
	const claude = "us.anthropic.claude-opus-4-8" // ordered {low,medium,high,xhigh,max}

	t.Run("forces down", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "max"}
		if w := setSubtaskEffort(p, "low"); p.effort != "low" || w == "" {
			t.Fatalf("want low; got effort=%q where=%q", p.effort, w)
		}
	})
	t.Run("forces up", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "low"}
		if w := setSubtaskEffort(p, "high"); p.effort != "high" || w == "" {
			t.Fatalf("want high; got effort=%q where=%q", p.effort, w)
		}
	})
	t.Run("same level no churn", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "medium"}
		if w := setSubtaskEffort(p, "medium"); w != "" {
			t.Fatalf("same level should report nothing, got %q", w)
		}
	})
	t.Run("empty level no-op", func(t *testing.T) {
		p := &effortProv{id: claude, effort: "high"}
		if w := setSubtaskEffort(p, ""); p.effort != "high" || w != "" {
			t.Fatalf("empty level must no-op; got effort=%q where=%q", p.effort, w)
		}
	})
}

// A coarse-ladder model ({off,on}, e.g. GLM) clamps the generic level to its
// nearest rung rather than being skipped: "low"→a low rung, "high"→"on".
func TestSetSubtaskEffortClampsCoarseLadder(t *testing.T) {
	// glm-5.2 maps to {off,on} in the catalog.
	p := &effortProv{id: "glm-5.2", effort: "off"}
	if w := setSubtaskEffort(p, "high"); p.effort != "on" || w == "" {
		t.Fatalf("high should clamp to 'on' on a {off,on} model; got effort=%q where=%q", p.effort, w)
	}
}

// The per-type effort policy: explore cheap, research/code high, judge low.
func TestSubagentEffortPolicy(t *testing.T) {
	cases := []struct {
		typ        llm.SubagentType
		difficulty string
		want       string
	}{
		{llm.TypeExplore, "medium", "low"},
		{llm.TypeResearch, "medium", "high"},
		{llm.TypeCode, "medium", "high"},
		{llm.TypeCode, "hard", "xhigh"}, // hard lifts a notch
		{llm.TypeGeneral, "medium", "medium"},
		{llm.TypeJudge, "hard", "low"}, // judge stays put regardless of difficulty
	}
	for _, c := range cases {
		if got := llm.SubagentEffort(c.typ, c.difficulty); got != c.want {
			t.Errorf("SubagentEffort(%s,%s)=%q want %q", c.typ, c.difficulty, got, c.want)
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

	t.Run("policy still never mutates the shared parent provider", func(t *testing.T) {
		// Under the per-type policy the subtask gets its OWN provider (built fresh)
		// before any effort change — so whatever effort it lands on, the parent's
		// shared instance must remain untouched. (If the fresh build can't be made
		// — no creds — it skips discipline and keeps the shared one unchanged too.)
		shared := &effortProv{id: "us.anthropic.claude-opus-4-8", effort: "max"}
		a := &Agent{Provider: shared, Perm: PermAuto}

		_, _ = a.subAgent(context.Background(), "hard refactor", SubtaskOpts{Difficulty: "hard"})

		if shared.effort != "max" {
			t.Fatalf("shared parent provider effort must never change; got %q", shared.effort)
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

package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// gateProv calls a mutating tool once, then finishes.
type gateProv struct{ step int }

func (p *gateProv) Name() string    { return "gate" }
func (p *gateProv) ModelID() string { return "gate" }
func (p *gateProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	p.step++
	if p.step == 1 {
		return &llm.Response{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "mutate", Arguments: json.RawMessage(`{}`)}}}, nil
	}
	return &llm.Response{Text: "done"}, nil
}

func TestLocalApprovalAsEvent(t *testing.T) {
	// Local backends surface gated tool calls as EventApproval through the
	// event sink — the same shape as daemon sessions — answered by id.
	ran := false
	mut := tool.Definition{
		Name:       "mutate",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { ran = true; return "ok", nil },
	}
	reg, _ := tool.NewRegistry(mut)
	a := &agent.Agent{Provider: &gateProv{}, Tools: reg, Perm: agent.PermGated}
	l := NewLocal(a, nil, "m")

	events := make(chan agent.Event, 32)
	l.Wire(func(e agent.Event) { events <- e }, nil)

	go func() {
		for e := range events {
			if e.Kind == agent.EventApproval {
				if e.ToolName != "mutate" || e.Result == "" {
					t.Errorf("malformed approval event: %+v", e)
				}
				l.Answer(e.Result, true)
				return
			}
		}
	}()

	out, err := l.Send(context.Background(), "go", nil)
	if err != nil || out != "done" {
		t.Fatalf("send: %v %q", err, out)
	}
	if !ran {
		t.Fatal("approved tool should have run")
	}
}

func TestLocalDeniedApproval(t *testing.T) {
	ran := false
	mut := tool.Definition{
		Name:       "mutate",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { ran = true; return "ok", nil },
	}
	reg, _ := tool.NewRegistry(mut)
	a := &agent.Agent{Provider: &gateProv{}, Tools: reg, Perm: agent.PermGated}
	l := NewLocal(a, nil, "m")
	l.Wire(func(e agent.Event) {
		if e.Kind == agent.EventApproval {
			l.Answer(e.Result, false)
		}
	}, nil)
	if _, err := l.Send(context.Background(), "go", nil); err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("denied tool must not run")
	}
}

func TestLocalResetAndState(t *testing.T) {
	reg, _ := tool.NewRegistry()
	a := &agent.Agent{Provider: &gateProv{step: 1}, Tools: reg, Perm: agent.PermAuto}
	l := NewLocal(a, []llm.Message{{Role: llm.RoleUser, Text: "hi"}}, "model-x")
	if len(l.Messages()) != 1 {
		t.Fatal("history should resume")
	}
	l.Reset(nil)
	if len(l.Messages()) != 0 {
		t.Fatal("reset should clear")
	}
	if l.ModelID() != "model-x" {
		t.Fatalf("model id: %q", l.ModelID())
	}
	l.SetPerm(agent.PermGated)
	if l.Perm() != agent.PermGated {
		t.Fatal("perm should round-trip")
	}
	l.SetGoal("ship it")
	if l.Goal() != "ship it" {
		t.Fatal("goal should round-trip")
	}
}

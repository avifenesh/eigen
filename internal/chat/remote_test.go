package chat

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// echoProv answers one line per turn.
type echoProv struct{}

func (echoProv) Name() string { return "echo-model" }
func (echoProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "echo says hi"}, nil
}

func startDaemon(t *testing.T, build daemon.Builder) (*daemon.Client, string) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "d.sock")
	srv, err := daemon.Listen(sock, daemon.NewHost(), build)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Close() })
	c, err := daemon.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	id, err := c.New("/tmp/proj", "echo-model")
	if err != nil {
		t.Fatal(err)
	}
	return c, id
}

func TestRemoteSendAndState(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermGated, MaxContextTokens: 9000}, func() {}, nil
	}
	c, id := startDaemon(t, build)

	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	// State snapshot before any turn.
	if r.ModelID() != "echo-model" || r.Perm() != agent.PermGated || r.MaxContextTokens() != 9000 {
		t.Fatalf("state: model=%q perm=%q max=%d", r.ModelID(), r.Perm(), r.MaxContextTokens())
	}

	var events []agent.EventKind
	r.Wire(func(e agent.Event) { events = append(events, e.Kind) }, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := r.Send(ctx, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "echo says hi" {
		t.Fatalf("final answer: %q", out)
	}
	// History synced after the turn.
	msgs := r.Messages()
	if len(msgs) < 2 {
		t.Fatalf("messages not synced: %d", len(msgs))
	}
	if msgs[0].Role != llm.RoleUser || msgs[0].Text != "hello" {
		t.Fatalf("first message: %+v", msgs[0])
	}

	// Mutations round-trip through the daemon.
	r.SetGoal("finish the thing")
	if r.Goal() != "finish the thing" {
		t.Fatalf("goal: %q", r.Goal())
	}
	r.SetPerm(agent.PermAuto)
	if r.Perm() != agent.PermAuto {
		t.Fatalf("perm: %q", r.Perm())
	}
}

// gateRemoteProv triggers one gated tool call then finishes.
type gateRemoteProv struct{ step int }

func (p *gateRemoteProv) Name() string { return "gate" }
func (p *gateRemoteProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	p.step++
	if p.step == 1 {
		return &llm.Response{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "mutate", Arguments: json.RawMessage(`{}`)}}}, nil
	}
	return &llm.Response{Text: "all done"}, nil
}

func TestRemoteApprovalRoundTrip(t *testing.T) {
	ran := false
	mut := tool.Definition{
		Name:       "mutate",
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { ran = true; return "ok", nil },
	}
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry(mut)
		return &agent.Agent{Provider: &gateRemoteProv{}, Tools: reg, Perm: agent.PermGated}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	// Approvals arrive as EventApproval; answer through the backend like the
	// TUI does.
	r.Wire(func(e agent.Event) {
		if e.Kind == agent.EventApproval {
			r.Answer(e.Result, true)
		}
	}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := r.Send(ctx, "do it", nil)
	if err != nil || out != "all done" {
		t.Fatalf("send: %v %q", err, out)
	}
	if !ran {
		t.Fatal("approved tool should have run in the daemon")
	}
}

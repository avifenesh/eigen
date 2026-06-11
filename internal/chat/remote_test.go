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

func (echoProv) Name() string    { return "echo-model" }
func (echoProv) ModelID() string { return "echo-model" }
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

func (p *gateRemoteProv) Name() string    { return "gate" }
func (p *gateRemoteProv) ModelID() string { return "gate" }
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

func TestRemoteClearAndResend(t *testing.T) {
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: echoProv{}, Tools: reg, Perm: agent.PermAuto, MaxContextTokens: 9000}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	r.Wire(func(agent.Event) {}, nil)
	ctx := context.Background()

	if _, err := r.Send(ctx, "first", nil); err != nil {
		t.Fatal(err)
	}
	if len(r.Messages()) < 2 {
		t.Fatal("turn should add messages")
	}
	// /clear empties the daemon session.
	r.Reset(nil)
	if len(r.Messages()) != 0 {
		t.Fatalf("clear should empty history, got %d", len(r.Messages()))
	}
	// /resend after a fresh turn retries the last user message.
	if _, err := r.Send(ctx, "again", nil); err != nil {
		t.Fatal(err)
	}
	n := len(r.Messages())
	if _, err := r.Resend(ctx); err != nil {
		t.Fatal(err)
	}
	if len(r.Messages()) <= n-2 {
		t.Fatalf("resend should re-run the last turn, msgs=%d (was %d)", len(r.Messages()), n)
	}
}

// namedProv reports a fixed name (used to verify model switching).
type namedProv struct{ name string }

func (p namedProv) Name() string    { return p.name + " (test backend)" } // decorated, like real providers
func (p namedProv) ModelID() string { return p.name }
func (p namedProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "ok"}, nil
}

func TestRemoteModelSwitch(t *testing.T) {
	reg, _ := tool.NewRegistry()
	build := func(_, _ string) (*agent.Agent, func(), error) {
		return &agent.Agent{Provider: namedProv{"model-a"}, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	sock := filepath.Join(t.TempDir(), "d.sock")
	host := daemon.NewHost()
	// The daemon rebuilds the provider for a switch — name it after the id.
	var gotSwitchID string
	host.SetModelSwitcher(func(_, modelID string) (llm.Provider, llm.Compactor, int, error) {
		gotSwitchID = modelID // must be the clean id, not "model-b (test backend)"
		return namedProv{modelID}, nil, 0, nil
	})
	srv, err := daemon.Listen(sock, host, build)
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
	id, _ := c.New("/tmp", "model-a")

	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.ModelID() != "model-a" {
		t.Fatalf("initial model: %q", r.ModelID())
	}
	r.SetModel(namedProv{"model-b"}, nil, 0)
	if gotSwitchID != "model-b" {
		t.Fatalf("daemon got switch id %q, want clean \"model-b\" (Remote.SetModel must send ModelID, not Name)", gotSwitchID)
	}
	if r.ModelID() != "model-b" {
		t.Fatalf("after switch: %q", r.ModelID())
	}
}

// effortProv supports reasoning effort (used to verify effort over the socket).
type effortProv struct{ effort string }

func (p *effortProv) Name() string    { return "effort-model" }
func (p *effortProv) ModelID() string { return "effort-model" }
func (p *effortProv) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "ok"}, nil
}
func (p *effortProv) SetEffort(l string) bool { p.effort = l; return l == "low" || l == "high" }
func (p *effortProv) Effort() string          { return p.effort }

func TestRemoteEffortOverSocket(t *testing.T) {
	prov := &effortProv{effort: "high"}
	build := func(_, _ string) (*agent.Agent, func(), error) {
		reg, _ := tool.NewRegistry()
		return &agent.Agent{Provider: prov, Tools: reg, Perm: agent.PermAuto}, func() {}, nil
	}
	c, id := startDaemon(t, build)
	r, err := NewRemote(c, id)
	if err != nil {
		t.Fatal(err)
	}
	// Effort travels in the state snapshot — the status bar shows it without
	// a provider handle.
	if r.Effort() != "high" {
		t.Fatalf("effort = %q, want high", r.Effort())
	}
	if !r.SetEffort("low") {
		t.Fatal("SetEffort(low) should succeed")
	}
	if r.Effort() != "low" {
		t.Fatalf("after set: %q", r.Effort())
	}
	if r.SetEffort("bogus") {
		t.Fatal("SetEffort(bogus) should fail")
	}
	// A provider with no search setting reports "" (segment hidden).
	if r.SearchMode() != "" {
		t.Fatalf("search should be unsupported, got %q", r.SearchMode())
	}
}

package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// safeProvider is a concurrency-safe provider for parallel fan-out tests: it
// returns the same canned final answer for every turn, guarded by a mutex.
type safeProvider struct {
	mu    sync.Mutex
	reply string
	calls int
}

func (s *safeProvider) Name() string    { return "safe" }
func (s *safeProvider) ModelID() string { return "safe" }
func (s *safeProvider) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return &llm.Response{Text: s.reply}, nil
}

func roleTestAgent(t *testing.T) *Agent {
	t.Helper()
	reg, err := tool.NewRegistry(
		roTool("read"), roTool("grep"), roTool("glob"), roTool("list"),
		roTool("tree"), roTool("symbols"), roTool("diff"), roTool("websearch"),
		roTool("fetch"), roTool("review"), roTool("skill"),
		mutTool("write"), mutTool("bash"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &Agent{Provider: &safeProvider{reply: "child done"}, Tools: reg, Perm: PermAuto}
}

func roTool(name string) tool.Definition {
	return tool.Definition{
		Name: name, ReadOnly: true,
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
	}
}

func mutTool(name string) tool.Definition {
	return tool.Definition{
		Name: name, ReadOnly: false,
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
	}
}

func TestRoleSubsetIsReadOnly(t *testing.T) {
	a := roleTestAgent(t)
	for _, name := range RoleNames() {
		role, ok := LookupRole(name)
		if !ok {
			t.Fatalf("built-in role %q should exist", name)
		}
		sub := a.Tools.Subset(role.Tools...)
		if !sub.AllReadOnly() {
			t.Fatalf("role %q toolset must be entirely read-only", name)
		}
	}
	if _, ok := a.Tools.Get("write"); !ok {
		t.Fatal("Subset must not remove tools from the parent registry")
	}
}

func TestTaskGroupRunsParallelReadOnly(t *testing.T) {
	a := roleTestAgent(t)
	out, err := a.TaskGroup(context.Background(), []GroupSubtask{
		{Task: "investigate A", Role: "researcher"},
		{Task: "review B", Role: "reviewer"},
		{Task: "summarize C", Role: "summarizer"},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "3 subtasks, 3 succeeded") {
		t.Fatalf("report header wrong:\n%s", out)
	}
	// Stable input order: [1] researcher, [2] reviewer, [3] summarizer.
	i1 := strings.Index(out, "[1] researcher")
	i2 := strings.Index(out, "[2] reviewer")
	i3 := strings.Index(out, "[3] summarizer")
	if i1 < 0 || i2 < 0 || i3 < 0 || !(i1 < i2 && i2 < i3) {
		t.Fatalf("children should appear in stable input order:\n%s", out)
	}
}

func TestTaskGroupRejectsUnknownRole(t *testing.T) {
	a := roleTestAgent(t)
	_, err := a.TaskGroup(context.Background(), []GroupSubtask{
		{Task: "x", Role: "implementer"}, // not a built-in role
	}, 1)
	if err == nil || !strings.Contains(err.Error(), "unknown role") {
		t.Fatalf("unknown role should fail closed, got %v", err)
	}
}

func TestTaskGroupRequiresRole(t *testing.T) {
	a := roleTestAgent(t)
	_, err := a.TaskGroup(context.Background(), []GroupSubtask{
		{Task: "x"}, // no role
	}, 1)
	if err == nil || !strings.Contains(err.Error(), "needs a role") {
		t.Fatalf("missing role should fail closed, got %v", err)
	}
}

func TestTaskGroupCapsChildren(t *testing.T) {
	a := roleTestAgent(t)
	var subs []GroupSubtask
	for i := 0; i < maxGroupChildren+1; i++ {
		subs = append(subs, GroupSubtask{Task: "x", Role: "researcher"})
	}
	if _, err := a.TaskGroup(context.Background(), subs, 3); err == nil {
		t.Fatal("too many children should error")
	}
}

func TestTaskGroupEmptyErrors(t *testing.T) {
	a := roleTestAgent(t)
	if _, err := a.TaskGroup(context.Background(), nil, 3); err == nil {
		t.Fatal("empty group should error")
	}
}

// readToolProvider drives a child to call one read-only tool then finish — to
// prove a read-only fan-out child never reaches the approval path.
type readToolProvider struct{ tool string }

func (p *readToolProvider) Name() string    { return "rp" }
func (p *readToolProvider) ModelID() string { return "rp" }
func (p *readToolProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool {
			return &llm.Response{Text: "done"}, nil
		}
	}
	return &llm.Response{ToolCalls: []llm.ToolCall{{ID: "t", Name: p.tool, Arguments: json.RawMessage(`{}`)}}}, nil
}

// TestReadOnlyFanoutNeverApproves is the SAFETY invariant: even when the parent
// is GATED, read-only children must never invoke Approve (which in a single
// window would race across N concurrent children). Read-only tools auto-run, so
// the approval path is never reached.
func TestReadOnlyFanoutNeverApproves(t *testing.T) {
	reg, err := tool.NewRegistry(roTool("read"), roTool("grep"), roTool("glob"),
		roTool("list"), roTool("tree"), roTool("symbols"), roTool("diff"), roTool("review"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{
		Provider: &readToolProvider{tool: "read"},
		Tools:    reg,
		Perm:     PermGated, // gated parent
		Approve: func(context.Context, string, json.RawMessage) (bool, error) {
			t.Fatal("a read-only fan-out child must NEVER call Approve")
			return false, nil
		},
	}
	if _, err := a.TaskGroup(context.Background(), []GroupSubtask{
		{Task: "investigate", Role: "researcher"},
		{Task: "critique", Role: "reviewer"},
	}, 2); err != nil {
		t.Fatalf("read-only fan-out should run under a gated parent: %v", err)
	}
}

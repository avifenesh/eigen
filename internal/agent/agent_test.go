package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
)

// mockProvider returns canned responses in order.
type mockProvider struct {
	replies []*llm.Response
	i       int
	seen    []llm.Request
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	m.seen = append(m.seen, req)
	r := m.replies[m.i]
	m.i++
	return r, nil
}

func callTool(name string) tool.Definition {
	return tool.Definition{
		Name:       name,
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { return "ok-" + name, nil },
	}
}

func TestRunExecutesToolThenFinishes(t *testing.T) {
	ran := false
	td := callTool("ping")
	td.ReadOnly = true
	td.Run = func(context.Context, json.RawMessage) (string, error) { ran = true; return "pong", nil }

	prov := &mockProvider{replies: []*llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "ping", Arguments: json.RawMessage(`{}`)}}},
		{Text: "done"},
	}}
	a := &Agent{Provider: prov, Tools: tool.NewRegistry(td), Perm: PermAuto}

	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Fatal("tool did not run")
	}
	if out != "done" {
		t.Fatalf("got %q, want %q", out, "done")
	}
	// Second request must carry the tool result back to the model.
	last := prov.seen[1]
	if len(last.Messages) != 3 || last.Messages[2].Role != llm.RoleTool || last.Messages[2].Text != "pong" {
		t.Fatalf("tool result not fed back correctly: %+v", last.Messages)
	}
}

func TestGatedDeniesMutatingWithoutApprover(t *testing.T) {
	ran := false
	td := callTool("mutate") // ReadOnly defaults false
	td.Run = func(context.Context, json.RawMessage) (string, error) { ran = true; return "x", nil }

	prov := &mockProvider{replies: []*llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "mutate", Arguments: json.RawMessage(`{}`)}}},
		{Text: "end"},
	}}
	a := &Agent{Provider: prov, Tools: tool.NewRegistry(td), Perm: PermGated} // Approve == nil

	out, err := a.Run(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("mutating tool ran in gated mode without approval")
	}
	if out != "end" {
		t.Fatalf("got %q, want %q", out, "end")
	}
}

func TestRunStopsAtMaxSteps(t *testing.T) {
	loop := &llm.Response{ToolCalls: []llm.ToolCall{{ID: "c", Name: "ping", Arguments: json.RawMessage(`{}`)}}}
	prov := &mockProvider{replies: []*llm.Response{loop, loop, loop}}
	a := &Agent{Provider: prov, Tools: tool.NewRegistry(callTool("ping")), Perm: PermAuto, MaxSteps: 2}

	if _, err := a.Run(context.Background(), "t"); err == nil {
		t.Fatal("expected MaxSteps error")
	}
}

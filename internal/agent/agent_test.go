package agent

import (
	"context"
	"encoding/json"
	"strings"
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
	reg, err := tool.NewRegistry(td)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}

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
	reg, err := tool.NewRegistry(td)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermGated} // Approve == nil

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
	reg, err := tool.NewRegistry(callTool("ping"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto, MaxSteps: 2}

	if _, err := a.Run(context.Background(), "t"); err == nil {
		t.Fatal("expected MaxSteps error")
	}
}

func TestRunNudgesPastEmptyTurn(t *testing.T) {
	// First turn is empty (reasoning-only); loop must nudge, not exit empty.
	prov := &mockProvider{replies: []*llm.Response{
		{}, // no tool calls, no text
		{Text: "final"},
	}}
	reg, err := tool.NewRegistry(callTool("ping"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}

	out, err := a.Run(context.Background(), "t")
	if err != nil {
		t.Fatal(err)
	}
	if out != "final" {
		t.Fatalf("got %q, want %q", out, "final")
	}
}

func TestRunErrorsOnPersistentEmptyTurns(t *testing.T) {
	prov := &mockProvider{replies: []*llm.Response{{}, {}, {}, {}}}
	reg, err := tool.NewRegistry(callTool("ping"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}

	if _, err := a.Run(context.Background(), "t"); err == nil {
		t.Fatal("expected error after persistent empty turns")
	}
}

func TestRunEmitsEventSequence(t *testing.T) {
	td := callTool("ping")
	td.ReadOnly = true
	prov := &mockProvider{replies: []*llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "ping", Arguments: json.RawMessage(`{}`)}}},
		{Text: "all done"},
	}}
	reg, err := tool.NewRegistry(td)
	if err != nil {
		t.Fatal(err)
	}

	var kinds []EventKind
	a := &Agent{
		Provider: prov,
		Tools:    reg,
		Perm:     PermAuto,
		OnEvent:  func(e Event) { kinds = append(kinds, e.Kind) },
	}
	out, err := a.Run(context.Background(), "task")
	if err != nil {
		t.Fatal(err)
	}
	if out != "all done" {
		t.Fatalf("got %q", out)
	}
	want := []EventKind{EventToolStart, EventToolResult, EventDone}
	if len(kinds) != len(want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("event %d = %v, want %v (all: %v)", i, kinds[i], want[i], kinds)
		}
	}
}

func TestSessionPreservesHistory(t *testing.T) {
	prov := &mockProvider{replies: []*llm.Response{
		{Text: "first answer"},
		{Text: "second answer"},
	}}
	reg, err := tool.NewRegistry(callTool("ping"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}
	s := a.NewSession()

	if _, err := s.Send(context.Background(), "first task"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Send(context.Background(), "second task"); err != nil {
		t.Fatal(err)
	}

	// The second request must carry the full prior conversation:
	// user(first task), assistant(first answer), user(second task).
	second := prov.seen[1]
	if len(second.Messages) != 3 {
		t.Fatalf("second turn should see 3 prior messages, got %d: %+v", len(second.Messages), second.Messages)
	}
	if second.Messages[0].Text != "first task" || second.Messages[1].Text != "first answer" || second.Messages[2].Text != "second task" {
		t.Fatalf("history not preserved: %+v", second.Messages)
	}
}

func TestSubtaskRunsFreshSession(t *testing.T) {
	prov := &mockProvider{replies: []*llm.Response{{Text: "subtask answer"}}}
	reg, _ := tool.NewRegistry(callTool("noop"))
	events := 0
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto, OnEvent: func(Event) { events++ }}

	out, err := a.Subtask(context.Background(), "do a thing")
	if err != nil {
		t.Fatal(err)
	}
	if out != "subtask answer" {
		t.Fatalf("got %q", out)
	}
	if events != 0 {
		t.Fatal("subtask events should be suppressed (not emitted to the caller)")
	}
}

func TestSubtaskDepthLimit(t *testing.T) {
	prov := &mockProvider{replies: []*llm.Response{{Text: "x"}}}
	reg, _ := tool.NewRegistry(callTool("noop"))
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}

	ctx := context.WithValue(context.Background(), subtaskDepthKey{}, maxSubtaskDepth)
	if _, err := a.Subtask(ctx, "too deep"); err == nil {
		t.Fatal("subtask at the depth limit should be refused")
	}
}

func TestToolPanicIsRecovered(t *testing.T) {
	boom := tool.Definition{
		Name:       "boom",
		ReadOnly:   true,
		Parameters: json.RawMessage(`{"type":"object"}`),
		Run:        func(context.Context, json.RawMessage) (string, error) { panic("kaboom") },
	}
	prov := &mockProvider{replies: []*llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "boom", Arguments: json.RawMessage(`{}`)}}},
		{Text: "recovered and finished"},
	}}
	reg, _ := tool.NewRegistry(boom)
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}

	out, err := a.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("a panicking tool must not fail the run: %v", err)
	}
	if out != "recovered and finished" {
		t.Fatalf("loop should continue after a tool panic, got %q", out)
	}
	// The tool result fed back to the model must mention the panic.
	last := prov.seen[len(prov.seen)-1]
	var sawPanic bool
	for _, msg := range last.Messages {
		if msg.Role == llm.RoleTool && strings.Contains(msg.Text, "panicked") {
			sawPanic = true
		}
	}
	if !sawPanic {
		t.Fatal("the panic should be surfaced to the model as a tool error")
	}
}

func TestPersistCalledPerMessage(t *testing.T) {
	td := callTool("ping")
	td.ReadOnly = true
	prov := &mockProvider{replies: []*llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "ping", Arguments: json.RawMessage(`{}`)}}},
		{Text: "done"},
	}}
	reg, _ := tool.NewRegistry(td)

	var snapshots [][]llm.Message
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto,
		Persist: func(msgs []llm.Message) {
			cp := make([]llm.Message, len(msgs))
			copy(cp, msgs)
			snapshots = append(snapshots, cp)
		},
	}
	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	// Expect persists after: user msg, after the tool round, and final answer.
	if len(snapshots) < 3 {
		t.Fatalf("expected continuous persists, got %d", len(snapshots))
	}
	// First persist holds at least the user message.
	if len(snapshots[0]) == 0 || snapshots[0][0].Role != llm.RoleUser {
		t.Fatal("first persist should include the user message")
	}
	// Last persist holds the final assistant answer.
	last := snapshots[len(snapshots)-1]
	if last[len(last)-1].Role != llm.RoleAssistant || last[len(last)-1].Text != "done" {
		t.Fatal("final persist should include the assistant answer")
	}
}

// loopProvider always returns the same tool call, so the loop never terminates
// on its own — used to exercise the optional MaxSteps runaway cap.
type loopProvider struct{ calls int }

func (p *loopProvider) Name() string { return "loop" }
func (p *loopProvider) Complete(context.Context, llm.Request) (*llm.Response, error) {
	p.calls++
	return &llm.Response{ToolCalls: []llm.ToolCall{
		{ID: "c", Name: "noop", Arguments: json.RawMessage(`{}`)},
	}}, nil
}

func TestMaxStepsCapsRunawayLoop(t *testing.T) {
	td := callTool("noop")
	td.ReadOnly = true
	reg, _ := tool.NewRegistry(td)

	// An explicit MaxSteps bounds a non-terminating loop and returns the error.
	prov := &loopProvider{}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto, MaxSteps: 5}
	if _, err := a.Run(context.Background(), "spin"); err == nil {
		t.Fatal("a non-terminating loop should hit MaxSteps")
	}
	if prov.calls != 5 {
		t.Fatalf("MaxSteps=5 should allow exactly 5 model calls, got %d", prov.calls)
	}
}

func TestUnlimitedByDefaultStopsOnAnswer(t *testing.T) {
	td := callTool("noop")
	td.ReadOnly = true
	reg, _ := tool.NewRegistry(td)

	// With no MaxSteps (unlimited), the loop still terminates the moment the
	// model returns a final answer — well past the old hardcoded cap of 20.
	var replies []*llm.Response
	for i := 0; i < 30; i++ {
		replies = append(replies, &llm.Response{ToolCalls: []llm.ToolCall{
			{ID: "c", Name: "noop", Arguments: json.RawMessage(`{}`)},
		}})
	}
	replies = append(replies, &llm.Response{Text: "finally done"})
	prov := &mockProvider{replies: replies}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto} // MaxSteps == 0 → unlimited

	out, err := a.Run(context.Background(), "long task")
	if err != nil {
		t.Fatalf("unlimited loop should not error before the final answer: %v", err)
	}
	if out != "finally done" {
		t.Fatalf("got %q, want final answer after 30 tool steps", out)
	}
}

func TestCanceledContextStopsLoop(t *testing.T) {
	td := callTool("noop")
	td.ReadOnly = true
	reg, _ := tool.NewRegistry(td)
	prov := &loopProvider{}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto} // unlimited

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled: the loop must stop at the first guard
	if _, err := a.Run(ctx, "spin"); err == nil {
		t.Fatal("a canceled context should stop the loop")
	}
}

// fakeCompactor replaces older history with a fixed short summary.
type fakeCompactor struct{ calls int }

func (f *fakeCompactor) Summarize(_ context.Context, _ []llm.Message) (string, error) {
	f.calls++
	return "SUMMARY", nil
}

func TestSessionCompactShrinksAndPersists(t *testing.T) {
	prov := &mockProvider{}
	reg, _ := tool.NewRegistry()
	fc := &fakeCompactor{}
	var persisted int
	a := &Agent{
		Provider:         prov,
		Tools:            reg,
		Perm:             PermAuto,
		Compactor:        fc,
		MaxContextTokens: 20000,
		Persist:          func(m []llm.Message) { persisted = len(m) },
	}
	// Build a long multi-round conversation so there is older history to fold.
	var msgs []llm.Message
	big := strings.Repeat("word ", 800) // ~1k tokens per message
	for i := 0; i < 12; i++ {
		msgs = append(msgs,
			llm.Message{Role: llm.RoleUser, Text: "do step " + big},
			llm.Message{Role: llm.RoleAssistant, Text: "did it " + big},
		)
	}
	s := a.Resume(msgs)

	before, after, err := s.Compact(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if fc.calls == 0 {
		t.Fatal("compact should have invoked the summarizer")
	}
	if after >= before {
		t.Fatalf("compact should reduce message count: %d → %d", before, after)
	}
	if persisted != after {
		t.Fatalf("compact should persist the compacted messages: persisted=%d after=%d", persisted, after)
	}
	// The summary text is present in the compacted history.
	found := false
	for _, m := range s.Messages() {
		if strings.Contains(m.Text, "SUMMARY") {
			found = true
		}
	}
	if !found {
		t.Fatal("compacted history should contain the summary")
	}
}

func TestSessionCompactEmpty(t *testing.T) {
	prov := &mockProvider{}
	reg, _ := tool.NewRegistry()
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto, Compactor: &fakeCompactor{}}
	s := a.NewSession()
	before, after, err := s.Compact(context.Background(), 0)
	if err != nil || before != 0 || after != 0 {
		t.Fatalf("compacting an empty session should be a no-op, got before=%d after=%d err=%v", before, after, err)
	}
}

func TestResendContinuesWithoutNewUserMessage(t *testing.T) {
	prov := &mockProvider{replies: []*llm.Response{{Text: "recovered"}}}
	reg, _ := tool.NewRegistry()
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}
	// History as left by a failed turn: the user message is already there.
	s := a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "do the thing"}})

	out, err := s.Resend(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out != "recovered" {
		t.Fatalf("resend answer = %q", out)
	}
	// No duplicate user message was appended.
	users := 0
	for _, m := range s.Messages() {
		if m.Role == llm.RoleUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("resend must not append a new user message, got %d user messages", users)
	}
}

func TestResendEmptySessionErrors(t *testing.T) {
	reg, _ := tool.NewRegistry()
	a := &Agent{Provider: &mockProvider{}, Tools: reg, Perm: PermAuto}
	if _, err := a.NewSession().Resend(context.Background()); err == nil {
		t.Fatal("resend on an empty session should error")
	}
}

// noShrinkCompactor returns a summary but the resulting context (as built by
// CompactWith) still doesn't drop much, simulating an ineffective compaction.
type noShrinkCompactor struct{ calls int }

func (n *noShrinkCompactor) Summarize(_ context.Context, _ []llm.Message) (string, error) {
	n.calls++
	return "SUMMARY", nil
}

func TestMaybeCompactCircuitBreakerTrips(t *testing.T) {
	cc := &noShrinkCompactor{}
	var notes []string
	a := &Agent{
		Provider:         &mockProvider{},
		Tools:            mustReg(t),
		Perm:             PermAuto,
		Compactor:        cc,
		MaxContextTokens: 8000,
		OnEvent: func(e Event) {
			if e.Kind == EventNote {
				notes = append(notes, e.Text)
			}
		},
	}
	// Over budget, and the most-recent round alone is ~7.4k tokens (close to the
	// 8k budget) and cannot be shed (it's user/assistant text), so compaction
	// must keep it verbatim and lands with little headroom.
	huge := strings.Repeat("word ", 3700) // ~3.7k tokens each
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: strings.Repeat("old ", 3000)},
		{Role: llm.RoleAssistant, Text: strings.Repeat("old ", 3000)},
		{Role: llm.RoleUser, Text: huge},
		{Role: llm.RoleAssistant, Text: huge},
	}
	s := a.Resume(msgs)

	// First pass: compaction runs but leaves little headroom (recent round big).
	s.maybeCompact(context.Background())
	firstCalls := cc.calls
	if firstCalls == 0 {
		t.Fatal("first over-budget pass should attempt compaction")
	}

	// Second pass: still over budget after low-headroom compaction → breaker trips.
	s.maybeCompact(context.Background())
	if !s.compactStall {
		t.Fatalf("breaker should trip after a low-headroom compaction (after=%d budget=%d)", s.lastCompactAfter, a.MaxContextTokens)
	}
	if len(notes) == 0 {
		t.Fatal("breaker should emit a note")
	}

	// Third pass: breaker stays tripped; no further summary calls.
	s.maybeCompact(context.Background())
	if cc.calls > firstCalls {
		t.Fatalf("breaker tripped: no further summary calls expected, got %d (was %d)", cc.calls, firstCalls)
	}
}

func TestMaybeCompactResetsUnderBudget(t *testing.T) {
	a := &Agent{
		Provider:         &mockProvider{},
		Tools:            mustReg(t),
		Perm:             PermAuto,
		Compactor:        &fakeCompactor{},
		MaxContextTokens: 100000,
	}
	s := a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "small"}})
	s.compactStall = true // pretend it was tripped earlier
	s.maybeCompact(context.Background())
	if s.compactStall {
		t.Fatal("breaker should reset when context is back under budget")
	}
}

func mustReg(t *testing.T) *tool.Registry {
	t.Helper()
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

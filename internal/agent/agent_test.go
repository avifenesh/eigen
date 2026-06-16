package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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

func (m *mockProvider) ModelID() string { return "mock" }
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

	out, err := a.Subtask(context.Background(), "do a thing", "", "")
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
	if _, err := a.Subtask(ctx, "too deep", "", ""); err == nil {
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

func (p *loopProvider) Name() string    { return "loop" }
func (p *loopProvider) ModelID() string { return "loop" }
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

// overflowOnceProvider returns a context-overflow error on its first call, then
// a normal final answer — to verify error-driven compaction retries the step.
type overflowOnceProvider struct {
	calls   int
	lastReq llm.Request
}

func (p *overflowOnceProvider) Name() string { return "overflow-once" }

func (p *overflowOnceProvider) ModelID() string { return "overflow-once" }
func (p *overflowOnceProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	p.calls++
	p.lastReq = req
	if p.calls == 1 {
		return nil, errors.New("prompt is too long: 300000 tokens > 200000 maximum")
	}
	return &llm.Response{Text: "done"}, nil
}

func TestErrorDrivenCompactionRetriesOnOverflow(t *testing.T) {
	prov := &overflowOnceProvider{}
	var notes []string
	a := &Agent{
		Provider:  prov,
		Tools:     mustReg(t),
		Perm:      PermAuto,
		Compactor: &fakeCompactor{},
		OnEvent: func(e Event) {
			if e.Kind == EventNote {
				notes = append(notes, e.Text)
			}
		},
	}
	// A multi-round history so there's something to fold on overflow.
	var msgs []llm.Message
	big := strings.Repeat("word ", 1000)
	for i := 0; i < 8; i++ {
		msgs = append(msgs,
			llm.Message{Role: llm.RoleUser, Text: big},
			llm.Message{Role: llm.RoleAssistant, Text: big},
		)
	}
	s := a.Resume(msgs)
	out, err := s.Send(context.Background(), "continue")
	if err != nil {
		t.Fatalf("expected recovery after overflow, got error: %v", err)
	}
	if out != "done" {
		t.Fatalf("expected final answer 'done', got %q", out)
	}
	if prov.calls != 2 {
		t.Fatalf("expected provider called twice (overflow then retry), got %d", prov.calls)
	}
	if len(notes) == 0 {
		t.Fatal("expected an EventNote about compacting on overflow")
	}
}

func TestErrorDrivenCompactionGivesUpWhenNothingToFold(t *testing.T) {
	// A provider that always overflows; with a tiny history compaction can't
	// shrink, so the loop must surface the error rather than spin forever.
	prov := &alwaysOverflowProvider{}
	a := &Agent{Provider: prov, Tools: mustReg(t), Perm: PermAuto, Compactor: &fakeCompactor{}}
	s := a.NewSession()
	_, err := s.Send(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected the overflow error to surface when nothing can be folded")
	}
	if !llm.IsContextOverflow(err) {
		t.Fatalf("expected a context-overflow error, got %v", err)
	}
}

type alwaysOverflowProvider struct{}

func (p *alwaysOverflowProvider) Name() string    { return "always-overflow" }
func (p *alwaysOverflowProvider) ModelID() string { return "always-overflow" }
func (p *alwaysOverflowProvider) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, errors.New("prompt is too long")
}

func TestGoalInjectedIntoSystemPerStep(t *testing.T) {
	cap := &systemCapturingProvider{}
	a := &Agent{Provider: cap, Tools: mustReg(t), Perm: PermAuto}
	a.SetGoal("ship the parser rewrite")
	s := a.NewSession()
	if _, err := s.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cap.system, "CURRENT GOAL") || !strings.Contains(cap.system, "ship the parser rewrite") {
		t.Fatalf("goal should be injected into the system prompt:\n%s", cap.system)
	}
	// Clearing the goal removes it next turn.
	a.SetGoal("")
	if _, err := s.Send(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cap.system, "CURRENT GOAL") {
		t.Fatal("cleared goal must not appear in the system prompt")
	}
}

type systemCapturingProvider struct{ system string }

func (p *systemCapturingProvider) Name() string    { return "syscap" }
func (p *systemCapturingProvider) ModelID() string { return "syscap" }
func (p *systemCapturingProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	p.system = req.System
	return &llm.Response{Text: "ok"}, nil
}

func TestNonStreamingEmitsInBetweenCommentary(t *testing.T) {
	// A NON-streaming provider (mockProvider has no Stream method) whose
	// tool-call step carries text + reasoning must emit them as delta events,
	// so the live view matches what resume renders from history.
	td := callTool("ping")
	td.ReadOnly = true
	prov := &mockProvider{replies: []*llm.Response{
		{
			Text:      "let me check the file",
			Reasoning: "I should look first",
			ToolCalls: []llm.ToolCall{{ID: "c1", Name: "ping", Arguments: json.RawMessage(`{}`)}},
		},
		{Text: "final answer"},
	}}
	reg, _ := tool.NewRegistry(td)
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}
	var kinds []EventKind
	var texts []string
	a.OnEvent = func(e Event) {
		kinds = append(kinds, e.Kind)
		texts = append(texts, e.Text)
	}
	out, err := a.NewSession().Send(context.Background(), "go")
	if err != nil || out != "final answer" {
		t.Fatalf("send: %v %q", err, out)
	}
	// Expect reasoning + text deltas BEFORE the tool start.
	var sawReasoning, sawText bool
	for i, k := range kinds {
		if k == EventToolStart {
			break
		}
		if k == EventReasoningDelta && texts[i] == "I should look first" {
			sawReasoning = true
		}
		if k == EventTextDelta && texts[i] == "let me check the file" {
			sawText = true
		}
	}
	if !sawReasoning || !sawText {
		t.Fatalf("in-between commentary not emitted before tool start: kinds=%v", kinds)
	}
}

func TestCompactExplicitTargetShrinksTokensViaShedding(t *testing.T) {
	// A conversation full of large TOOL RESULTS, under the message-count radar:
	// compaction sheds the old tool-result payloads (shrinking TOKENS) without
	// necessarily removing messages. The bug was reporting "nothing to compact"
	// because only the message COUNT was checked.
	reg, _ := tool.NewRegistry()
	a := &Agent{Provider: &mockProvider{}, Tools: reg, Perm: PermAuto}
	big := strings.Repeat("x ", 3000) // ~3k tokens of tool output
	var msgs []llm.Message
	for i := 0; i < 12; i++ {
		msgs = append(msgs,
			llm.Message{Role: llm.RoleUser, Text: "go"},
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c", Name: "bash"}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "c", Text: big},
		)
	}
	s := a.Resume(msgs)
	beforeTok := s.Tokens()
	// Explicit aggressive target (like manual /compact halving the live size).
	_, _, err := s.Compact(context.Background(), beforeTok*45/100)
	if err != nil {
		t.Fatal(err)
	}
	afterTok := s.Tokens()
	if afterTok >= beforeTok {
		t.Fatalf("explicit compact should shrink tokens: %d → %d", beforeTok, afterTok)
	}
}

// slowStreamProvider emits a few tool calls then a final answer, sleeping
// between steps so a concurrent reader has a wide window to race the writer.
type slowStreamProvider struct{ step int }

func (p *slowStreamProvider) Name() string    { return "slow" }
func (p *slowStreamProvider) ModelID() string { return "slow" }
func (p *slowStreamProvider) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	time.Sleep(2 * time.Millisecond)
	p.step++
	if p.step < 6 {
		return &llm.Response{ToolCalls: []llm.ToolCall{
			{ID: "c", Name: "noop", Arguments: json.RawMessage(`{}`)},
		}}, nil
	}
	return &llm.Response{Text: "done"}, nil
}

// TestSessionMessagesConcurrentWithTurn pins the daemon contract: state
// snapshots (Messages/Tokens) can be read on a different goroutine while a
// turn mutates history. Run with -race.
func TestSessionMessagesConcurrentWithTurn(t *testing.T) {
	td := callTool("noop")
	td.ReadOnly = true
	reg, _ := tool.NewRegistry(td)
	a := &Agent{Provider: &slowStreamProvider{}, Tools: reg, Perm: PermAuto}
	s := a.NewSession()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.Send(context.Background(), "go")
	}()

	// Hammer the read paths while the turn runs.
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				msgs := s.Messages()
				for _, m := range msgs { // iterate the copy — must be stable
					_ = m.Text
				}
				_ = s.Tokens()
			}
		}
	}()

	<-done
	close(stop)
	if got := len(s.Messages()); got == 0 {
		t.Fatal("turn should have produced messages")
	}
}

// hangingProvider blocks until ctx is canceled — simulating a stalled LLM call
// (e.g. a vision read the gateway never answers).
type hangingProvider struct{}

func (hangingProvider) Name() string    { return "hang" }
func (hangingProvider) ModelID() string { return "hang" }
func (hangingProvider) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestForegroundSubtaskStallsWhenIdle proves a hung foreground subtask (no tool
// activity) is killed by the idle-stall watchdog instead of hanging forever.
func TestForegroundSubtaskStallsWhenIdle(t *testing.T) {
	oldIdle, oldGrace, oldFront := stallIdle, heartbeatGrace, frontWindow
	stallIdle = 120 * time.Millisecond
	heartbeatGrace = 0
	frontWindow = 10 * time.Second // long, so promotion doesn't pre-empt the stall
	defer func() { stallIdle, heartbeatGrace, frontWindow = oldIdle, oldGrace, oldFront }()

	a := &Agent{Provider: hangingProvider{}, Tools: mustReg(t), MaxSteps: 3}
	start := time.Now()
	_, err := a.SubtaskWith(context.Background(), "do a thing", SubtaskOpts{})
	if err == nil {
		t.Fatal("a hung (idle) subtask should return an error, not hang")
	}
	if !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("want a stall error, got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("subtask should abort near its %s idle window, took %v", stallIdle, d)
	}
}

func TestSteerInjectsBetweenToolRounds(t *testing.T) {
	// A 3-step turn: round 1 calls a tool, round 2 calls a tool, round 3 done.
	// During round 1's tool, we Steer a message — it must appear as a user
	// message in round 2's request (between rounds), NOT at end-of-turn.
	var sess *Session
	steerTool := callTool("work")
	steerTool.ReadOnly = true
	steered := false
	steerTool.Run = func(context.Context, json.RawMessage) (string, error) {
		if !steered { // steer once, during the first tool call
			sess.Steer("actually, focus on X instead", nil)
			steered = true
		}
		return "did work", nil
	}
	prov := &mockProvider{replies: []*llm.Response{
		{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "work", Arguments: json.RawMessage(`{}`)}}},
		{ToolCalls: []llm.ToolCall{{ID: "c2", Name: "work", Arguments: json.RawMessage(`{}`)}}},
		{Text: "done"},
	}}
	reg, err := tool.NewRegistry(steerTool)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}
	sess = a.NewSession()
	if _, err := sess.Send(context.Background(), "original task"); err != nil {
		t.Fatal(err)
	}
	// Round 2's request (prov.seen[1]) must contain the steer as a user message
	// that was NOT present in round 1's request (prov.seen[0]).
	r2 := prov.seen[1]
	found := false
	for _, m := range r2.Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Text, "focus on X") {
			found = true
		}
	}
	if !found {
		t.Fatalf("steer message should appear in round 2's request (mid-turn), messages: %+v", r2.Messages)
	}
	// And it must NOT have been in round 1 (it was injected DURING round 1).
	for _, m := range prov.seen[0].Messages {
		if strings.Contains(m.Text, "focus on X") {
			t.Fatal("steer leaked into round 1 — should only appear from round 2 on")
		}
	}
}

// activeProvider calls a tool every step (steady activity), finishing after n
// tool rounds — simulates a subtask doing real, ongoing work.
type activeProvider struct {
	step, rounds int
}

func (p *activeProvider) Name() string    { return "active" }
func (p *activeProvider) ModelID() string { return "active" }
func (p *activeProvider) Complete(_ context.Context, _ llm.Request) (*llm.Response, error) {
	p.step++
	if p.step > p.rounds {
		return &llm.Response{Text: "done"}, nil
	}
	return &llm.Response{ToolCalls: []llm.ToolCall{{ID: "c", Name: "work", Arguments: json.RawMessage(`{}`)}}}, nil
}

// TestSlowButActiveSubtaskSurvives proves a subtask making steady tool calls is
// NOT killed even when it runs longer than stallIdle — only IDLE kills it.
func TestSlowButActiveSubtaskSurvives(t *testing.T) {
	oldIdle, oldGrace, oldFront := stallIdle, heartbeatGrace, frontWindow
	stallIdle = 100 * time.Millisecond
	heartbeatGrace = 0
	frontWindow = 10 * time.Second
	defer func() { stallIdle, heartbeatGrace, frontWindow = oldIdle, oldGrace, oldFront }()

	// A tool that takes 40ms per call; 8 rounds = ~320ms total, well past the
	// 100ms idle window — but each call resets the heartbeat, so no stall.
	work := callTool("work")
	work.ReadOnly = true
	work.Run = func(context.Context, json.RawMessage) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "did work", nil
	}
	reg, err := tool.NewRegistry(work)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: &activeProvider{rounds: 8}, Tools: reg, Perm: PermAuto}
	out, err := a.SubtaskWith(context.Background(), "steady work", SubtaskOpts{})
	if err != nil {
		t.Fatalf("a steadily-active subtask must NOT be killed, got: %v", err)
	}
	if out != "done" {
		t.Fatalf("want done, got %q", out)
	}
}

// TestForegroundPromotesToBackground proves a still-active subtask that outruns
// the front window is promoted to the background (returns a bg id, not blocked).
func TestForegroundPromotesToBackground(t *testing.T) {
	oldIdle, oldGrace, oldFront := stallIdle, heartbeatGrace, frontWindow
	stallIdle = 10 * time.Second // long: don't stall an active child
	heartbeatGrace = 0
	frontWindow = 80 * time.Millisecond // short: promote quickly
	defer func() { stallIdle, heartbeatGrace, frontWindow = oldIdle, oldGrace, oldFront }()

	work := callTool("work")
	work.ReadOnly = true
	work.Run = func(context.Context, json.RawMessage) (string, error) {
		time.Sleep(30 * time.Millisecond)
		return "did work", nil
	}
	reg, err := tool.NewRegistry(work)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Provider: &activeProvider{rounds: 20}, Tools: reg, Perm: PermAuto, Bg: NewBgRegistry(t.TempDir())}
	out, err := a.SubtaskWith(context.Background(), "long active work", SubtaskOpts{})
	if err != nil {
		t.Fatalf("promotion path should not error: %v", err)
	}
	if !strings.Contains(out, "moved to background") || !strings.Contains(out, "task_status") {
		t.Fatalf("a subtask past the front window should report promotion to bg, got: %q", out)
	}
	// The bg task exists; wait for it to finish so the detached goroutine
	// doesn't outlive the test's defer (and so we observe a clean terminal).
	tasks := a.Bg.List()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 background task after promotion, got %d", len(tasks))
	}
	id := tasks[0].ID
	deadline := time.Now().Add(3 * time.Second)
	for {
		bt := a.Bg.Get(id)
		if bt != nil && bt.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			_ = a.Bg // give up waiting; the activeProvider finishes fast anyway
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// steerProvider is a thread-safe scripted provider that fires a callback after
// a given reply index (to inject a steer at the final-answer boundary).
type steerProvider struct {
	mu      sync.Mutex
	replies []*llm.Response
	seen    []llm.Request
	after   map[int]func() // index → side effect run just before returning that reply
}

func (p *steerProvider) Name() string    { return "steer" }
func (p *steerProvider) ModelID() string { return "steer" }
func (p *steerProvider) Complete(_ context.Context, r llm.Request) (*llm.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := len(p.seen)
	p.seen = append(p.seen, r)
	if fn := p.after[i]; fn != nil {
		fn()
	}
	if i >= len(p.replies) {
		return &llm.Response{Text: "done"}, nil
	}
	return p.replies[i], nil
}

func TestSteerAtFinalAnswerIsConsumedNotStranded(t *testing.T) {
	// The model returns a final answer (no tool calls) on reply 0. A steer
	// lands DURING that call (the after-hook). The turn must NOT end stranding
	// the steer: it loops once more, the steer appears in reply 1's request,
	// and the model answers it.
	var sess *Session
	prov := &steerProvider{
		replies: []*llm.Response{
			{Text: "first answer"},    // reply 0: final answer — but a steer lands now
			{Text: "answer to steer"}, // reply 1: must respond to the steer
		},
	}
	prov.after = map[int]func(){
		0: func() { sess.Steer("wait, also do Y", nil) },
	}
	reg, _ := tool.NewRegistry()
	a := &Agent{Provider: prov, Tools: reg, Perm: PermAuto}
	sess = a.NewSession()
	out, err := sess.Send(context.Background(), "do X")
	if err != nil {
		t.Fatal(err)
	}
	// The turn must have made a SECOND request that carries the steer.
	if len(prov.seen) < 2 {
		t.Fatalf("steer at final answer should trigger another round, got %d requests", len(prov.seen))
	}
	found := false
	for _, m := range prov.seen[1].Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Text, "also do Y") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the steer must appear in the follow-up request, messages: %+v", prov.seen[1].Messages)
	}
	if out != "answer to steer" {
		t.Fatalf("turn should end with the post-steer answer, got %q", out)
	}
}

// TestMaybeCompactFiresAtThreshold: compaction now triggers with headroom (at
// compactTriggerFrac of the budget), not only when the full budget is exceeded.
// A conversation between the threshold and the budget must compact.
func TestMaybeCompactFiresAtThreshold(t *testing.T) {
	fc := &fakeCompactor{}
	const budget = 10000
	a := &Agent{
		Provider:         &mockProvider{},
		Tools:            mustReg(t),
		Perm:             PermAuto,
		Compactor:        fc,
		MaxContextTokens: budget,
	}
	// Build a conversation ~90% of budget: above the 85% trigger, below 100%.
	// EstimateTokens ≈ chars/4. Needs ≥2 user turns so there's older history to
	// summarize (CompactWith degrades to non-LLM Compact with <2 user starts).
	target := int(0.90 * budget)
	chunk := strings.Repeat("word ", target*4/5/3) // ~target/3 tokens each
	msgs := []llm.Message{
		{Role: llm.RoleUser, Text: chunk},
		{Role: llm.RoleAssistant, Text: chunk},
		{Role: llm.RoleUser, Text: chunk},
		{Role: llm.RoleAssistant, Text: "ok"},
	}
	s := a.Resume(msgs)
	got := llm.EstimateTokens(s.snapshot())
	if got <= int(compactTriggerFrac*budget) || got > budget {
		t.Skipf("test fixture sizing off (tokens=%d, want between %d and %d) — estimator drift",
			got, int(compactTriggerFrac*budget), budget)
	}
	s.maybeCompact(context.Background())
	if fc.calls == 0 {
		t.Fatalf("compaction should fire at %d tokens (threshold %d, budget %d)",
			got, int(compactTriggerFrac*budget), budget)
	}
}

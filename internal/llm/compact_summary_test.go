package llm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeCompactor struct{ calls int }

func (f *fakeCompactor) Summarize(_ context.Context, msgs []Message) (string, error) {
	f.calls++
	return "SUMMARY of " + itoa(len(msgs)) + " messages", nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// big builds a message whose text is ~n tokens (n*4 chars).
func big(role Role, n int) Message { return Message{Role: role, Text: strings.Repeat("x ", n*2)} }

func TestCompactWithSummarizesAndFits(t *testing.T) {
	// 6 rounds, each user + assistant; far over budget.
	var msgs []Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, big(RoleUser, 2000), big(RoleAssistant, 2000))
	}
	budget := 8000
	fc := &fakeCompactor{}
	out, err := CompactWith(context.Background(), fc, msgs, budget)
	if err != nil {
		t.Fatal(err)
	}
	if fc.calls == 0 {
		t.Fatal("expected the compactor to be called")
	}
	if EstimateTokens(out) > budget {
		t.Fatalf("compacted still over budget: %d > %d", EstimateTokens(out), budget)
	}
	// First message must be the injected summary as a user turn.
	if out[0].Role != RoleUser || !strings.Contains(out[0].Text, "SUMMARY") {
		t.Fatalf("first message should be the injected summary, got %+v", out[0])
	}
}

func TestCompactWithNeverOrphansToolCalls(t *testing.T) {
	// A round whose assistant makes a tool call answered by a tool message.
	msgs := []Message{
		big(RoleUser, 3000),
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "read"}}},
		{Role: RoleTool, ToolCallID: "c1", Text: strings.Repeat("y ", 6000)},
		big(RoleUser, 100),
		big(RoleAssistant, 100),
	}
	out, err := CompactWith(context.Background(), &fakeCompactor{}, msgs, 4000)
	if err != nil {
		t.Fatal(err)
	}
	// Any tool message retained must have its assistant tool_call retained too.
	calls := map[string]bool{}
	for _, m := range out {
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = true
		}
	}
	for _, m := range out {
		if m.Role == RoleTool && m.ToolCallID != "" && !calls[m.ToolCallID] {
			t.Fatalf("orphaned tool result %q (no matching tool call retained)", m.ToolCallID)
		}
	}
}

func TestCompactWithUnderBudgetIsNoop(t *testing.T) {
	msgs := []Message{big(RoleUser, 10), big(RoleAssistant, 10)}
	fc := &fakeCompactor{}
	out, _ := CompactWith(context.Background(), fc, msgs, 100000)
	if fc.calls != 0 || len(out) != len(msgs) {
		t.Fatal("under-budget conversation should be returned unchanged")
	}
}

func TestCompactWithPreservesOriginalTask(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Text: "ORIGINAL_TASK: build the parser"},
		big(RoleAssistant, 4000),
		big(RoleUser, 4000),
		big(RoleAssistant, 4000),
		big(RoleUser, 100),
		big(RoleAssistant, 100),
	}
	out, err := CompactWith(context.Background(), &fakeCompactor{}, msgs, 6000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out[0].Text, "ORIGINAL_TASK: build the parser") {
		t.Fatalf("compaction should preserve the original task verbatim:\n%s", out[0].Text)
	}
}

type errCompactor struct{ calls int }

func (e *errCompactor) Summarize(context.Context, []Message) (string, error) {
	e.calls++
	return "", errors.New("small model unavailable")
}

func TestCompactorChainFallsBack(t *testing.T) {
	bad := &errCompactor{}
	good := &fakeCompactor{}
	c := CompactorChain(bad, good)
	out, err := c.Summarize(context.Background(), []Message{{Role: RoleUser, Text: "x"}})
	if err != nil {
		t.Fatalf("chain should fall back to the good compactor: %v", err)
	}
	if out == "" {
		t.Fatal("expected the fallback summary")
	}
	if bad.calls != 1 || good.calls != 1 {
		t.Fatalf("want bad=1 good=1, got bad=%d good=%d", bad.calls, good.calls)
	}
}

func TestCompactorChainSkipsNilAndCollapses(t *testing.T) {
	good := &fakeCompactor{}
	if c := CompactorChain(nil, good, nil); c != Compactor(good) {
		t.Fatal("single non-nil compactor should collapse to itself")
	}
	if c := CompactorChain(nil, nil); c != nil {
		t.Fatal("all-nil chain should be nil")
	}
}

func TestCompactorChainAllFail(t *testing.T) {
	bad1, bad2 := &errCompactor{}, &errCompactor{}
	c := CompactorChain(bad1, bad2)
	_, err := c.Summarize(context.Background(), nil)
	if err == nil {
		t.Fatal("want the last error when every compactor fails")
	}
	if bad1.calls != 1 || bad2.calls != 1 {
		t.Fatalf("both should be tried, got %d/%d", bad1.calls, bad2.calls)
	}
}

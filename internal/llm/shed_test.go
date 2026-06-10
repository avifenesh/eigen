package llm

import (
	"context"
	"strings"
	"testing"
)

func TestShedToolResultsStubsOldKeepsRecent(t *testing.T) {
	// 4 rounds, each: user, assistant(tool call), tool result.
	var msgs []Message
	for i := 0; i < 4; i++ {
		msgs = append(msgs,
			Message{Role: RoleUser, Text: "do thing"},
			Message{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "read"}}},
			Message{Role: RoleTool, ToolName: "read", Text: strings.Repeat("BIG ", 500)},
		)
	}
	out := ShedToolResults(msgs, 2)

	// Rounds 0 and 1 (indices 0..5) should be stubbed; rounds 2 and 3 verbatim.
	stubbed := 0
	verbatim := 0
	for _, m := range out {
		if m.Role != RoleTool {
			continue
		}
		if m.Text == toolResultStub {
			stubbed++
		} else if strings.HasPrefix(m.Text, "BIG ") {
			verbatim++
		}
	}
	if stubbed != 2 {
		t.Fatalf("want 2 stubbed tool results, got %d", stubbed)
	}
	if verbatim != 2 {
		t.Fatalf("want 2 verbatim tool results, got %d", verbatim)
	}
	// Tool CALLS must survive (pairing stays valid).
	calls := 0
	for _, m := range out {
		if m.Role == RoleAssistant && len(m.ToolCalls) == 1 {
			calls++
		}
	}
	if calls != 4 {
		t.Fatalf("want all 4 tool calls preserved, got %d", calls)
	}
}

func TestShedToolResultsSkipsErrorsAndStubs(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Text: "a"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "bash"}}},
		{Role: RoleTool, ToolName: "bash", Text: "boom", ToolError: true},
		{Role: RoleTool, ToolName: "x", Text: toolResultStub}, // already stubbed
		{Role: RoleUser, Text: "b"},
		{Role: RoleUser, Text: "c"},
	}
	out := ShedToolResults(msgs, 1)
	if out[2].Text != "boom" {
		t.Fatalf("error result must be preserved, got %q", out[2].Text)
	}
	if out[3].Text != toolResultStub {
		t.Fatalf("already-stubbed result must be unchanged, got %q", out[3].Text)
	}
}

func TestShedToolResultsKeepRoundsZeroStubsAll(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Text: "a"},
		{Role: RoleTool, ToolName: "x", Text: "result"},
	}
	out := ShedToolResults(msgs, 0)
	if out[1].Text != toolResultStub {
		t.Fatalf("keepRounds=0 should stub every result, got %q", out[1].Text)
	}
}

func TestShedToolResultsDoesNotMutateInput(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Text: "a"},
		{Role: RoleTool, ToolName: "x", Text: "original"},
		{Role: RoleUser, Text: "b"},
		{Role: RoleUser, Text: "c"},
	}
	_ = ShedToolResults(msgs, 1)
	if msgs[1].Text != "original" {
		t.Fatalf("input was mutated: %q", msgs[1].Text)
	}
}

// TestCompactWithShedsBeforeSummarizing verifies microcompaction can avoid the
// model summary call entirely when stubbing tool results is enough.
func TestCompactWithShedsBeforeSummarizing(t *testing.T) {
	// Many rounds with huge tool results but small text; shedding old results
	// should bring it under budget without a summary.
	var msgs []Message
	for i := 0; i < 8; i++ {
		msgs = append(msgs,
			Message{Role: RoleUser, Text: "u"},
			Message{Role: RoleAssistant, ToolCalls: []ToolCall{{Name: "read"}}},
			Message{Role: RoleTool, ToolName: "read", Text: strings.Repeat("x ", 2000)}, // ~1000 tok each
		)
	}
	fc := &fakeCompactor{}
	out, err := CompactWith(context.Background(), fc, msgs, 8000)
	if err != nil {
		t.Fatal(err)
	}
	if fc.calls != 0 {
		t.Fatalf("shedding should have avoided the summary call; got %d calls", fc.calls)
	}
	if EstimateTokens(out) > 8000 {
		t.Fatalf("shedding did not bring context under budget: %d", EstimateTokens(out))
	}
}

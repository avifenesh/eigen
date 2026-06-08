package llm

import (
	"encoding/json"
	"testing"
)

func TestConverseGroupsToolResultsIntoUserTurn(t *testing.T) {
	msgs := converseMessages(Request{
		Messages: []Message{
			{Role: RoleUser, Text: "do it"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "t1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
				{ID: "t2", Name: "read", Arguments: json.RawMessage(`{"path":"b"}`)},
			}},
			{Role: RoleTool, ToolCallID: "t1", Text: "A"},
			{Role: RoleTool, ToolCallID: "t2", Text: "B"},
		},
	})

	// Expect: user(text), assistant(2 toolUse), user(2 toolResult).
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || msgs[0].Content[0].Text != "do it" {
		t.Errorf("msg0 wrong: %+v", msgs[0])
	}
	if msgs[1].Role != "assistant" || len(msgs[1].Content) != 2 || msgs[1].Content[0].ToolUse == nil || msgs[1].Content[0].ToolUse.Name != "read" {
		t.Errorf("msg1 (toolUse) wrong: %+v", msgs[1])
	}
	if msgs[2].Role != "user" || len(msgs[2].Content) != 2 {
		t.Fatalf("msg2 should group both tool results into one user turn: %+v", msgs[2])
	}
	if msgs[2].Content[0].ToolResult == nil || msgs[2].Content[0].ToolResult.ToolUseID != "t1" || msgs[2].Content[0].ToolResult.Content[0].Text != "A" {
		t.Errorf("tool result 0 wrong: %+v", msgs[2].Content[0])
	}
	if msgs[2].Content[1].ToolResult == nil || msgs[2].Content[1].ToolResult.Status != "success" {
		t.Errorf("tool result 1 wrong: %+v", msgs[2].Content[1])
	}
}

func TestConverseToolsInputSchema(t *testing.T) {
	tools := converseTools([]ToolSpec{
		{Name: "read", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)},
	})
	if len(tools) != 1 || tools[0].ToolSpec.Name != "read" {
		t.Fatalf("tools wrong: %+v", tools)
	}
	if string(tools[0].ToolSpec.InputSchema.JSON) != `{"type":"object"}` {
		t.Errorf("input schema wrong: %s", tools[0].ToolSpec.InputSchema.JSON)
	}
}

func TestNormalizeArgsRaw(t *testing.T) {
	if string(normalizeArgsRaw(nil)) != "{}" {
		t.Error("nil should become {}")
	}
	if string(normalizeArgsRaw(json.RawMessage(`{"a":1}`))) != `{"a":1}` {
		t.Error("passthrough failed")
	}
}

func TestConverseToolErrorStatus(t *testing.T) {
	msgs := converseMessages(Request{
		Messages: []Message{
			{Role: RoleTool, ToolCallID: "t1", Text: "boom", ToolError: true},
			{Role: RoleTool, ToolCallID: "t2", Text: "ok"},
		},
	})
	if len(msgs) != 1 || len(msgs[0].Content) != 2 {
		t.Fatalf("expected one user turn with two results: %+v", msgs)
	}
	if msgs[0].Content[0].ToolResult.Status != "error" {
		t.Errorf("errored tool result should have status=error, got %q", msgs[0].Content[0].ToolResult.Status)
	}
	if msgs[0].Content[1].ToolResult.Status != "success" {
		t.Errorf("ok tool result should have status=success, got %q", msgs[0].Content[1].ToolResult.Status)
	}
}

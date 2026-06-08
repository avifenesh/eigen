package llm

import (
	"encoding/json"
	"testing"
)

func TestBuildInputSerializesToolTurns(t *testing.T) {
	req := Request{
		System: "sys",
		Messages: []Message{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"x"}`)},
			}},
			{Role: RoleTool, ToolCallID: "c1", ToolName: "read", Text: "filebody"},
		},
	}

	items := buildInput(req)
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4: %+v", len(items), items)
	}
	if items[0].Role != "developer" || string(items[0].Content) != `"sys"` {
		t.Errorf("system not mapped to developer: %+v", items[0])
	}
	if items[1].Role != "user" || string(items[1].Content) != `"hi"` {
		t.Errorf("user message wrong: %+v", items[1])
	}
	fc := items[2]
	if fc.Type != "function_call" || fc.CallID != "c1" || fc.Name != "read" || fc.Arguments != `{"path":"x"}` {
		t.Errorf("function_call wrong: %+v", fc)
	}
	out := items[3]
	if out.Type != "function_call_output" || out.CallID != "c1" || out.Output != "filebody" {
		t.Errorf("function_call_output wrong: %+v", out)
	}
}

func TestArgNormalization(t *testing.T) {
	if got := argString(nil); got != "{}" {
		t.Errorf("argString(nil) = %q, want {}", got)
	}
	if got := argString(json.RawMessage(`{"a":1}`)); got != `{"a":1}` {
		t.Errorf("argString passthrough = %q", got)
	}
	if got := string(normalizeArgs("")); got != "{}" {
		t.Errorf("normalizeArgs(empty) = %q, want {}", got)
	}
	if got := string(normalizeArgs(`{"a":1}`)); got != `{"a":1}` {
		t.Errorf("normalizeArgs passthrough = %q", got)
	}
}

func TestToResponsesTools(t *testing.T) {
	tools := toResponsesTools([]ToolSpec{
		{Name: "read", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)},
	})
	if len(tools) != 1 || tools[0].Type != "function" || tools[0].Name != "read" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if toResponsesTools(nil) != nil {
		t.Error("expected nil tools for empty input")
	}
}

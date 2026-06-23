package transcript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

// TestClaudeResultText covers flattening a tool_result's content. Multiple text
// blocks must be newline-separated (previously concatenated with no separator,
// folding "line one"+"line two" into "line oneline two"); non-text blocks carry
// no text and are skipped.
func TestClaudeResultText(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", ``, ""},
		{"string", `"hello world"`, "hello world"},
		{"single text block", `[{"type":"text","text":"only"}]`, "only"},
		{"two text blocks joined", `[{"type":"text","text":"line one"},{"type":"text","text":"line two"}]`, "line one\nline two"},
		{"text with image skipped", `[{"type":"text","text":"alpha"},{"type":"image","source":{}},{"type":"text","text":"beta"}]`, "alpha\nbeta"},
		{"empty array", `[]`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claudeResultText([]byte(tc.raw))
			if got != tc.want {
				t.Fatalf("claudeResultText(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestParseClaudeMultiTextBlocks verifies that multiple assistant text blocks
// and a multi-block tool_result are each folded with a newline separator rather
// than running together.
func TestParseClaudeMultiTextBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	lines := []string{
		// assistant with two text blocks around a tool_use
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"line one"},{"type":"text","text":"line two"},{"type":"tool_use","id":"t1","name":"read","input":{"path":"x"}}]}}`,
		// tool_result with two text blocks
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"out one"},{"type":"text","text":"out two"}]}]}}`,
	}
	var data string
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, err := parseClaude(path)
	if err != nil {
		t.Fatalf("parseClaude: %v", err)
	}

	var asst, tool *llm.Message
	for i := range msgs {
		switch msgs[i].Role {
		case llm.RoleAssistant:
			asst = &msgs[i]
		case llm.RoleTool:
			tool = &msgs[i]
		}
	}
	if asst == nil {
		t.Fatalf("no assistant message: %#v", msgs)
	}
	if asst.Text != "line one\nline two" {
		t.Fatalf("assistant text folded without separator: got %q, want %q", asst.Text, "line one\nline two")
	}
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].ID != "t1" {
		t.Fatalf("assistant tool call lost: %#v", asst.ToolCalls)
	}
	if tool == nil {
		t.Fatalf("no tool message: %#v", msgs)
	}
	if tool.Text != "out one\nout two" {
		t.Fatalf("tool_result folded without separator: got %q, want %q", tool.Text, "out one\nout two")
	}
}

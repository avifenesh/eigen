package transcript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

// TestParseCodexArrayShapedOutput covers the case where a function_call_output's
// output is a JSON array of content blocks rather than a plain string. The text
// of input_text/output_text blocks must be preserved (previously dropped because
// the field was typed as string and failed to unmarshal).
func TestParseCodexArrayShapedOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-array.jsonl")

	lines := []string{
		// assistant tool call
		`{"type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"c1","arguments":"{\"cmd\":\"ls\"}"}}`,
		// array-shaped output: input_text block carries text, input_image carries none
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":[{"type":"input_text","text":"Wall time: 0.06s\nOutput:\nfile.go\n"},{"type":"input_image","image_url":"data:image/jpeg;base64,AAAA","detail":"auto"}]}}`,
		// plain string output still works
		`{"type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"c2","arguments":"{}"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"c2","output":"plain string output"}}`,
	}
	var data string
	for _, l := range lines {
		data += l + "\n"
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	msgs, err := parseCodex(path)
	if err != nil {
		t.Fatalf("parseCodex: %v", err)
	}

	var tools []llm.Message
	for _, m := range msgs {
		if m.Role == llm.RoleTool {
			tools = append(tools, m)
		}
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tool messages, got %d: %#v", len(tools), msgs)
	}

	const wantArray = "Wall time: 0.06s\nOutput:\nfile.go\n"
	if tools[0].ToolCallID != "c1" || tools[0].Text != wantArray {
		t.Fatalf("array-shaped output not flattened: got %q (id %q), want %q",
			tools[0].Text, tools[0].ToolCallID, wantArray)
	}
	if tools[1].ToolCallID != "c2" || tools[1].Text != "plain string output" {
		t.Fatalf("string output mishandled: got %q (id %q)", tools[1].Text, tools[1].ToolCallID)
	}
}

func TestCodexOutputText(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", ``, ""},
		{"string", `"hello world"`, "hello world"},
		{"array text blocks", `[{"type":"input_text","text":"a"},{"type":"output_text","text":"b"}]`, "ab"},
		{"array with image", `[{"type":"input_text","text":"only text"},{"type":"input_image","image_url":"data:..."}]`, "only text"},
		{"empty array", `[]`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := codexOutputText([]byte(tc.raw))
			if got != tc.want {
				t.Fatalf("codexOutputText(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

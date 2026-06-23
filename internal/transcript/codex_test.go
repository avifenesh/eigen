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

// TestParseCodexCarriesReasoning verifies that reasoning response_item lines
// fold into the assistant message: the summary text becomes Reasoning and the
// encrypted_content blob (with its paired id) becomes ReasoningEncrypted, so a
// resumed store:false session can echo the blob back instead of 404ing on the id.
func TestParseCodexCarriesReasoning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-reasoning.jsonl")

	lines := []string{
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"do it"}]}}`,
		// reasoning item: summary text + encrypted blob bound to id rs_1
		`{"type":"response_item","payload":{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"first I plan"}],"encrypted_content":"BLOB1=="}}`,
		// assistant invokes a tool in the same turn
		`{"type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"c1","arguments":"{}"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"ok"}}`,
		// next turn: two reasoning items, blob on the LAST one must win and stay paired with its id
		`{"type":"response_item","payload":{"type":"reasoning","id":"rs_2","summary":[{"type":"summary_text","text":"reconsider"}]}}`,
		`{"type":"response_item","payload":{"type":"reasoning","id":"rs_3","summary":[{"type":"summary_text","text":"final answer plan"}],"encrypted_content":"BLOB3=="}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`,
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

	var asst []llm.Message
	for _, m := range msgs {
		if m.Role == llm.RoleAssistant {
			asst = append(asst, m)
		}
	}
	if len(asst) != 2 {
		t.Fatalf("expected 2 assistant messages, got %d: %#v", len(asst), msgs)
	}

	// First turn: reasoning + tool call.
	if asst[0].Reasoning != "first I plan" {
		t.Fatalf("turn1 Reasoning = %q, want %q", asst[0].Reasoning, "first I plan")
	}
	if asst[0].ReasoningID != "rs_1" || asst[0].ReasoningEncrypted != "BLOB1==" {
		t.Fatalf("turn1 id/blob = %q/%q, want rs_1/BLOB1==", asst[0].ReasoningID, asst[0].ReasoningEncrypted)
	}
	if len(asst[0].ToolCalls) != 1 || asst[0].ToolCalls[0].ID != "c1" {
		t.Fatalf("turn1 tool calls = %#v, want one call c1", asst[0].ToolCalls)
	}

	// Second turn: blob must come from rs_3 (the last item carrying one), and the
	// id must stay paired with it — never rs_2's id with rs_3's blob (that 400s).
	if asst[1].Reasoning != "reconsider\nfinal answer plan" {
		t.Fatalf("turn2 Reasoning = %q, want %q", asst[1].Reasoning, "reconsider\nfinal answer plan")
	}
	if asst[1].ReasoningID != "rs_3" || asst[1].ReasoningEncrypted != "BLOB3==" {
		t.Fatalf("turn2 id/blob = %q/%q, want rs_3/BLOB3== (paired)", asst[1].ReasoningID, asst[1].ReasoningEncrypted)
	}
	if asst[1].Text != "done" {
		t.Fatalf("turn2 Text = %q, want done", asst[1].Text)
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

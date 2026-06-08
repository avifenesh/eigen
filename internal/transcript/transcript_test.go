package transcript

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/avifenesh/eigen/internal/llm"
)

func TestEigenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.eigen.jsonl")
	in := []llm.Message{
		{Role: llm.RoleUser, Text: "hello"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Name: "read", Arguments: []byte(`{"path":"x"}`)}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Text: "filebody"},
		{Role: llm.RoleAssistant, Text: "done"},
	}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("got %d messages, want %d", len(out), len(in))
	}
	if out[1].ToolCalls[0].Name != "read" || out[2].Text != "filebody" || out[3].Text != "done" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

// validateImport asserts an imported transcript is non-empty and well-formed.
func validateImport(t *testing.T, msgs []llm.Message) {
	t.Helper()
	if len(msgs) == 0 {
		t.Fatal("imported zero messages")
	}
	for i, m := range msgs {
		switch m.Role {
		case llm.RoleUser, llm.RoleAssistant, llm.RoleTool:
		default:
			t.Fatalf("message %d has invalid role %q", i, m.Role)
		}
		for _, tc := range m.ToolCalls {
			if tc.Name == "" {
				t.Fatalf("message %d has a tool call with empty name", i)
			}
		}
	}
}

// importRealSource globs a source's transcripts and imports the most recent one,
// skipping when none exist on this machine.
func importRealSource(t *testing.T, glob string, src Source) {
	t.Helper()
	home, _ := os.UserHomeDir()
	matches, _ := filepath.Glob(filepath.Join(home, glob))
	if len(matches) == 0 {
		t.Skipf("no %s transcripts found", src)
	}
	// pick the largest (most substantial) match
	var pick string
	var best int64
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.Size() > best {
			best, pick = fi.Size(), m
		}
	}
	msgs, err := ImportFrom(src, pick)
	if err != nil {
		t.Fatalf("import %s (%s): %v", src, pick, err)
	}
	validateImport(t, msgs)
	t.Logf("%s: imported %d messages from %s", src, len(msgs), filepath.Base(pick))
}

func TestImportClaude(t *testing.T) { importRealSource(t, ".claude/projects/*/*.jsonl", SourceClaude) }
func TestImportCodex(t *testing.T) {
	importRealSource(t, ".codex/sessions/*/*/*/rollout-*.jsonl", SourceCodex)
}
func TestImportPi(t *testing.T)     { importRealSource(t, ".pi/agent/sessions/*/*.jsonl", SourcePi) }
func TestImportHermes(t *testing.T) { importRealSource(t, ".hermes/sessions/*.jsonl", SourceHermes) }

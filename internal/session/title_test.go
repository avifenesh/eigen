package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/transcript"
)

// codexUserLine is a Codex rollout "response_item" message line carrying one
// user text block — the shape userTextFromLine extracts the title head from.
func codexUserLine(t *testing.T, text string) string {
	t.Helper()
	rec := map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":    "message",
			"role":    "user",
			"content": []map[string]any{{"type": "input_text", "text": text}},
		},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// codexNoiseLine is a non-message rollout record (the kind Codex prepends in
// bulk before the first real user turn), which userTextFromLine must skip.
func codexNoiseLine(t *testing.T) string {
	t.Helper()
	rec := map[string]any{
		"type":    "response_item",
		"payload": map[string]any{"type": "reasoning", "summary": "thinking…"},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestFirstUserTextPastLineCap is the regression guard for APP-067: Codex
// rollouts prepend large non-message blocks, so the first user message can land
// well past the old 300-line cap. With a byte budget instead of a line cap,
// firstUserText must still find it.
func TestFirstUserTextPastLineCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")

	var lines []string
	for i := 0; i < 1000; i++ { // far past the old 300-line cap
		lines = append(lines, codexNoiseLine(t))
	}
	lines = append(lines, codexUserLine(t, "the real first user ask"))
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := firstUserText(transcript.SourceCodex, path); got != "the real first user ask" {
		t.Fatalf("firstUserText = %q, want the user message past the old line cap", got)
	}
}

// TestFirstUserTextRespectsByteBudget verifies the scan stays bounded: a user
// message buried beyond the byte budget is (intentionally) not returned, so the
// titler never reads an unbounded prefix of a huge transcript.
func TestFirstUserTextRespectsByteBudget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.jsonl")

	noise := codexNoiseLine(t)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	written := 0
	for written < firstUserScanBudget+len(noise) { // overrun the budget with noise
		n, _ := f.WriteString(noise + "\n")
		written += n
	}
	if _, err := f.WriteString(codexUserLine(t, "buried far too deep") + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	if got := firstUserText(transcript.SourceCodex, path); got != "" {
		t.Fatalf("firstUserText = %q, want empty (message past the byte budget)", got)
	}
}

// TestFirstUserTextOpenCodeSkipped confirms the OpenCode short-circuit is intact
// (its titles come from the DB, not the file).
func TestFirstUserTextOpenCodeSkipped(t *testing.T) {
	if got := firstUserText(transcript.SourceOpenCode, "/nonexistent"); got != "" {
		t.Fatalf("firstUserText(OpenCode) = %q, want empty", got)
	}
}

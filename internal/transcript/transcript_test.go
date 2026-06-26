package transcript

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSaveKeepsPreviousFileBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	first := []llm.Message{{Role: llm.RoleUser, Text: "first"}}
	second := []llm.Message{{Role: llm.RoleUser, Text: "second"}}

	if err := Save(path, first); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, second); err != nil {
		t.Fatal(err)
	}

	bak, err := Load(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(bak) != 1 || bak[0].Text != "first" {
		t.Fatalf("backup should contain previous transcript, got %#v", bak)
	}
	cur, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cur) != 1 || cur[0].Text != "second" {
		t.Fatalf("current transcript should contain new save, got %#v", cur)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temporary file should be gone after save, stat err=%v", err)
	}
}

func TestSaveRefusesEmptyOverNonEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	good := []llm.Message{{Role: llm.RoleUser, Text: "keep-me"}}
	if err := Save(path, good); err != nil {
		t.Fatal(err)
	}

	// An accidental empty/short autosave must be refused, not silently truncate
	// the live transcript (and rotate it into a backup).
	if err := Save(path, nil); err == nil {
		t.Fatal("Save([]) over a non-empty transcript should return an error")
	}
	if err := Save(path, []llm.Message{}); err == nil {
		t.Fatal("Save(empty slice) over a non-empty transcript should return an error")
	}

	// The live transcript is untouched and no backup was rotated by the refusal.
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "keep-me" {
		t.Fatalf("transcript must be unchanged after refused empty save, got %#v", got)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("a refused empty save must not rotate a backup, stat .bak err=%v", err)
	}
}

func TestSaveAllowsEmptyOverEmptyOrMissing(t *testing.T) {
	dir := t.TempDir()

	// Empty save when the target is missing is allowed (creates an empty file).
	missing := filepath.Join(dir, "missing.jsonl")
	if err := Save(missing, nil); err != nil {
		t.Fatalf("empty save over a missing target should succeed: %v", err)
	}

	// Empty save over an already-empty file is allowed (nothing to lose).
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(empty, nil); err != nil {
		t.Fatalf("empty save over an empty target should succeed: %v", err)
	}
}

func TestSaveForceClearsNonEmptyTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	if err := Save(path, []llm.Message{{Role: llm.RoleUser, Text: "old"}}); err != nil {
		t.Fatal(err)
	}

	// A deliberate clear bypasses the guard.
	if err := SaveForce(path, nil); err != nil {
		t.Fatalf("SaveForce should clear a live transcript: %v", err)
	}
	got, err := loadJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("SaveForce(nil) should leave an empty transcript, got %#v", got)
	}
}

func TestSaveRotatesBackupGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	for i := 1; i <= 7; i++ {
		if err := Save(path, []llm.Message{{Role: llm.RoleUser, Text: string(rune('0' + i))}}); err != nil {
			t.Fatal(err)
		}
	}

	cases := map[string]string{
		path:            "7",
		path + ".bak":   "6",
		path + ".bak.1": "5",
		path + ".bak.2": "4",
		path + ".bak.3": "3",
		path + ".bak.4": "2",
	}
	for p, want := range cases {
		got, err := Load(p)
		if err != nil {
			t.Fatalf("load %s: %v", filepath.Base(p), err)
		}
		if len(got) != 1 || got[0].Text != want {
			t.Fatalf("%s = %#v, want %q", filepath.Base(p), got, want)
		}
	}
	if _, err := os.Stat(path + ".bak.5"); !os.IsNotExist(err) {
		t.Fatalf("only five backup generations should be kept, stat .bak.5 err=%v", err)
	}
}

func TestLoadRecoversFromBackupWhenPrimaryTruncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")

	// First save establishes a good transcript; the second save rotates that
	// first transcript into path+".bak" (the newest backup generation).
	if err := Save(path, []llm.Message{{Role: llm.RoleUser, Text: "keep-me"}}); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, []llm.Message{{Role: llm.RoleUser, Text: "newer"}}); err != nil {
		t.Fatal(err)
	}

	// Corrupt the primary by truncating it to zero bytes. The .bak generation
	// still holds the previous readable transcript.
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after truncation: %v", err)
	}
	if len(got) != 1 || got[0].Text != "keep-me" {
		t.Fatalf("Load should recover newest backup, got %#v", got)
	}

	// When the primary is removed entirely, recovery still applies.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatalf("Load after removal: %v", err)
	}
	if len(got) != 1 || got[0].Text != "keep-me" {
		t.Fatalf("Load should recover backup when primary missing, got %#v", got)
	}
}

func TestSaveConcurrentWritersLeaveCompleteTranscriptAndNoTemps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.eigen.jsonl")
	const writers = 16
	const rounds = 25
	errCh := make(chan error, writers)
	for w := 0; w < writers; w++ {
		w := w
		go func() {
			for r := 0; r < rounds; r++ {
				msgs := []llm.Message{
					{Role: llm.RoleUser, Text: "writer" + string(rune('A'+w))},
					{Role: llm.RoleAssistant, Text: "round" + string(rune('A'+r%26)) + strings.Repeat("x", 4096)},
				}
				if err := Save(path, msgs); err != nil {
					errCh <- err
					return
				}
			}
			errCh <- nil
		}()
	}
	for i := 0; i < writers; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].Role != llm.RoleUser || msgs[1].Role != llm.RoleAssistant {
		t.Fatalf("current transcript should be one complete save, got %#v", msgs)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files leaked after concurrent saves: %v", matches)
	}
}

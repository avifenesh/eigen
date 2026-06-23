package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/transcript"
)

// TestExportSessionDaemonPersisted proves the daemon-persisted branch: an id
// that lives only under daemon.PersistedTranscriptPath (NOT in the session
// store) exports its durable JSONL instead of failing with os.ErrNotExist —
// mirroring the TUI fork in internal/app/sessions.go (APP-065).
func TestExportSessionDaemonPersisted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("EIGEN_INSTANCE", "") // default instance → ~/.eigen/daemon/sessions

	const id = "daemon-only-1234"
	src := daemon.PersistedTranscriptPath(id)
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatalf("mkdir sessions dir: %v", err)
	}
	want := []llm.Message{
		{Role: llm.RoleUser, Text: "hello daemon"},
		{Role: llm.RoleAssistant, Text: "hi from the durable transcript"},
	}
	if err := transcript.Save(src, want); err != nil {
		t.Fatalf("seed durable transcript: %v", err)
	}

	b := &Bridge{}
	dest, err := b.ExportSession(id)
	if err != nil {
		t.Fatalf("ExportSession(daemon-persisted) error = %v, want nil", err)
	}
	if !strings.HasPrefix(dest, filepath.Join(home, "eigen-exports")) {
		t.Fatalf("dest %q not under ~/eigen-exports", dest)
	}
	got, err := transcript.Load(dest)
	if err != nil {
		t.Fatalf("load exported transcript: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("exported %d messages, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Text != want[i].Text {
			t.Fatalf("message %d = {%s %q}, want {%s %q}", i, got[i].Role, got[i].Text, want[i].Role, want[i].Text)
		}
	}
}

// TestExportSessionMissing proves a wholly-unknown id (neither a durable daemon
// transcript nor a store meta) reports an error rather than writing a file.
func TestExportSessionMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("EIGEN_INSTANCE", "")

	b := &Bridge{}
	if _, err := b.ExportSession("nope-does-not-exist"); err == nil {
		t.Fatal("ExportSession(unknown id) error = nil, want non-nil")
	}
}

// TestSafeFileIDRejectsTraversal is the security guard (APP-027 family): an id
// carrying path separators or traversal must be flattened so the export path
// can never escape ~/eigen-exports.
func TestSafeFileIDRejectsTraversal(t *testing.T) {
	// safeFileID preserves dots (ids may carry them) and only flattens path
	// separators, spaces, and control bytes — enough that the joined export path
	// can never escape ~/eigen-exports.
	cases := map[string]string{
		"../../etc/passwd": ".._.._etc_passwd",
		"a/b\\c":           "a_b_c",
		"normal-id_1.2":    "normal-id_1.2",
		"/abs/path":        "_abs_path",
		"":                 "session",
		"with space":       "with_space",
		"nul\x00byte":      "nul_byte",
		"..":               "..",
	}
	for in, want := range cases {
		got := safeFileID(in)
		if got != want {
			t.Errorf("safeFileID(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(got, "/\\") {
			t.Errorf("safeFileID(%q) = %q still contains a path separator", in, got)
		}
	}
}

// TestExportStampFormat keeps the stamp filename-safe (no separators/colons).
func TestExportStampFormat(t *testing.T) {
	s := exportStamp()
	if strings.ContainsAny(s, "/\\: ") {
		t.Errorf("exportStamp() = %q contains a filename-unsafe char", s)
	}
	if len(s) != len("20060102-150405") {
		t.Errorf("exportStamp() = %q, unexpected length %d", s, len(s))
	}
}

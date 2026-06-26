package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/transcript"
)

// writeAt creates a file with the given relative path under home and stamps it
// with mtime, creating parent dirs as needed.
func writeAt(t *testing.T, home, rel string, mtime time.Time) string {
	t.Helper()
	path := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestRecentAgentSessionsOrderingAndTagging lays down one transcript per
// file-based agent with distinct mtimes and asserts recentAgentSessions returns
// them newest-first with the correct Source tag, and that a missing agent dir
// is skipped rather than erroring.
func TestRecentAgentSessionsOrderingAndTagging(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads USERPROFILE on Windows

	base := time.Now().Add(-time.Hour)
	// Newest -> oldest: codex, claude, eigen.
	eigenPath := writeAt(t, home, ".eigen/sessions/a.eigen.jsonl", base)
	claudePath := writeAt(t, home, ".claude/projects/-home-u-proj/b.jsonl", base.Add(time.Minute))
	codexPath := writeAt(t, home, ".codex/sessions/2026/06/23/rollout-c.jsonl", base.Add(2*time.Minute))

	got := recentAgentSessions(10)

	// OpenCode DB does not exist under the temp HOME, so it is skipped silently.
	if len(got) != 3 {
		t.Fatalf("expected 3 refs, got %d: %+v", len(got), got)
	}

	wantOrder := []struct {
		path string
		src  transcript.Source
	}{
		{codexPath, transcript.SourceCodex},
		{claudePath, transcript.SourceClaude},
		{eigenPath, transcript.SourceEigen},
	}
	for i, w := range wantOrder {
		if got[i].Path != w.path {
			t.Errorf("ref %d: path = %q, want %q", i, got[i].Path, w.path)
		}
		if got[i].Source != w.src {
			t.Errorf("ref %d: source = %q, want %q", i, got[i].Source, w.src)
		}
	}

	// n caps the result.
	if capped := recentAgentSessions(2); len(capped) != 2 {
		t.Fatalf("expected 2 with n=2, got %d", len(capped))
	}
	if capped := recentAgentSessions(2); capped[0].Source != transcript.SourceCodex {
		t.Fatalf("n cap should keep newest; got %q first", capped[0].Source)
	}
}

// TestRecentAgentSessionsEmptyHome confirms an entirely missing set of agent
// dirs yields no refs and no panic (every dir guarded by os.Stat).
func TestRecentAgentSessionsEmptyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads USERPROFILE on Windows
	if got := recentAgentSessions(8); len(got) != 0 {
		t.Fatalf("expected no refs for empty home, got %d", len(got))
	}
}

// TestDreamSessionID confirms the dream id is source-prefixed and strips the
// eigen suffix, so two agents can't collide on an identical base name.
func TestDreamSessionID(t *testing.T) {
	cases := []struct {
		ref  sessionRef
		want string
	}{
		{sessionRef{Path: "/h/.eigen/sessions/a.eigen.jsonl", Source: transcript.SourceEigen}, "eigen:a"},
		{sessionRef{Path: "/h/.claude/projects/p/b.jsonl", Source: transcript.SourceClaude}, "claude:b.jsonl"},
		{sessionRef{Path: "oc-session-123", Source: transcript.SourceOpenCode}, "opencode:oc-session-123"},
	}
	for _, c := range cases {
		if got := dreamSessionID(c.ref); got != c.want {
			t.Errorf("dreamSessionID(%+v) = %q, want %q", c.ref, got, c.want)
		}
	}
}

// TestDreamWatermark confirms a file-backed ref mixes mtime with size, while a
// ref with no file on disk (e.g. OpenCode) falls back to its ModTime.
func TestDreamWatermark(t *testing.T) {
	home := t.TempDir()
	mtime := time.Now().Add(-time.Hour).Truncate(time.Second)
	path := writeAt(t, home, ".eigen/sessions/a.eigen.jsonl", mtime)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	wantFile := fi.ModTime().Unix() ^ fi.Size()
	if got := dreamWatermark(sessionRef{Path: path, Source: transcript.SourceEigen, ModTime: mtime}); got != wantFile {
		t.Errorf("file watermark = %d, want %d", got, wantFile)
	}

	// No file on disk: fall back to the ref's ModTime.
	oc := sessionRef{Path: "oc-session-123", Source: transcript.SourceOpenCode, ModTime: mtime}
	if got := dreamWatermark(oc); got != mtime.Unix() {
		t.Errorf("opencode watermark = %d, want %d", got, mtime.Unix())
	}
}

// TestPrintSessionsFallbackSpansAgents confirms that when the store index is
// unavailable, --list still surfaces OTHER agents' sessions as resumable, by
// path (which --resume translates into eigen history on load). Without the
// cross-agent fallback this prints nothing.
func TestPrintSessionsFallbackSpansAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir reads USERPROFILE on Windows

	base := time.Now().Add(-time.Hour)
	claudePath := writeAt(t, home, ".claude/projects/-home-u-proj/b.jsonl", base.Add(time.Minute))
	codexPath := writeAt(t, home, ".codex/sessions/2026/06/23/rollout-c.jsonl", base.Add(2*time.Minute))

	out := captureStdout(t, func() { printSessions(nil) })

	for _, want := range []string{claudePath, codexPath, "claude", "codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("printSessions(nil) output missing %q:\n%s", want, out)
		}
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it
// wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	fn()
	w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFirstGlobSegment(t *testing.T) {
	cases := map[string]string{
		".eigen/sessions/*.eigen.jsonl":         ".eigen/sessions",
		".claude/projects/*/*.jsonl":            ".claude/projects",
		".codex/sessions/*/*/*/rollout-*.jsonl": ".codex/sessions",
	}
	for glob, want := range cases {
		if got := firstGlobSegment(glob); got != want {
			t.Errorf("firstGlobSegment(%q) = %q, want %q", glob, got, want)
		}
	}
}

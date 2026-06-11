package feed

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	return dir
}

func TestScanGitDirty(t *testing.T) {
	dir := gitRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0o644)
	items := scanGit([]string{dir})
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d: %+v", len(items), items)
	}
	it := items[0]
	if it.Kind != "git" || !strings.Contains(it.Title, "2 uncommitted") {
		t.Fatalf("item: %+v", it)
	}
	if it.Dir != dir || it.Task == "" {
		t.Fatal("item must carry dir + a ready task")
	}
}

func TestScanGitCleanRepoQuiet(t *testing.T) {
	dir := gitRepo(t)
	if items := scanGit([]string{dir}); len(items) != 0 {
		t.Fatalf("clean repo should offer nothing, got %+v", items)
	}
	// Non-repos are skipped silently.
	if items := scanGit([]string{t.TempDir()}); len(items) != 0 {
		t.Fatal("non-repo should be skipped")
	}
}

func TestSplitBullets(t *testing.T) {
	notes := "- first bullet\n  continued line\n- second bullet\n"
	bs := splitBullets(notes)
	if len(bs) != 2 || !strings.Contains(bs[0], "continued line") {
		t.Fatalf("bullets: %q", bs)
	}
}

func TestIntentRegexp(t *testing.T) {
	yes := []string{
		"REMAINING: routing the top-level turn",
		"STILL DEFERRED: global memory split",
		"the user wants to clean the edges",
		"TODO: fix the flaky test",
		"next steps: wire the feed",
		"revisit when projects multiply",
		"still need to wire the auxiliary router",
	}
	no := []string{
		"shipped the feature end to end",
		"verified live on this host",
		"go) shipped — the last deferred ROADMAP tool", // 'deferred' as adjective
		"this deferred the work to a later commit",     // past-tense, not a marker
	}
	for _, s := range yes {
		if !intentRe.MatchString(s) {
			t.Fatalf("should match: %q", s)
		}
	}
	for _, s := range no {
		if intentRe.MatchString(s) {
			t.Fatalf("should NOT match: %q", s)
		}
	}
}

func TestCacheRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	f := Feed{Items: []Item{{Kind: "git", Title: "t", Task: "do it"}}, Scanned: time.Now()}
	save(f)
	got, fresh := Load()
	if !fresh || len(got.Items) != 1 || got.Items[0].Title != "t" {
		t.Fatalf("cache round-trip: fresh=%v items=%+v", fresh, got.Items)
	}
	// Stale cache loads but reports not-fresh.
	f.Scanned = time.Now().Add(-time.Hour)
	save(f)
	_, fresh = Load()
	if fresh {
		t.Fatal("hour-old cache must not be fresh")
	}
}

func TestFirstSentenceAround(t *testing.T) {
	b := "- 2026-06-10 — shipped lots of things. REMAINING: tray presence + autostart; daemon sessions persisting. more text."
	got := firstSentenceAround(b, intentRe)
	if !strings.HasPrefix(got, "REMAINING") {
		t.Fatalf("sentence: %q", got)
	}
}

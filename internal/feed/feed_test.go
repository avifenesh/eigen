package feed

import (
	"encoding/json"
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

func TestDirtyFilesCountsRenameAsOne(t *testing.T) {
	dir := gitRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "old.txt"), []byte("hello\n"), 0o644)
	run("add", ".")
	run("commit", "-qm", "init")
	// A rename is one logical change; -z spells it across two NUL fields.
	run("mv", "old.txt", "new.txt")
	// Plus one untracked file → two dirty files total.
	os.WriteFile(filepath.Join(dir, "u.txt"), []byte("x"), 0o644)
	if n := dirtyFiles(dir); n != 2 {
		t.Fatalf("rename + untracked should count as 2 files, got %d", n)
	}
}

func TestDirtyFilesNewlineInPath(t *testing.T) {
	dir := gitRepo(t)
	// A single path containing a newline is one file, not two lines.
	os.WriteFile(filepath.Join(dir, "a\nb.txt"), []byte("x"), 0o644)
	if n := dirtyFiles(dir); n != 1 {
		t.Fatalf("newline-containing path is one file, got %d", n)
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

func TestFirstSentenceAroundStripsBulletMarker(t *testing.T) {
	// No preceding .;: separator before the intent match, so start==0 and the
	// clause begins at the bullet's leading "- " marker. It must be stripped.
	b := "- we still need to ship X before the demo"
	got := firstSentenceAround(b, intentRe)
	if strings.HasPrefix(got, "-") {
		t.Fatalf("clause must not keep the bullet marker: %q", got)
	}
	if !strings.HasPrefix(got, "we still need to ship X") {
		t.Fatalf("clause: %q", got)
	}
}

func TestRankOrdersByActionability(t *testing.T) {
	items := []Item{
		{Kind: "memory", Title: "m1"},
		{Kind: "git", Title: "x: 3 unpushed commit(s)"},
		{Kind: "github", Title: "assigned issue: do thing"},
		{Kind: "git", Title: "x: 2 uncommitted file(s)"},
		{Kind: "github", Title: "review requested: fix bug"},
	}
	got := rank(items)
	want := []string{
		"review requested: fix bug",
		"assigned issue: do thing",
		"x: 2 uncommitted file(s)",
		"x: 3 unpushed commit(s)",
		"m1",
	}
	for i, w := range want {
		if got[i].Title != w {
			t.Fatalf("rank[%d] = %q, want %q", i, got[i].Title, w)
		}
	}
}

func TestRankStableWithinScore(t *testing.T) {
	items := []Item{
		{Kind: "memory", Title: "first"},
		{Kind: "memory", Title: "second"},
	}
	got := rank(items)
	if got[0].Title != "first" || got[1].Title != "second" {
		t.Fatalf("rank must be stable: %+v", got)
	}
}

func TestDismissRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := Item{Kind: "git", Title: "x: 2 uncommitted file(s)", Dir: "/p"}
	b := Item{Kind: "git", Title: "x: 5 uncommitted file(s)", Dir: "/p"}
	Dismiss(a)
	out := FilterDismissed([]Item{a, b})
	if len(out) != 1 || out[0].Title != b.Title {
		t.Fatalf("filter: %+v", out)
	}
	// Content change = new identity = resurfaces (b was never dismissed).
	if a.Key() == b.Key() {
		t.Fatal("changed content must change the key")
	}
}

func TestDismissExpires(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	it := Item{Kind: "memory", Title: "old intent", Dir: "/p"}
	// Write an expired dismissal directly.
	d := map[string]time.Time{it.Key(): time.Now().Add(-15 * 24 * time.Hour)}
	b, _ := json.Marshal(d)
	os.MkdirAll(filepath.Dir(dismissedPath()), 0o755)
	os.WriteFile(dismissedPath(), b, 0o644)
	out := FilterDismissed([]Item{it})
	if len(out) != 1 {
		t.Fatal("expired dismissal must resurface the item")
	}
}

func TestTopDiversity(t *testing.T) {
	items := []Item{
		{Kind: "github", Title: "g1"}, {Kind: "github", Title: "g2"},
		{Kind: "github", Title: "g3"}, {Kind: "github", Title: "g4"},
		{Kind: "git", Title: "w1"}, {Kind: "memory", Title: "m1"},
	}
	got := Top(items, 6, 3)
	if len(got) != 6 {
		t.Fatalf("len = %d", len(got))
	}
	// First 5: g1 g2 g3 (cap) then w1 m1; g4 backfills last.
	want := []string{"g1", "g2", "g3", "w1", "m1", "g4"}
	for i, w := range want {
		if got[i].Title != w {
			t.Fatalf("top[%d] = %q, want %q (%+v)", i, got[i].Title, w, got)
		}
	}
}

func TestTopLimit(t *testing.T) {
	items := []Item{{Kind: "git", Title: "a"}, {Kind: "git", Title: "b"}}
	if got := Top(items, 1, 3); len(got) != 1 || got[0].Title != "a" {
		t.Fatalf("top: %+v", got)
	}
	if got := Top(nil, 5, 3); got != nil {
		t.Fatal("nil in, nil out")
	}
}

func TestScanGitBehindUpstream(t *testing.T) {
	// upstream repo with two commits; clone at the first.
	up := gitRepo(t)
	os.WriteFile(filepath.Join(up, "f.txt"), []byte("1"), 0o644)
	run := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run(up, "add", ".")
	run(up, "commit", "-qm", "c1")
	clone := t.TempDir()
	run(up, "worktree", "list") // ensure repo valid
	cmd := exec.Command("git", "clone", "-q", up, clone)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone: %v %s", err, out)
	}
	os.WriteFile(filepath.Join(up, "f.txt"), []byte("2"), 0o644)
	run(up, "add", ".")
	run(up, "commit", "-qm", "c2")
	run(clone, "fetch", "-q")

	items := scanGit([]string{clone})
	var found bool
	for _, it := range items {
		if strings.Contains(it.Title, "behind upstream by 1") {
			found = true
			if it.Task == "" || it.Dir != clone {
				t.Fatalf("bad item: %+v", it)
			}
		}
	}
	if !found {
		t.Fatalf("no behind-upstream item: %+v", items)
	}
}

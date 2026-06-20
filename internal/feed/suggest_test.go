package feed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSuggestionsLenientAndValidated(t *testing.T) {
	dirs := []string{"/home/u/proj-a"}
	out := "Here you go:\n[{\"title\":\"proj-a: add regression test\",\"detail\":\"bug fixed, no test\",\"dir\":\"/home/u/proj-a\",\"task\":\"Write the regression test for the fix in commit abc; run it; show me the diff.\"}," +
		"{\"title\":\"\",\"detail\":\"no title\",\"dir\":\"/home/u/proj-a\",\"task\":\"x\"}," +
		"{\"title\":\"hallucinated dir\",\"detail\":\"\",\"dir\":\"/evil/path\",\"task\":\"do a thing\"}]\nthanks"
	items := parseSuggestions(out, dirs)
	if len(items) != 2 {
		t.Fatalf("want 2 valid items (empty-title dropped), got %d: %+v", len(items), items)
	}
	if items[0].Kind != "suggest" || items[0].Dir != "/home/u/proj-a" {
		t.Fatalf("first item wrong: %+v", items[0])
	}
	// A dir the scanner didn't provide must not be trusted.
	if items[1].Dir != "" {
		t.Fatalf("hallucinated dir should be cleared, got %q", items[1].Dir)
	}
}

func TestParseSuggestionsGarbage(t *testing.T) {
	for _, out := range []string{"", "no json here", "[not valid", "{}"} {
		if items := parseSuggestions(out, nil); len(items) != 0 {
			t.Fatalf("garbage %q should yield nothing, got %+v", out, items)
		}
	}
	if items := parseSuggestions("[]", nil); len(items) != 0 {
		t.Fatal("empty array should yield nothing")
	}
}

func TestParseSuggestionsCaps(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < 6; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"title":"t%d","detail":"d","dir":"","task":"task %d"}`, i, i)
	}
	sb.WriteString("]")
	if items := parseSuggestions(sb.String(), nil); len(items) != maxSuggestItems {
		t.Fatalf("want cap %d, got %d", maxSuggestItems, len(items))
	}
}

func TestScanSuggestNilSuggester(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if items := scanSuggest(context.Background(), []string{t.TempDir()}, nil); items != nil {
		t.Fatal("nil suggester must disable the source")
	}
}

func TestScanSuggestCanceledSkipsModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := gitRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	s := func(context.Context, string, string) (string, error) {
		called = true
		return `[]`, nil
	}
	if items := scanSuggest(ctx, []string{dir}, s); len(items) != 0 || called {
		t.Fatalf("canceled scan should skip the model (items=%d called=%v)", len(items), called)
	}
}

func TestScanSuggestEndToEnd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := gitRepo(t)
	// One commit so the context has something to show.
	writeAndCommit(t, dir, "a.txt", "hello")
	var gotSystem, gotPrompt string
	s := func(_ context.Context, system, prompt string) (string, error) {
		gotSystem, gotPrompt = system, prompt
		return `[{"title":"x: follow up","detail":"d","dir":"` + dir + `","task":"do the follow-up"}]`, nil
	}
	items := scanSuggest(context.Background(), []string{dir}, s)
	if len(items) != 1 || items[0].Kind != "suggest" || items[0].Dir != dir {
		t.Fatalf("items: %+v", items)
	}
	if !strings.Contains(gotPrompt, dir) || !strings.Contains(gotPrompt, "recent commits") {
		t.Fatalf("prompt should carry project context:\n%s", gotPrompt)
	}
	if !strings.Contains(gotSystem, "JSON array") {
		t.Fatal("system should carry the JSON contract")
	}
}

// writeAndCommit writes a file and commits it in dir.
func writeAndCommit(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "add " + name}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestScanSuggestModelErrorIsolated(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := gitRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello")
	s := func(context.Context, string, string) (string, error) { return "", fmt.Errorf("boom") }
	if items := scanSuggest(context.Background(), []string{dir}, s); len(items) != 0 {
		t.Fatal("a failing model must yield nothing, not an error")
	}
}

func TestScanSuggestNoContextSkipsModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	called := false
	s := func(context.Context, string, string) (string, error) { called = true; return "[]", nil }
	// No git repos → no context → the model is never bothered.
	if items := scanSuggest(context.Background(), []string{t.TempDir()}, s); len(items) != 0 || called {
		t.Fatalf("no context should skip the model (called=%v)", called)
	}
}

func TestSuggestScore(t *testing.T) {
	if s := score(Item{Kind: "suggest"}); s <= 0 || s >= score(Item{Kind: "memory"}) {
		t.Fatalf("suggest should rank below memory but above nothing, got %d", s)
	}
}

func TestSuggestCacheReusedWhileFresh(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := gitRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello")
	calls := 0
	s := func(context.Context, string, string) (string, error) {
		calls++
		return `[{"title":"x: idea","detail":"d","dir":"` + dir + `","task":"do it"}]`, nil
	}
	// First scan: model called, cached.
	if items := scanSuggest(context.Background(), []string{dir}, s); len(items) != 1 || calls != 1 {
		t.Fatalf("first scan: items=%d calls=%d", len(items), calls)
	}
	// Second scan within the TTL: cache served, model NOT called again.
	if items := scanSuggest(context.Background(), []string{dir}, s); len(items) != 1 || calls != 1 {
		t.Fatalf("fresh cache should skip the model: items=%d calls=%d", len(items), calls)
	}
}

func TestSuggestStaleCacheBeatsFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := gitRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello")
	// Seed an EXPIRED cache directly.
	saveSuggestCache([]Item{{Kind: "suggest", Title: "old idea", Task: "t"}})
	var c suggestCache
	b, _ := os.ReadFile(suggestCachePath())
	_ = json.Unmarshal(b, &c)
	c.Scanned = time.Now().Add(-2 * suggestTTL)
	b, _ = json.Marshal(c)
	os.WriteFile(suggestCachePath(), b, 0o644)
	// The model fails → the stale cache is served rather than nothing.
	s := func(context.Context, string, string) (string, error) { return "", fmt.Errorf("model down") }
	items := scanSuggest(context.Background(), []string{dir}, s)
	if len(items) != 1 || items[0].Title != "old idea" {
		t.Fatalf("stale cache should be served on model failure: %+v", items)
	}
}

func TestReadmeIntro(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# revuto\n\n[![badge](x)](y)\n\nA PR review bot for GitHub.\nIt watches repos and reviews diffs.\n"), 0o644)
	got := readmeIntro(dir)
	if !strings.Contains(got, "review bot") || strings.Contains(got, "badge") {
		t.Fatalf("readmeIntro = %q", got)
	}
	if readmeIntro(t.TempDir()) != "" {
		t.Fatal("no README should yield empty")
	}
}

package feed

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	if items := scanSuggest([]string{t.TempDir()}, nil); items != nil {
		t.Fatal("nil suggester must disable the source")
	}
}

func TestScanSuggestEndToEnd(t *testing.T) {
	dir := gitRepo(t)
	// One commit so the context has something to show.
	writeAndCommit(t, dir, "a.txt", "hello")
	var gotSystem, gotPrompt string
	s := func(_ context.Context, system, prompt string) (string, error) {
		gotSystem, gotPrompt = system, prompt
		return `[{"title":"x: follow up","detail":"d","dir":"` + dir + `","task":"do the follow-up"}]`, nil
	}
	items := scanSuggest([]string{dir}, s)
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
	dir := gitRepo(t)
	writeAndCommit(t, dir, "a.txt", "hello")
	s := func(context.Context, string, string) (string, error) { return "", fmt.Errorf("boom") }
	if items := scanSuggest([]string{dir}, s); len(items) != 0 {
		t.Fatal("a failing model must yield nothing, not an error")
	}
}

func TestScanSuggestNoContextSkipsModel(t *testing.T) {
	called := false
	s := func(context.Context, string, string) (string, error) { called = true; return "[]", nil }
	// No git repos → no context → the model is never bothered.
	if items := scanSuggest([]string{t.TempDir()}, s); len(items) != 0 || called {
		t.Fatalf("no context should skip the model (called=%v)", called)
	}
}

func TestSuggestScore(t *testing.T) {
	if s := score(Item{Kind: "suggest"}); s <= 0 || s >= score(Item{Kind: "memory"}) {
		t.Fatalf("suggest should rank below memory but above nothing, got %d", s)
	}
}

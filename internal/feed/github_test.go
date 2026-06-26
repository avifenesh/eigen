package feed

import (
	"context"
	"strings"
	"testing"
)

func TestScanGitHubCanceledSkipsCommands(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ghCommandCount.Store(0)
	items := scanGitHub(ctx)
	if len(items) != 0 {
		t.Fatalf("canceled GitHub scan returned %d items", len(items))
	}
	if got := ghCommandCount.Load(); got != 0 {
		t.Fatalf("canceled GitHub scan should not start gh commands, got %d", got)
	}
}

func TestIsGHAuthError(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   bool
	}{
		{"not logged in", "To get started with GitHub CLI, please run:  gh auth login\n", true},
		{"explicit phrasing", "error: not logged in to any GitHub hosts\n", true},
		{"authentication required", "HTTP 401: Authentication required\n", true},
		{"requires authentication", "this command requires authentication\n", true},
		{"upper case marker", "Run GH AUTH LOGIN to authenticate.\n", true},
		{"unrelated network error", "error connecting to api.github.com: timeout\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := isGHAuthError([]byte(c.stderr)); got != c.want {
			t.Errorf("%s: isGHAuthError(%q) = %v, want %v", c.name, c.stderr, got, c.want)
		}
	}
}

func TestGHAuthItem(t *testing.T) {
	it := ghAuthItem()
	if it.Kind != "github" {
		t.Errorf("auth item Kind = %q, want %q", it.Kind, "github")
	}
	if it.Task == "" {
		t.Error("auth item Task is empty")
	}
	if !strings.Contains(it.Task, "gh auth login") {
		t.Errorf("auth item Task should mention `gh auth login`, got %q", it.Task)
	}
}

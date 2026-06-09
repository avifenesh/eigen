package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runTree(t *testing.T, dir string, args any) (string, error) {
	t.Helper()
	b, _ := json.Marshal(args)
	return Tree(NewPolicy(dir)).Run(context.Background(), b)
}

func TestTreeRendersLayout(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("x"), 0o644)
	}
	mk("src/main.go")
	mk("src/util.go")
	mk("README.md")
	mk(".git/config")
	mk("node_modules/pkg/index.js")

	out, err := runTree(t, dir, map[string]any{"path": dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"src/", "main.go", "util.go", "README.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("tree missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ".git") || strings.Contains(out, "node_modules") {
		t.Fatalf("tree should skip VCS/build dirs:\n%s", out)
	}
}

func TestTreeDepthLimit(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "a", "b", "c", "d")
	os.MkdirAll(deep, 0o755)
	os.WriteFile(filepath.Join(deep, "deep.txt"), []byte("x"), 0o644)

	out, err := runTree(t, dir, map[string]any{"path": dir, "depth": 2})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "deep.txt") {
		t.Fatalf("depth=2 should not reach deep.txt:\n%s", out)
	}
	if !strings.Contains(out, "a/") {
		t.Fatalf("depth=2 should show top-level dir:\n%s", out)
	}
}

func TestTreeIsReadOnly(t *testing.T) {
	if !Tree(NewPolicy(t.TempDir())).ReadOnly {
		t.Fatal("tree should be read-only")
	}
}

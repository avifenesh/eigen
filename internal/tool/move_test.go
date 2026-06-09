package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func runMove(t *testing.T, dir string, from, to string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	b, _ := json.Marshal(map[string]string{"from": from, "to": to})
	return Move(NewPolicy(dir)).Run(context.Background(), b)
}

func TestMoveRenamesFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)
	if _, err := runMove(t, dir, "a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Fatal("source should be gone")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "b.txt"))
	if string(got) != "hi" {
		t.Fatalf("dest content wrong: %q", got)
	}
}

func TestMoveCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)
	if _, err := runMove(t, dir, "a.txt", "sub/dir/b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "dir", "b.txt")); err != nil {
		t.Fatalf("dest not created: %v", err)
	}
}

func TestMoveMissingSource(t *testing.T) {
	dir := t.TempDir()
	if _, err := runMove(t, dir, "nope.txt", "b.txt"); err == nil {
		t.Fatal("moving a missing source should error")
	}
}

func TestMoveIsMutating(t *testing.T) {
	if Move(NewPolicy(t.TempDir())).ReadOnly {
		t.Fatal("move must be mutating (gated mode should prompt)")
	}
}

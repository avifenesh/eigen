package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@t.t"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v (%s)", err, out)
		}
	}
}

func TestDiffShowsWorkingTreeChanges(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	f := filepath.Join(dir, "f.txt")
	os.WriteFile(f, []byte("one\n"), 0o644)
	// commit, then modify
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	os.WriteFile(f, []byte("two\n"), 0o644)

	t.Chdir(dir)
	out, err := Diff(NewPolicy(dir)).Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-one") || !strings.Contains(out, "+two") {
		t.Fatalf("diff should show the change:\n%s", out)
	}
}

func TestDiffNoChanges(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	t.Chdir(dir)
	out, err := Diff(NewPolicy(dir)).Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no changes") {
		t.Fatalf("clean tree should report no changes, got %q", out)
	}
}

func TestDiffIsReadOnly(t *testing.T) {
	if !Diff(NewPolicy(t.TempDir())).ReadOnly {
		t.Fatal("diff should be read-only")
	}
}

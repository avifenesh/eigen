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

func TestDiffDisabledOutsideGitRepo(t *testing.T) {
	dir := t.TempDir()
	d := Diff(NewPolicy(dir))
	if !d.Disabled {
		t.Fatal("diff should be disabled outside repos")
	}
	reg, err := NewRegistry(d, Read(NewPolicy(dir)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("diff"); ok {
		t.Fatal("disabled diff should not be registered or advertised")
	}
	if _, ok := reg.Get("read"); !ok {
		t.Fatal("enabled tools should still register")
	}
	out, err := d.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "disabled") || !strings.Contains(out, "not inside a git repository") {
		t.Fatalf("non-git diff should return a disabled notice, got %q", out)
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
	if !diff(NewPolicy(t.TempDir()), false).ReadOnly {
		t.Fatal("diff should be read-only")
	}
}

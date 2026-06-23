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

// moveCrossDevice is the copy-then-remove fallback used when os.Rename returns
// EXDEV across mounts. EXDEV can't be forced portably in a unit test, so the
// fallback is exercised directly.
func TestMoveCrossDeviceFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.sh")
	dst := filepath.Join(dir, "out", "dst.sh")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := moveCrossDevice(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source should be gone after cross-device move")
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("dest not created: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o755 {
		t.Fatalf("dest mode = %o, want 0755 (executable bit must survive)", got)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "#!/bin/sh\n" {
		t.Fatalf("dest content wrong: %q", got)
	}
}

func TestMoveCrossDeviceDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	dst := filepath.Join(dir, "moved")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "f.txt"), []byte("deep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveCrossDevice(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source tree should be gone after cross-device move")
	}
	got, err := os.ReadFile(filepath.Join(dst, "sub", "f.txt"))
	if err != nil || string(got) != "deep" {
		t.Fatalf("nested file not copied: content=%q err=%v", got, err)
	}
	fi, _ := os.Stat(filepath.Join(dst, "sub", "f.txt"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("nested file mode = %o, want 0600", fi.Mode().Perm())
	}
}

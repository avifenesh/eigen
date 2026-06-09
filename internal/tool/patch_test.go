package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func runPatch(t *testing.T, dir, patch string) (string, error) {
	t.Helper()
	t.Chdir(dir) // diffs use project-relative paths, like production (cwd = root)
	b, _ := json.Marshal(map[string]string{"patch": patch})
	return Patch(NewPolicy(dir)).Run(context.Background(), b)
}

func TestPatchModifiesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0o644)

	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "one\nTWO\nthree\n" {
		t.Fatalf("got %q", got)
	}
}

func TestPatchAppliesByContextWhenLineDrifts(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	// File has extra leading lines so the hunk's stated start is wrong.
	os.WriteFile(p, []byte("zero\nextra\none\ntwo\nthree\n"), 0o644)

	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "zero\nextra\none\nTWO\nthree\n" {
		t.Fatalf("context match failed: %q", got)
	}
}

func TestPatchCreatesFile(t *testing.T) {
	dir := t.TempDir()
	patch := "--- /dev/null\n+++ b/new/created.txt\n@@ -0,0 +1,2 @@\n+hello\n+world\n"
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "new", "created.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\nworld\n" {
		t.Fatalf("created content wrong: %q", got)
	}
}

func TestPatchDeletesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gone.txt")
	os.WriteFile(p, []byte("bye\n"), 0o644)
	patch := "--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n-bye\n"
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}

func TestPatchMultiFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b1\n"), 0o644)
	patch := "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a1\n+A1\n" +
		"--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-b1\n+B1\n"
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(dir, "b.txt"))
	if string(a) != "A1\n" || string(b) != "B1\n" {
		t.Fatalf("multi-file patch failed: a=%q b=%q", a, b)
	}
}

func TestPatchAtomicOnFailure(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b1\n"), 0o644)
	// First file applies; second hunk's context is wrong → whole patch must abort.
	patch := "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a1\n+A1\n" +
		"--- a/b.txt\n+++ b/b.txt\n@@ -1 +1 @@\n-NOPE\n+B1\n"
	if _, err := runPatch(t, dir, patch); err == nil {
		t.Fatal("patch with a non-applying hunk should fail")
	}
	a, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(a) != "a1\n" {
		t.Fatalf("first file must be untouched on failure, got %q", a)
	}
}

func TestPatchEmptyErrors(t *testing.T) {
	if _, err := runPatch(t, t.TempDir(), "not a diff\n"); err == nil {
		t.Fatal("a patch with no file sections should error")
	}
}

func TestPatchIsMutating(t *testing.T) {
	if Patch(NewPolicy(t.TempDir())).ReadOnly {
		t.Fatal("apply_patch must be mutating (gated mode should prompt)")
	}
}

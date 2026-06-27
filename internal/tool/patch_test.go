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

func TestPatchAcceptsAgentUpdateFormat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: f.txt
@@
 one
-two
+TWO
 three
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "one\nTWO\nthree\n" {
		t.Fatalf("agent patch update failed: %q", got)
	}
}

func TestPatchAcceptsAgentUpdateFormatDeepInFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("header\nnoise\nkeep\nintermediate\nold\nend\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: f.txt
@@ keep
-old
+new
 end
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "header\nnoise\nkeep\nintermediate\nnew\nend\n" {
		t.Fatalf("deep agent patch update failed: %q", got)
	}
}

func TestPatchAgentAnchorsDisambiguateDuplicateContext(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("func first\nctx\nold\nend\nfunc second\nctx\nold\nend\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: f.txt
@@ func second
 ctx
-old
+new
 end
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "func first\nctx\nold\nend\nfunc second\nctx\nnew\nend\n" {
		t.Fatalf("anchor should select second duplicate block: %q", got)
	}
}

func TestPatchAcceptsAgentUpdateFormatDuplicateContext(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("block\nold\nend\nblock\nold\nend\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: f.txt
@@
 block
 old
-end
+done
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "block\nold\ndone\nblock\nold\nend\n" {
		t.Fatalf("duplicate-context agent patch picked wrong block or failed: %q", got)
	}
}

func TestPatchAcceptsAgentUpdateFormatMultipleHunks(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("a\nold-a\nmid\nb\nold-b\nz\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: f.txt
@@
 a
-old-a
+new-a
 mid
@@
 b
-old-b
+new-b
 z
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "a\nnew-a\nmid\nb\nnew-b\nz\n" {
		t.Fatalf("multi-hunk agent patch failed: %q", got)
	}
}

func TestPatchAgentHierarchicalAnchors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("class A\ndef target\nctx\nold\nclass B\ndef target\nctx\nold\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: f.txt
@@ class B
@@ def target
 ctx
-old
+new
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "class A\ndef target\nctx\nold\nclass B\ndef target\nctx\nnew\n" {
		t.Fatalf("hierarchical anchors should select class B target: %q", got)
	}
}

func TestPatchAgentDirectiveTextInUnifiedDiffStaysUnified(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "docs.txt")
	os.WriteFile(p, []byte("usage\n"), 0o644)

	patch := "--- a/docs.txt\n+++ b/docs.txt\n@@ -1 +1,2 @@\n usage\n+*** Begin Patch\n"
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "usage\n*** Begin Patch\n" {
		t.Fatalf("unified diff containing directive text should stay unified: %q", got)
	}
}

func TestPatchAgentContentCanContainDirectiveText(t *testing.T) {
	dir := t.TempDir()
	patch := `*** Begin Patch
*** Add File: literal.txt
+*** Begin Patch
+body
+*** End Patch
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "literal.txt"))
	if string(got) != "*** Begin Patch\nbody\n*** End Patch\n" {
		t.Fatalf("directive-looking added content should be literal: %q", got)
	}
}

func TestPatchAgentRejectsEscapingPaths(t *testing.T) {
	dir := t.TempDir()
	patch := `*** Begin Patch
*** Add File: ../outside.txt
+nope
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err == nil {
		t.Fatal("agent patch should reject paths outside policy roots")
	}
}

func TestPatchAgentCreateFailsIfFileExists(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "exists.txt"), []byte("keep\n"), 0o644)
	patch := `*** Begin Patch
*** Add File: exists.txt
+replace
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err == nil {
		t.Fatal("agent add-file should not overwrite existing files")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "exists.txt"))
	if string(got) != "keep\n" {
		t.Fatalf("existing file should remain untouched: %q", got)
	}
}

func TestPatchAcceptsAgentAddFileFormat(t *testing.T) {
	dir := t.TempDir()
	patch := `*** Begin Patch
*** Add File: new/created.txt
+hello
+world
*** End Patch
`
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

func TestPatchAcceptsAgentDeleteFileFormat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gone.txt")
	os.WriteFile(p, []byte("bye\n"), 0o644)
	patch := `*** Begin Patch
*** Delete File: gone.txt
*** End Patch
`
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

func TestPatchAgentDeleteBeforeFailureDoesNotMutate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("bye\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b1\n"), 0o644)
	patch := `*** Begin Patch
*** Delete File: gone.txt
*** Update File: b.txt
@@
-NOPE
+B1
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err == nil {
		t.Fatal("agent patch with delete followed by non-applying hunk should fail")
	}
	if got, err := os.ReadFile(filepath.Join(dir, "gone.txt")); err != nil || string(got) != "bye\n" {
		t.Fatalf("deleted file should remain untouched on failure, got %q err=%v", got, err)
	}
}

func TestPatchAgentAtomicOnFailure(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b1\n"), 0o644)
	patch := `*** Begin Patch
*** Update File: a.txt
@@
-a1
+A1
*** Update File: b.txt
@@
-NOPE
+B1
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err == nil {
		t.Fatal("agent patch with a non-applying hunk should fail")
	}
	a, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(a) != "a1\n" {
		t.Fatalf("first file must be untouched on failure, got %q", a)
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

func TestPatchAgentRenamesAndEdits(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.txt")
	os.WriteFile(src, []byte("one\ntwo\nthree\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: old.txt
*** Move to: new.txt
@@
 one
-two
+TWO
 three
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source file should be removed after rename")
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("renamed file should exist: %v", err)
	}
	if string(got) != "one\nTWO\nthree\n" {
		t.Fatalf("rename+edit produced wrong content: %q", got)
	}
}

func TestPatchAgentPureRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.txt")
	os.WriteFile(src, []byte("one\ntwo\n"), 0o644)

	patch := `*** Begin Patch
*** Update File: old.txt
*** Move to: new.txt
*** End Patch
`
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source file should be removed after pure rename")
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("renamed file should exist: %v", err)
	}
	if string(got) != "one\ntwo\n" {
		t.Fatalf("pure rename changed content: %q", got)
	}
}

func TestPatchRenamesViaUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.txt")
	os.WriteFile(src, []byte("one\ntwo\nthree\n"), 0o644)

	patch := "--- a/old.txt\n+++ b/new.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source file should be removed after rename")
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("renamed file should exist: %v", err)
	}
	if string(got) != "one\nTWO\nthree\n" {
		t.Fatalf("rename+edit produced wrong content: %q", got)
	}
}

func TestPatchPureRenameViaUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "old.txt")
	os.WriteFile(src, []byte("one\ntwo\n"), 0o644)

	patch := "--- a/old.txt\n+++ b/new.txt\n"
	if _, err := runPatch(t, dir, patch); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source file should be removed after pure rename")
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("renamed file should exist: %v", err)
	}
	if string(got) != "one\ntwo\n" {
		t.Fatalf("pure unified rename changed content: %q", got)
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

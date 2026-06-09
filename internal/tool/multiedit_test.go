package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func runMultiEdit(t *testing.T, dir string, args any) (string, error) {
	t.Helper()
	def := MultiEdit(NewPolicy(dir))
	b, _ := json.Marshal(args)
	return def.Run(context.Background(), b)
}

func TestMultiEditAppliesAllInOrder(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("alpha beta gamma"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := runMultiEdit(t, dir, map[string]any{
		"path": p,
		"edits": []map[string]any{
			{"old_string": "alpha", "new_string": "ALPHA"},
			{"old_string": "gamma", "new_string": "GAMMA"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "ALPHA beta GAMMA" {
		t.Fatalf("got %q", got)
	}
}

func TestMultiEditIsAtomicOnFailure(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	original := "alpha beta gamma"
	if err := os.WriteFile(p, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	// Second edit fails (no match) → nothing should be written.
	_, err := runMultiEdit(t, dir, map[string]any{
		"path": p,
		"edits": []map[string]any{
			{"old_string": "alpha", "new_string": "ALPHA"},
			{"old_string": "nope", "new_string": "x"},
		},
	})
	if err == nil {
		t.Fatal("expected failure on the unmatched second edit")
	}
	got, _ := os.ReadFile(p)
	if string(got) != original {
		t.Fatalf("file must be unchanged on failure, got %q", got)
	}
}

func TestMultiEditSequentialDependentEdits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Each edit operates on the result of the previous one.
	_, err := runMultiEdit(t, dir, map[string]any{
		"path": p,
		"edits": []map[string]any{
			{"old_string": "one", "new_string": "two"},
			{"old_string": "two", "new_string": "three"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "three" {
		t.Fatalf("sequential edits failed, got %q", got)
	}
}

func TestMultiEditRequiresEdits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runMultiEdit(t, dir, map[string]any{"path": p, "edits": []map[string]any{}}); err == nil {
		t.Fatal("empty edits must error")
	}
}

func TestMultiEditNonUniqueWithoutReplaceAll(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(p, []byte("x x x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runMultiEdit(t, dir, map[string]any{
		"path":  p,
		"edits": []map[string]any{{"old_string": "x", "new_string": "y"}},
	}); err == nil {
		t.Fatal("ambiguous old_string must error without replace_all")
	}
	// replace_all makes it succeed.
	if _, err := runMultiEdit(t, dir, map[string]any{
		"path":  p,
		"edits": []map[string]any{{"old_string": "x", "new_string": "y", "replace_all": true}},
	}); err != nil {
		t.Fatalf("replace_all should succeed: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "y y y" {
		t.Fatalf("got %q", got)
	}
}

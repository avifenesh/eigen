package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mustArgs(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestWriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	w := Write(NewPolicy(dir))
	target := filepath.Join(dir, "sub", "hello.txt")

	if _, err := w.Run(context.Background(), mustArgs(t, map[string]any{"path": target, "content": "hi"})); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Fatalf("got %q, want %q", got, "hi")
	}
}

func TestWriteDeniedOutsideRoot(t *testing.T) {
	w := Write(NewPolicy(t.TempDir()))
	if _, err := w.Run(context.Background(), mustArgs(t, map[string]any{"path": "/tmp/eigen-escape.txt", "content": "x"})); err == nil {
		t.Fatal("expected denial writing outside root")
	}
}

func TestWriteIsMutating(t *testing.T) {
	if Write(NewPolicy(t.TempDir())).ReadOnly {
		t.Fatal("write must not be read-only")
	}
}

func TestEditUniqueAndNonUnique(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("alpha beta alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := Edit(NewPolicy(dir))

	// Non-unique without replace_all -> error, file unchanged.
	if _, err := e.Run(context.Background(), mustArgs(t, map[string]any{
		"path": f, "old_string": "alpha", "new_string": "X",
	})); err == nil {
		t.Fatal("expected non-unique error")
	}
	if b, _ := os.ReadFile(f); string(b) != "alpha beta alpha" {
		t.Fatalf("file should be unchanged, got %q", b)
	}

	// replace_all -> both replaced.
	if _, err := e.Run(context.Background(), mustArgs(t, map[string]any{
		"path": f, "old_string": "alpha", "new_string": "X", "replace_all": true,
	})); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(f); string(b) != "X beta X" {
		t.Fatalf("got %q, want %q", b, "X beta X")
	}
}

func TestEditMissingString(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := Edit(NewPolicy(dir))
	if _, err := e.Run(context.Background(), mustArgs(t, map[string]any{
		"path": f, "old_string": "absent", "new_string": "x",
	})); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestBashRunsAndIsMutating(t *testing.T) {
	b := Bash()
	if b.ReadOnly {
		t.Fatal("bash must not be read-only")
	}
	out, err := b.Run(context.Background(), mustArgs(t, map[string]any{"command": "echo eigen-bash-ok"}))
	if err != nil {
		t.Fatal(err)
	}
	if want := "eigen-bash-ok"; !contains(out, want) {
		t.Fatalf("got %q, want it to contain %q", out, want)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

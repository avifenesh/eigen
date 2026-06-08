package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readArgs(t *testing.T, path string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadReturnsContents(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rd := Read(NewPolicy(dir))
	out, err := rd.Run(context.Background(), readArgs(t, f))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("got %q, want %q", out, "hello")
	}
}

func TestReadTruncates(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(f, []byte(strings.Repeat("a", maxReadBytes+10)), 0o644); err != nil {
		t.Fatal(err)
	}
	rd := Read(NewPolicy(dir))
	out, err := rd.Run(context.Background(), readArgs(t, f))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "[truncated]") {
		t.Fatal("expected truncation marker")
	}
}

func TestReadErrors(t *testing.T) {
	dir := t.TempDir()
	rd := Read(NewPolicy(dir))

	if _, err := rd.Run(context.Background(), readArgs(t, "")); err == nil {
		t.Error("expected error on empty path")
	}
	if _, err := rd.Run(context.Background(), readArgs(t, filepath.Join(dir, "nope.txt"))); err == nil {
		t.Error("expected error on missing file")
	}
	if _, err := rd.Run(context.Background(), readArgs(t, "/etc/hosts")); err == nil {
		t.Error("expected error on denied path")
	}
}

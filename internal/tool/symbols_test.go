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

func runSymbols(t *testing.T, dir string, args any) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep not installed")
	}
	b, _ := json.Marshal(args)
	return Symbols(NewPolicy(dir)).Run(context.Background(), b)
}

func TestSymbolsFindsDefinitions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\nfunc DoThing() {}\ntype Widget struct{}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.py"), []byte("def do_thing():\n    pass\n"), 0o644)

	out, err := runSymbols(t, dir, map[string]any{"name": "DoThing", "path": dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "func DoThing") {
		t.Fatalf("should find the Go func definition:\n%s", out)
	}
}

func TestSymbolsNoMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n"), 0o644)
	out, err := runSymbols(t, dir, map[string]any{"name": "Nonexistent", "path": dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no definitions") {
		t.Fatalf("expected no-match message, got %q", out)
	}
}

func TestSymbolsRequiresName(t *testing.T) {
	if _, err := runSymbols(t, t.TempDir(), map[string]any{"name": ""}); err == nil {
		t.Fatal("empty name should error")
	}
}

func TestSymbolsIsReadOnly(t *testing.T) {
	if !Symbols(NewPolicy(t.TempDir())).ReadOnly {
		t.Fatal("symbols should be read-only")
	}
}

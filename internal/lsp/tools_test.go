package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadToolsMissingConfig(t *testing.T) {
	defs, mgr, errs := LoadTools(t.TempDir(), filepath.Join(t.TempDir(), "none.json"))
	if defs != nil || mgr != nil || errs != nil {
		t.Fatalf("missing config should yield nothing: defs=%v mgr=%v errs=%v", defs, mgr, errs)
	}
}

func TestLoadToolsParsesServers(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"servers":[
		{"name":"gopls","command":["gopls"],"extensions":[".go"]},
		{"name":"bad"}
	]}`
	path := filepath.Join(dir, "lsp.json")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	defs, mgr, errs := LoadTools(dir, path)
	if mgr == nil {
		t.Fatal("expected a manager")
	}
	// The 5 LSP tools.
	if len(defs) != 5 {
		t.Fatalf("expected 5 tools, got %d", len(defs))
	}
	// The invalid "bad" server is reported.
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for the invalid server, got %v", errs)
	}
	// Tool names + read-only.
	names := map[string]bool{}
	for _, d := range defs {
		names[d.Name] = true
		if !d.ReadOnly {
			t.Errorf("LSP tool %q should be read-only", d.Name)
		}
	}
	for _, want := range []string{"lsp_definition", "lsp_references", "lsp_hover", "lsp_symbols", "lsp_diagnostics"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestManagerConfigFor(t *testing.T) {
	mgr := NewManager("/proj", []ServerConfig{
		{Name: "gopls", Command: []string{"gopls"}, Extensions: []string{".go"}},
		{Name: "ts", Command: []string{"typescript-language-server", "--stdio"}, Extensions: []string{".ts", ".tsx"}},
	})
	if c, ok := mgr.configFor("a/b/main.go"); !ok || c.Name != "gopls" {
		t.Fatalf("main.go should map to gopls, got %+v ok=%v", c, ok)
	}
	if c, ok := mgr.configFor("x.TSX"); !ok || c.Name != "ts" {
		t.Fatalf(".TSX (case-insensitive) should map to ts, got %+v ok=%v", c, ok)
	}
	if _, ok := mgr.configFor("README.md"); ok {
		t.Fatal(".md should not map to any server")
	}
}

func TestClientForUnconfiguredExtension(t *testing.T) {
	mgr := NewManager("/proj", []ServerConfig{
		{Name: "gopls", Command: []string{"gopls"}, Extensions: []string{".go"}},
	})
	_, _, err := mgr.clientFor(context.Background(), "notes.md")
	if err == nil || !strings.Contains(err.Error(), "no language server configured") {
		t.Fatalf("expected an unconfigured-extension error, got %v", err)
	}
}

func TestDefinitionToolUnconfigured(t *testing.T) {
	// A tool over a manager with no matching server returns a clear error
	// instead of trying to start anything.
	mgr := NewManager(t.TempDir(), []ServerConfig{
		{Name: "gopls", Command: []string{"gopls"}, Extensions: []string{".go"}},
	})
	def := definitionTool(mgr)
	args, _ := json.Marshal(map[string]any{"path": "x.md", "line": 1, "character": 1})
	if _, err := def.Run(context.Background(), args); err == nil {
		t.Fatal("definition on an unconfigured extension should error")
	}
}

func TestPosArgsConversion(t *testing.T) {
	// 1-based human input → 0-based LSP positions, clamped at 0.
	p := posArgs{Line: 1, Character: 1}.position()
	if p.Line != 0 || p.Character != 0 {
		t.Fatalf("1:1 should map to 0,0, got %+v", p)
	}
	p = posArgs{Line: 42, Character: 7}.position()
	if p.Line != 41 || p.Character != 6 {
		t.Fatalf("42:7 should map to 41,6, got %+v", p)
	}
	p = posArgs{Line: 0, Character: 0}.position()
	if p.Line != 0 || p.Character != 0 {
		t.Fatalf("0:0 should clamp to 0,0, got %+v", p)
	}
}

func TestFormatLocations(t *testing.T) {
	out := formatLocations([]Location{
		{URI: PathToURI("/proj/a.go"), Range: Range{Start: Position{Line: 0, Character: 0}}},
		{URI: PathToURI("/proj/b.go"), Range: Range{Start: Position{Line: 9, Character: 4}}},
	})
	if !strings.Contains(out, ":1:1") || !strings.Contains(out, ":10:5") {
		t.Fatalf("locations should be 1-based file:line:col, got:\n%s", out)
	}
}

package tui

import (
	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestLooksLikeJSON(t *testing.T) {
	yes := []string{`{"a":1}`, `[1,2,3]`, "  {\n\"x\": true}\n", `{"nested":{"k":[1,2]}}`}
	no := []string{"", "hello", "42", `"just a string"`, "{not json}", "{unclosed"}
	for _, s := range yes {
		if !looksLikeJSON(s) {
			t.Errorf("should be JSON: %q", s)
		}
	}
	for _, s := range no {
		if looksLikeJSON(s) {
			t.Errorf("should NOT be JSON: %q", s)
		}
	}
}

func TestRenderJSONPrettyPrints(t *testing.T) {
	in := `{"name":"opus","ctx":200000,"vision":true,"tags":["a","b"]}`
	out := renderJSON(in, nil)
	plain := ansi.Strip(out)
	// Pretty-printed: indented, multi-line, all values present.
	if !strings.Contains(plain, "\n") {
		t.Fatalf("should be multi-line:\n%s", plain)
	}
	for _, want := range []string{`"name"`, `"opus"`, "200000", "true", `"tags"`, `"a"`} {
		if !strings.Contains(plain, want) {
			t.Errorf("missing %q in:\n%s", want, plain)
		}
	}
	// Round-trips structurally (no dropped content vs compact re-indent).
	if strings.Count(plain, "\"") < strings.Count(in, "\"") {
		t.Errorf("lost quoted tokens:\n%s", plain)
	}
}

func TestRenderJSONFallsBackOnBadInput(t *testing.T) {
	// Not valid JSON → returned unchanged (caller guards with looksLikeJSON, but
	// be safe).
	in := "{not valid"
	if out := renderJSON(in, nil); ansi.Strip(out) != in {
		t.Errorf("bad JSON should pass through: %q -> %q", in, ansi.Strip(out))
	}
}

func TestCodeResultFramedAndTinted(t *testing.T) {
	// A `read` of a .go file renders as a framed, surface-filled code block.
	b := &block{kind: blockTool, toolName: "read",
		toolArgs: []byte(`{"path":"greet.go"}`), state: toolDone,
		result: "package main\n\nfunc main() {}", wrapW: 70}
	out := b.codeResult(b.result)
	if out == "" {
		t.Fatal("read of a .go file should render as a code block")
	}
	if !strings.Contains(out, bgSeq(surfaceHex(themeSurface()))) {
		t.Error("code result should be filled on the Surface tint")
	}
	if !strings.Contains(ansiStrip(out), "go") || !strings.Contains(ansiStrip(out), "package main") {
		t.Errorf("code result should show the lang chip + content:\n%s", ansiStrip(out))
	}
	// A non-code result (bash log) does NOT get the treatment.
	bash := &block{kind: blockTool, toolName: "bash", state: toolDone, result: "PASS", wrapW: 70}
	if bash.codeResult(bash.result) != "" {
		t.Error("bash output should NOT be rendered as a code block")
	}
	// A read of a non-code file is left plain.
	txt := &block{kind: blockTool, toolName: "read", toolArgs: []byte(`{"path":"notes.txt"}`), state: toolDone, result: "hello", wrapW: 70}
	if txt.codeResult(txt.result) != "" {
		t.Error(".txt read should not be a code block")
	}
}

func themeSurface() lipgloss.AdaptiveColor { return theme.Surface }
func ansiStrip(s string) string            { return ansi.Strip(s) }

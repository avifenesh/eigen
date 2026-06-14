package tui

import (
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

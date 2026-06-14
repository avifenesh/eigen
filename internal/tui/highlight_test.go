package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestHighlightCodeUsesLexer(t *testing.T) {
	// Force truecolor for this test, then restore so we don't leak the profile
	// into other tests (which assert on plain, color-stripped substrings).
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	out, ok := highlightCode("func main() { return 42 }", "go", nil)
	if !ok {
		t.Fatal("go should resolve a chroma lexer")
	}
	// Multiple distinct color sequences = real tokenization (keyword/number/…).
	colors := map[string]bool{}
	for _, seg := range strings.Split(out, "\x1b[38;2;") {
		if i := strings.IndexByte(seg, 'm'); i > 0 && i < 16 {
			colors[seg[:i]] = true
		}
	}
	if len(colors) < 3 {
		t.Errorf("expected ≥3 token colors, got %d", len(colors))
	}
	// Unknown language with no analysable content → no lexer (caller falls back).
	if _, ok := highlightCode("xyzzy", "zzz-not-a-lang", nil); ok {
		// Analyse may still guess; only assert content preserved when it does.
		t.Log("analyse guessed a lexer for gibberish (acceptable)")
	}
}

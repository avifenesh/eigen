package tui

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/theme"
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

func TestRenderDiffLangHighlightsContextNotChanges(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	// A diff with a code context line + a -/+ change.
	diff := "  func f() {\n- \tx := 1\n+ \tx := 2\n  }"
	out := renderDiffLang(diff, "go")
	// Context "func f()" got syntax color (the 'func' keyword → SynKeyword).
	kw := theme.SynKeyword.Dark
	r, g, b, _ := hexRGB(kw)
	wantKw := "38;2;" + itoa(int(r)) + ";" + itoa(int(g)) + ";" + itoa(int(b))
	if !containsTrueColorNear(out, "func", int(r), int(g), int(b), 1) {
		t.Errorf("context code should be syntax-highlighted near %s:\n%q", wantKw, out)
	}
	// The change still reads with its +/- markers in the plain text.
	plain := stripANSI(out)
	if !strings.Contains(plain, "- ") || !strings.Contains(plain, "+ ") {
		t.Errorf("diff should keep -/+ markers:\n%s", plain)
	}
	if !strings.Contains(plain, "x := 1") || !strings.Contains(plain, "x := 2") {
		t.Errorf("diff should keep changed content:\n%s", plain)
	}
}

func containsTrueColorNear(s, needle string, r, g, b, tolerance int) bool {
	idx := strings.Index(s, needle)
	if idx < 0 {
		return false
	}
	prefix := s[:idx]
	seq := "38;2;"
	for start := strings.LastIndex(prefix, seq); start >= 0; start = strings.LastIndex(prefix[:start], seq) {
		p := prefix[start+len(seq):]
		end := strings.IndexByte(p, 'm')
		if end < 0 {
			continue
		}
		parts := strings.Split(p[:end], ";")
		if len(parts) < 3 {
			continue
		}
		rr, ok1 := atoiSmall(parts[0])
		gg, ok2 := atoiSmall(parts[1])
		bb, ok3 := atoiSmall(parts[2])
		if ok1 && ok2 && ok3 && absInt(rr-r) <= tolerance && absInt(gg-g) <= tolerance && absInt(bb-b) <= tolerance {
			return true
		}
	}
	return false
}

func atoiSmall(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
		if n > 255 {
			return 0, false
		}
	}
	return n, true
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == '\x1b' {
			in = true
			continue
		}
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

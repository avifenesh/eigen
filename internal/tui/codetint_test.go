package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestTintCodePreservesContent(t *testing.T) {
	base := lipgloss.NewStyle()
	in := `func main() { return "hi" } // note`
	out := tintCodeLine(in, base)
	// Plain text (ANSI stripped) is unchanged — tinting must not drop chars.
	if got := ansi.Strip(out); got != in {
		t.Fatalf("tint changed content:\n in: %q\nout: %q", in, got)
	}
}

func TestSplitComment(t *testing.T) {
	code, com := splitComment(`x := 1 // hello`)
	if strings.TrimSpace(code) != "x := 1" || com != "// hello" {
		t.Fatalf("split: code=%q com=%q", code, com)
	}
	// A // inside a string is NOT a comment.
	code, com = splitComment(`url := "http://x"`)
	if com != "" {
		t.Fatalf("// inside a string should not split: code=%q com=%q", code, com)
	}
}

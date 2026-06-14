package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFillBGReassertsAfterReset(t *testing.T) {
	// A fragment with an inner reset must not tear a hole in the surface bg.
	frag := "\x1b[38;2;62;158;150mλ eigen\x1b[0m   tail"
	out := fillBG(frag, "#11171A", 20)
	bg := bgSeq("#11171A")
	// Every reset inside is followed by a re-asserted bg.
	for _, seg := range strings.Split(out, "\x1b[0m") {
		_ = seg
	}
	// Count: each \x1b[0m (except the final terminator) is immediately followed
	// by the bg sequence.
	if !strings.Contains(out, "\x1b[0m"+bg) {
		t.Fatal("bg not re-asserted after reset")
	}
	// Display width is exactly 20 (padding applied, bg ignored by StringWidth).
	if w := ansi.StringWidth(out); w != 20 {
		t.Errorf("filled width = %d, want 20", w)
	}
	// Starts with bg, ends with a reset.
	if !strings.HasPrefix(out, bg) {
		t.Error("should start with the bg sequence")
	}
	if !strings.HasSuffix(out, "\x1b[0m") {
		t.Error("should end with a reset")
	}
}

func TestFillBGReassertsAfterShorthandReset(t *testing.T) {
	// lipgloss emits the EMPTY-param reset \x1b[m (not just \x1b[0m) for wrapped
	// / styled lines — e.g. "…word\x1b[m   " with trailing pad spaces. fillBG
	// must re-assert the bg after \x1b[m too, or those spaces show the terminal
	// background (the wrapped-line "empty space" leak).
	frag := "\x1b[38;2;221;227;227malpha beta\x1b[m   " // shorthand reset + pad
	bg := bgSeq("#0B0E0F")
	out := fillBG(frag, "#0B0E0F", 20)
	if !strings.Contains(out, "\x1b[m"+bg) {
		t.Fatalf("bg not re-asserted after the \\x1b[m shorthand reset:\n%q", out)
	}
	if w := ansi.StringWidth(out); w != 20 {
		t.Errorf("filled width = %d, want 20", w)
	}
}

func TestHexRGB(t *testing.T) {
	r, g, b, ok := hexRGB("#11171A")
	if !ok || r != 0x11 || g != 0x17 || b != 0x1A {
		t.Fatalf("hexRGB(#11171A) = %d,%d,%d ok=%v", r, g, b, ok)
	}
	if _, _, _, ok := hexRGB("nope"); ok {
		t.Error("invalid hex should fail")
	}
}

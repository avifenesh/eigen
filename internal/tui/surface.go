package tui

import (
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Surface rendering — the "construction"/elevation the design system wants
// (base → surface → overlay). Painting a background behind already-styled
// content is subtle in a terminal: a fragment's reset (\x1b[0m) clears the
// outer background mid-line, leaving gaps. fillBG re-asserts the background
// after every reset so the tint runs edge-to-edge across the whole row,
// regardless of the fg styling inside.

// bgSeq returns the truecolor background SGR for a hex color (e.g. "#11171A").
func bgSeq(hex string) string {
	r, g, b, ok := hexRGB(hex)
	if !ok {
		return ""
	}
	return "\x1b[48;2;" + itoa(int(r)) + ";" + itoa(int(g)) + ";" + itoa(int(b)) + "m"
}

func hexRGB(hex string) (r, g, b uint8, ok bool) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	v := func(c byte) (uint8, bool) {
		switch {
		case c >= '0' && c <= '9':
			return c - '0', true
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10, true
		case c >= 'A' && c <= 'F':
			return c - 'A' + 10, true
		}
		return 0, false
	}
	hi, ok1 := v(hex[0])
	lo, ok2 := v(hex[1])
	r = hi<<4 | lo
	hi, ok3 := v(hex[2])
	lo, ok4 := v(hex[3])
	g = hi<<4 | lo
	hi, ok5 := v(hex[4])
	lo, ok6 := v(hex[5])
	b = hi<<4 | lo
	return r, g, b, ok1 && ok2 && ok3 && ok4 && ok5 && ok6
}

// fillBG paints content on a surface background of the given hex, padded (or
// truncated) to width display columns, with the background re-asserted after
// every reset so inner fg styling never tears a hole in the tint. width<=0
// leaves the content's own width (still bg-filled, no padding).
func fillBG(content, hex string, width int) string {
	bg := bgSeq(hex)
	if bg == "" {
		return content
	}
	if width > 0 {
		w := ansi.StringWidth(content)
		if w > width {
			content = ansi.Truncate(content, width, "")
		} else if w < width {
			content += strings.Repeat(" ", width-w)
		}
	}
	// Re-assert the bg immediately after each reset so padding + post-reset
	// runs keep the tint. (lipgloss emits \x1b[0m for resets.)
	content = strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+bg)
	return bg + content + "\x1b[0m"
}

// onSurface is the dark-resolved hex for a theme surface role, for fillBG.
// (fillBG needs a hex string, not an AdaptiveColor; we're dark-only for now.)
func surfaceHex(c lipgloss.AdaptiveColor) string { return c.Dark }

// paintBase guarantees every line of a composed view sits on the Base canvas,
// padded to the full width. Terminals with a non-black background (e.g. ghostty
// with background-opacity / a custom background) show their OWN color in any
// cell eigen leaves unpainted — so an unpainted region (the input row, a short
// status line, a gap) glows as an "exposed background" hole. Running the final
// View through this paints every cell, so eigen owns the whole rectangle and no
// terminal background leaks through. Rows that already carry their own bg
// spans (rail/panel/code) keep them — Base is only the foundation/padding.
func paintBase(view string, width int) string {
	if width <= 0 {
		return view
	}
	hex := surfaceHex(theme.Base)
	lines := strings.Split(view, "\n")
	for i, ln := range lines {
		lines[i] = fillBG(ln, hex, width)
	}
	return strings.Join(lines, "\n")
}

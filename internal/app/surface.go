package app

import (
	"strconv"
	"strings"

	"github.com/avifenesh/eigen/internal/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// App-surface painting mirrors the chat TUI canvas contract: Eigen owns the
// whole terminal rectangle, so transparent/custom terminal backgrounds never
// leak through gaps in the title bar, gutters, short rows, or status line.

func bgSeq(hex string) string {
	r, g, b, ok := hexRGB(hex)
	if !ok {
		return ""
	}
	return "\x1b[48;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b)) + "m"
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

func surfaceHex(c lipgloss.AdaptiveColor) string { return c.Dark }

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
	content = strings.ReplaceAll(content, "\x1b[0m", "\x1b[0m"+bg)
	content = strings.ReplaceAll(content, "\x1b[m", "\x1b[m"+bg)
	return bg + content + "\x1b[0m"
}

func paintBase(view string, width, height int) string {
	if width <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	hex := surfaceHex(theme.Base)
	for i, ln := range lines {
		lines[i] = fillBG(ln, hex, width)
	}
	return strings.Join(lines, "\n")
}

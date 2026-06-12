package tui

// Shared panel chrome for Tier 11: titled side panels with a visible clickable
// close affordance. The panel frame is intentionally light (a title line +
// existing separator/gutter) so it composes with the current transcript-band
// layout without changing vertical geometry yet. It is the first minimal slice
// of the reviewed Tier 11 plan: close/reopen controls + keyboard parity before
// tabbed content.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// panelCloseGlyph is the visible mouse target used by both side panels.
const panelCloseGlyph = "[x]"

// panelTitleLine renders a one-line panel header padded to width. The close
// affordance is right-aligned when close=true. Width is display columns.
func panelTitleLine(title string, width int, close bool) string {
	if width <= 0 {
		return ""
	}
	left := styleAccent.Render(ansiTrunc(title, width))
	right := ""
	if close {
		right = dim(panelCloseGlyph)
	}
	lw := ansi.StringWidth(ansi.Strip(left))
	rw := ansi.StringWidth(ansi.Strip(right))
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	// Clamp to width in case an overlong title plus close overflowed.
	if ansi.StringWidth(ansi.Strip(line)) > width {
		plainTitleWidth := width - rw - 1
		if plainTitleWidth < 0 {
			plainTitleWidth = 0
		}
		line = styleAccent.Render(ansiTrunc(title, plainTitleWidth)) + " " + right
	}
	return line
}

// panelCloseAt reports whether local (x,y) is on the right-aligned [x] in a
// panel's title row.
func panelCloseAt(localX, localY, width int) bool {
	if localY != 0 || width <= 0 {
		return false
	}
	start := width - ansi.StringWidth(panelCloseGlyph)
	return localX >= start && localX < width
}

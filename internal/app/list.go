package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// list is a minimal selectable list with j/k movement and a window that
// follows the cursor. Pages embed it for consistent navigation.
type list struct {
	cursor int
	count  int
	top    int // first visible row
}

func (l *list) clamp() {
	if l.cursor >= l.count {
		l.cursor = l.count - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
}

func (l *list) move(d, visible int) {
	l.cursor += d
	l.clamp()
	if l.cursor < l.top {
		l.top = l.cursor
	}
	if visible > 0 && l.cursor >= l.top+visible {
		l.top = l.cursor - visible + 1
	}
}

// handles j/k/up/down/g/G; returns true when consumed.
func (l *list) key(key string, visible int) bool {
	switch key {
	case "j", "down":
		l.move(1, visible)
	case "k", "up":
		l.move(-1, visible)
	case "G", "end":
		l.cursor = l.count - 1
		l.clamp()
		if visible > 0 && l.cursor >= l.top+visible {
			l.top = l.cursor - visible + 1
		}
	case "home":
		l.cursor, l.top = 0, 0
	case "ctrl+d", "pgdown":
		l.move(10, visible)
	case "ctrl+u", "pgup":
		l.move(-10, visible)
	default:
		return false
	}
	return true
}

// window returns the [from,to) visible range for the current viewport.
//
// The renderer is the authority on geometry: move()/key() advance l.top against
// update()'s estimate of the visible rows, but the page only knows its REAL row
// budget here (layout inner height minus that page's fixed chrome). When the two
// disagree — update() routinely sees a larger height than view()'s window — the
// cursor can sit below l.top+visible. We re-anchor on the cursor so the selected
// row is always inside the rendered window instead of scrolling off the bottom.
func (l *list) window(visible int) (int, int) {
	if visible <= 0 || l.count <= visible {
		return 0, l.count
	}
	from := l.top
	// Keep the cursor on-screen even if l.top was advanced with a different
	// (larger) visible estimate in update().
	if l.cursor < from {
		from = l.cursor
	}
	if l.cursor >= from+visible {
		from = l.cursor - visible + 1
	}
	to := from + visible
	if to > l.count {
		to = l.count
		from = to - visible
	}
	if from < 0 {
		from = 0
	}
	return from, to
}

// clickMap records, during a page's view() render, which content-local line
// each selectable item occupies — so a click maps to an item robustly across
// sectioned/variable-height layouts (home's feed+recent, expansion, wrapping)
// instead of fragile analytic row math. It mirrors the chat TUI's blockStart
// approach: the renderer is the authority on geometry.
type clickMap struct {
	line2idx map[int]int // content-local line → item index
}

func (c *clickMap) reset() { c.line2idx = map[int]int{} }

// mark records that the item with index idx renders at content-local line.
func (c *clickMap) mark(line, idx int) {
	if c.line2idx == nil {
		c.line2idx = map[int]int{}
	}
	c.line2idx[line] = idx
}

// at returns the item index at content-local line, or (-1,false).
func (c *clickMap) at(line int) (int, bool) {
	if c.line2idx == nil {
		return -1, false
	}
	idx, ok := c.line2idx[line]
	return idx, ok
}

// lineCount counts the content-local lines an accumulated render string spans
// so far (used while building a view to know where the next row lands).
func lineCount(s string) int { return strings.Count(s, "\n") }

// pageTitle renders a page heading with a subtle full-width underline rule.
func pageTitle(title, sub string, w int) string {
	t := sTitle.Render(title)
	if sub != "" {
		t += "  " + sDim.Render(sub)
	}
	rw := w
	if rw < 1 {
		rw = 1
	}
	t = truncate(t, rw)
	rule := sFaint.Render(strings.Repeat("─", rw))
	return t + "\n" + rule + "\n"
}

// row renders a list row with selection styling.
func row(selected bool, text string) string {
	if selected {
		// One selection treatment (shared with the chat rail): a Focus bar +
		// Focus-tinted text — NOT brand accent (brand rule: blue is brand only).
		return lipgloss.NewStyle().Foreground(cSel).Render("▎") + sRowSel.Render(text)
	}
	return " " + sRowDim.Render(text)
}

// truncate cuts s to display width with an ellipsis, preserving UTF-8 and ANSI
// escape boundaries and never returning a string wider than w cells.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "⋯")
}

// pad right-pads s to width (for column alignment).
func pad(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// countLabel renders "N things" dimly.
func countLabel(n int, noun string) string {
	if n == 1 {
		return sDim.Render(fmt.Sprintf("1 %s", noun))
	}
	return sDim.Render(fmt.Sprintf("%d %ss", n, noun))
}

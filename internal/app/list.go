package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
func (l *list) window(visible int) (int, int) {
	if visible <= 0 || l.count <= visible {
		return 0, l.count
	}
	from := l.top
	to := from + visible
	if to > l.count {
		to = l.count
		from = to - visible
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

// pageTitle renders a page heading with a subtle underline rule.
func pageTitle(title, sub string, w int) string {
	t := sTitle.Render(title)
	if sub != "" {
		t += "  " + sDim.Render(sub)
	}
	rule := sFaint.Render(strings.Repeat("─", min(w, 60)))
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

// truncate cuts s to width with an ellipsis.
func truncate(s string, w int) string {
	if w <= 1 || lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if len(r) <= w-1 {
		return s
	}
	return string(r[:w-1]) + "⋯"
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

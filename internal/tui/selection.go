package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// point is a position in the transcript's rendered content: line is the
// absolute viewport content line (0-based), col is the rune column on that line.
type point struct{ line, col int }

// before reports whether p comes at or before q in reading order.
func (p point) beforeOrEq(q point) bool {
	if p.line != q.line {
		return p.line < q.line
	}
	return p.col <= q.col
}

// screenToContent maps an absolute screen row/col to a content point. The
// transcript viewport starts topHeight() rows down (below the plan panel) and
// scrolls by vp.YOffset, so a screen row maps to YOffset + (y - topHeight()).
// The returned point is clamped to the available content.
func (m *model) screenToContent(x, y int) (point, bool) {
	y -= m.topHeight()
	if y < 0 || y >= m.vp.Height {
		return point{}, false
	}
	x -= m.railWidth() // transcript origin shifts right by the rail column
	if x < 0 {
		return point{}, false
	}
	cl := m.vp.YOffset + y
	if cl < 0 {
		cl = 0
	}
	if cl >= len(m.plainLines) {
		// Past the last line: clamp to end of content.
		if len(m.plainLines) == 0 {
			return point{}, false
		}
		last := len(m.plainLines) - 1
		return point{line: last, col: len([]rune(m.plainLines[last]))}, true
	}
	col := x
	if col < 0 {
		col = 0
	}
	if n := len([]rune(m.plainLines[cl])); col > n {
		col = n
	}
	return point{line: cl, col: col}, true
}

// selectedText returns the plain text covered by the drag selection (anchor to
// cursor, normalized), or "" when there is no real selection. Spanning multiple
// lines joins them with newlines; trailing per-line whitespace is trimmed.
func (m *model) selectedText() string {
	if len(m.plainLines) == 0 {
		return ""
	}
	a, b := m.selAnchor, m.selCursor
	if !a.beforeOrEq(b) {
		a, b = b, a
	}
	if a.line == b.line {
		if a.line < 0 || a.line >= len(m.plainLines) {
			return ""
		}
		runes := []rune(m.plainLines[a.line])
		lo, hi := clamp(a.col, len(runes)), clamp(b.col, len(runes))
		if lo >= hi {
			return ""
		}
		return string(runes[lo:hi])
	}
	var sb strings.Builder
	for ln := a.line; ln <= b.line && ln < len(m.plainLines); ln++ {
		runes := []rune(m.plainLines[ln])
		lo, hi := 0, len(runes)
		if ln == a.line {
			lo = clamp(a.col, len(runes))
		}
		if ln == b.line {
			hi = clamp(b.col, len(runes))
		}
		if lo > hi {
			lo = hi
		}
		seg := strings.TrimRight(string(runes[lo:hi]), " ")
		if ln > a.line {
			sb.WriteByte('\n')
		}
		sb.WriteString(seg)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func clamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

// styleSelect is the highlight applied to the active drag selection.
var styleSelect = lipgloss.NewStyle().Reverse(true)

// showSelection re-renders the viewport from the plain text with the active
// selection highlighted. During a drag this replaces the styled transcript with
// a plain, highlighted view so the user sees exactly what will be copied;
// sync() restores the styled content once the drag ends.
func (m *model) showSelection() {
	if !m.ready || len(m.plainLines) == 0 {
		return
	}
	a, b := m.selAnchor, m.selCursor
	if !a.beforeOrEq(b) {
		a, b = b, a
	}
	var out strings.Builder
	for i, ln := range m.plainLines {
		if i < a.line || i > b.line {
			out.WriteString(ln)
			out.WriteByte('\n')
			continue
		}
		runes := []rune(ln)
		lo, hi := 0, len(runes)
		if i == a.line {
			lo = clamp(a.col, len(runes))
		}
		if i == b.line {
			hi = clamp(b.col, len(runes))
		}
		if lo > hi {
			lo = hi
		}
		out.WriteString(string(runes[:lo]))
		out.WriteString(styleSelect.Render(string(runes[lo:hi])))
		out.WriteString(string(runes[hi:]))
		out.WriteByte('\n')
	}
	m.vp.SetContent(out.String())
}

package tui

// Input-box geometry and interaction: row math for the auto-growing textarea,
// soft-wrap helpers, click-to-position, paste, and viewport relayout.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// inputRows returns how many terminal rows the input box occupies including its
// border (top+bottom): the textarea grows with its wrapped content up to
// inputMaxRows, plus 2 for the rounded border frame.
func (m *model) inputRows() int {
	h := m.ti.Height()
	if h < 1 {
		h = 1
	}
	return h + 2 // + top & bottom border rows
}

// visualInputRows counts the number of *visual* (soft-wrapped) rows the current
// input text occupies, so a single long line that wraps grows the box. It must
// match the textarea's own word-wrap (which can break earlier than a hard
// column split), otherwise the box is sized too short and the textarea scrolls
// its first line out of view. We replicate the bubbles word-wrap row count.
func (m *model) visualInputRows() int {
	w := m.ti.Width()
	if w < 1 {
		return m.ti.LineCount()
	}
	total := 0
	for _, line := range strings.Split(m.ti.Value(), "\n") {
		total += wrappedRowCount(line, w)
	}
	if total < 1 {
		total = 1
	}
	return total
}

// wrappedRowCount returns how many visual rows a single logical line occupies
// when word-wrapped to width w, matching bubbles/textarea's wrap(): words are
// kept whole and moved to the next row when they would overflow; a word longer
// than the line is hard-split. Always at least 1.
func wrappedRowCount(line string, w int) int {
	if w < 1 {
		return 1
	}
	rows := 1
	col := 0
	for _, field := range splitKeepingSpaces(line) {
		fw := ansi.StringWidth(field)
		if strings.TrimSpace(field) == "" {
			// trailing/leading spaces: they extend the current column
			col += fw
			continue
		}
		if col > 0 && col+fw > w {
			rows++
			col = 0
		}
		// A word longer than the whole line hard-wraps across rows.
		for fw > w {
			rows++
			fw -= w
		}
		col += fw
	}
	return rows
}

// splitKeepingSpaces splits s into alternating word / whitespace chunks so the
// wrap estimator can treat spaces like the textarea does.
func splitKeepingSpaces(s string) []string {
	var out []string
	var cur strings.Builder
	inSpace := false
	for i, r := range s {
		sp := r == ' ' || r == '\t'
		if i > 0 && sp != inSpace {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
		inSpace = sp
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// wrapSegments returns the visual rows a single logical line breaks into when
// word-wrapped to width w (mirrors bubbles/textarea wrap closely enough for
// click mapping). Long words are hard-split by runes.
func wrapSegments(line string, w int) []string {
	if w < 1 {
		return []string{line}
	}
	var rows []string
	var cur strings.Builder
	curw := 0
	flush := func() { rows = append(rows, cur.String()); cur.Reset(); curw = 0 }
	for _, field := range splitKeepingSpaces(line) {
		fw := ansi.StringWidth(field)
		if strings.TrimSpace(field) == "" {
			cur.WriteString(field)
			curw += fw
			continue
		}
		if curw > 0 && curw+fw > w {
			flush()
		}
		if fw > w {
			// Hard-split a word longer than the line, rune by rune.
			for _, r := range field {
				rwi := ansi.StringWidth(string(r))
				if curw+rwi > w && curw > 0 {
					flush()
				}
				cur.WriteRune(r)
				curw += rwi
			}
			continue
		}
		cur.WriteString(field)
		curw += fw
	}
	rows = append(rows, cur.String())
	return rows
}

// inputTopRow is the absolute screen row of the input box's top border.
func (m *model) inputTopRow() int {
	r := m.topHeight() + m.vp.Height
	if m.state == stRunning {
		r++ // spinner/status line above the input
	}
	if m.comp.active() {
		r += m.comp.rows()
	}
	return r
}

// inputPromptWidth is the visual width of the prompt caret rendered on each
// text row (e.g. "│ ").
func (m *model) inputPromptWidth() int { return ansi.StringWidth(m.ti.Prompt) }

// clickInInput reports whether an absolute screen (x,y) falls on a text row of
// the input box and, if so, the visual text-row index (0-based, from the top of
// the box) and the rune column within that row.
func (m *model) clickInInput(x, y int) (vrow, col int, ok bool) {
	if m.pending != nil {
		return 0, 0, false
	}
	top := m.inputTopRow()
	// Row 0 of the box is the top border; text rows follow; then bottom border.
	vrow = y - top - 1
	if vrow < 0 || vrow >= m.ti.Height() {
		return 0, 0, false
	}
	// Columns: 1 border + prompt width before the text begins.
	col = x - 1 - m.inputPromptWidth()
	if col < 0 {
		col = 0
	}
	return vrow, col, true
}

// positionCursorAt moves the textarea cursor to the visual row (from the top of
// the visible text) and rune column, mapping back through the word-wrap to a
// logical (line, offset). Best-effort: when the box is scrolled (content taller
// than inputMaxRows) the mapping is approximate.
func (m *model) positionCursorAt(vrow, col int) {
	w := m.ti.Width()
	lines := strings.Split(m.ti.Value(), "\n")
	vr := 0
	for li, line := range lines {
		segs := wrapSegments(line, w)
		if vrow < vr+len(segs) {
			segIdx := vrow - vr
			off := 0
			for k := 0; k < segIdx; k++ {
				off += len([]rune(segs[k]))
			}
			seg := []rune(segs[segIdx])
			cc := col
			if cc > len(seg) {
				cc = len(seg)
			}
			off += cc
			// Move to logical line li, then set the column.
			for m.ti.Line() < li {
				m.ti.CursorDown()
			}
			for m.ti.Line() > li {
				m.ti.CursorUp()
			}
			m.ti.SetCursor(off)
			return
		}
		vr += len(segs)
	}
	m.ti.CursorEnd()
}

// pasteIntoInput inserts the clipboard contents at the input cursor (right-click
// paste). Newlines are kept literal so a multi-line paste fills the box.
func (m *model) pasteIntoInput() {
	if m.clip == nil || !m.clip.CanPaste() {
		m.push(&block{kind: blockNote, isErr: true, body: sb("no paste command found (set EIGEN_CLIPBOARD_PASTE_CMD or install wl-paste/xclip/xsel/pbpaste)")})
		return
	}
	text, err := m.clip.Paste()
	if err != nil || text == "" {
		if err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("paste failed: " + err.Error())})
		}
		return
	}
	m.ti.InsertString(text)
	m.resizeInput()
	m.refreshCompletion()
}

// bottomHeight is the number of terminal rows the bottom UI occupies: the input
// box (1+ rows incl. border), the persistent status bar (1–2 rows), plus a
// status/spinner line while a turn runs, plus the autocomplete menu.
func (m *model) bottomHeight() int {
	if m.pending != nil {
		return 1 + m.statusBarHeight() // approval prompt + status bar
	}
	h := m.inputRows() // input box (grows with content, incl. border)
	h += m.statusBarHeight()
	if m.composerBarVisible() {
		h++ // composer bar (voice controls) under the input
	}
	if m.state == stRunning {
		h++ // status/spinner line above the input
	}
	if m.comp.active() {
		h += m.comp.rows()
	}
	if m.ov.active {
		h++ // overlay confirm/text line above the input
	}
	return h
}

// resizeInput grows/shrinks the input box to fit its content (1..inputMaxRows)
// and relays out when the height changes. It counts soft-wrapped visual rows,
// so a long single line that wraps also grows the box.
func (m *model) resizeInput() {
	want := m.visualInputRows()
	if want < 1 {
		want = 1
	}
	if want > inputMaxRows {
		want = inputMaxRows
	}
	if want != m.ti.Height() {
		m.ti.SetHeight(want)
		m.relayout()
	}
}

// relayout sizes the viewport to leave room for the top plan panel and the
// bottom UI.
func (m *model) relayout() {
	if !m.ready {
		return
	}
	h := m.height - 1 - m.bottomHeight() - m.topHeight()
	if h < 1 {
		h = 1
	}
	m.vp.Width = m.width - m.railWidth() - m.rightPanelWidth()
	if m.vp.Width < 1 {
		m.vp.Width = 1
	}
	m.vp.Height = h
	m.sync()
}

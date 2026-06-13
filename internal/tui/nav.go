package tui

// Transcript navigation: block selection, expand/collapse, input history,
// find, and copy.

import (
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/fuzzy"
)

// collapsibleIdx returns block indices that can be selected/toggled.
func (m *model) collapsibleIdx() []int {
	var idx []int
	for i, b := range m.blocks {
		if b.collapsible() {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *model) moveSel(dir int) {
	idx := m.collapsibleIdx()
	if len(idx) == 0 {
		return
	}
	cur := -1
	for j, i := range idx {
		if i == m.sel {
			cur = j
			break
		}
	}
	switch {
	case cur == -1 && dir < 0:
		m.sel = idx[len(idx)-1] // entering from tail → last
	case cur == -1:
		m.sel = idx[0]
	default:
		n := cur + dir
		if n < 0 {
			n = 0
		}
		if n >= len(idx) {
			m.sel = -1 // past the end → back to following tail
			m.sync()
			return
		}
		m.sel = idx[n]
	}
	m.sync()
}

func (m *model) toggleSel() {
	if m.sel >= 0 && m.sel < len(m.blocks) && m.blocks[m.sel].collapsible() {
		m.blocks[m.sel].collapsed = !m.blocks[m.sel].collapsed
		m.sync()
	}
}

// recordHistory appends a submitted line to the input history and resets the
// browse cursor to the live end.
func (m *model) recordHistory(line string) {
	if line == "" {
		return
	}
	// Avoid consecutive duplicates.
	if n := len(m.history); n == 0 || m.history[n-1] != line {
		m.history = append(m.history, line)
	}
	m.histIdx = len(m.history)
	m.histDraft = ""
}

// historyPrev recalls an older input (↑), saving the live draft first.
func (m *model) historyPrev() {
	if len(m.history) == 0 {
		return
	}
	if m.histIdx >= len(m.history) {
		m.histDraft = m.ti.Value()
		m.histIdx = len(m.history)
	}
	if m.histIdx > 0 {
		m.histIdx--
		m.ti.SetValue(m.history[m.histIdx])
		m.ti.CursorEnd()
		m.resizeInput()
	}
}

// historyNext recalls a newer input (↓), restoring the live draft past the end.
func (m *model) historyNext() {
	if m.histIdx >= len(m.history) {
		return
	}
	m.histIdx++
	if m.histIdx >= len(m.history) {
		m.ti.SetValue(m.histDraft)
	} else {
		m.ti.SetValue(m.history[m.histIdx])
	}
	m.ti.CursorEnd()
	m.resizeInput()
}

// copySelected copies the selected block (or the last answer) to the clipboard.
func (m *model) copySelected() {
	if m.clip == nil || !m.clip.Available() {
		return
	}
	if text := m.copyTarget(); text != "" {
		if err := m.clip.Copy(text); err == nil {
			m.note("copied to clipboard")
		}
	}
}

// findBlocks returns indices of blocks whose text matches q (case-insensitive),
// searching body, tool result, title, and rich header.
func (m *model) findBlocks(q string) []int {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	var out []int
	for i, b := range m.blocks {
		hay := strings.ToLower(b.body + "\n" + b.result + "\n" + b.title + "\n" + b.header())
		if strings.Contains(hay, q) {
			out = append(out, i)
		}
	}
	return out
}

// scrollToSelected scrolls the viewport so the selected block is in view.
func (m *model) scrollToSelected() {
	if m.sel < 0 || m.sel >= len(m.blockStart) {
		return
	}
	m.vp.SetYOffset(m.blockStart[m.sel])
}

// copyTarget is the text /copy puts on the clipboard: the selected block (body +
// tool result) if one is selected, otherwise the latest assistant message.
func (m *model) copyTarget() string {
	if m.sel >= 0 && m.sel < len(m.blocks) {
		b := m.blocks[m.sel]
		text := b.body
		if b.result != "" {
			if text != "" {
				text += "\n"
			}
			text += b.result
		}
		return text
	}
	return m.lastAssistantText()
}

// toggleAtRow maps an absolute screen row (msg.Y) to a transcript block and
// toggles it if collapsible — the click handler for thinking/tool blocks. The
// viewport starts topHeight() rows down (below the plan panel), so the click is
// rebased into viewport space first.
func (m *model) toggleAtRow(y int) {
	y -= m.topHeight() // rebase: rows above the viewport (plan panel) don't count
	if y < 0 || y >= m.vp.Height || len(m.blockStart) < 2 {
		return
	}
	target := m.vp.YOffset + y
	for i := 0; i+1 < len(m.blockStart); i++ {
		if target >= m.blockStart[i] && target < m.blockStart[i+1] {
			if i < len(m.blocks) && m.blocks[i].collapsible() {
				m.sel = i
				m.blocks[i].collapsed = !m.blocks[i].collapsed
				m.sync()
			}
			return
		}
	}
}

// openSwitcher opens the in-window session switcher (alt+s / /sessions):
// every daemon session listed, enter hops this WINDOW there — the session
// being shown keeps running in the daemon. Local (non-daemon) chats have no
// siblings to switch to.
func (m *model) openSwitcher() {
	sl, ok := m.backend.(interface {
		Sessions() []chat.SessionEntry
		SessionID() string
	})
	if !ok {
		m.note("session switching needs a daemon-hosted chat")
		return
	}
	entries := sl.Sessions()
	if len(entries) == 0 {
		m.note("no daemon sessions")
		return
	}
	m.switchEntries = entries
	m.switchQuery = ""
	m.switchIdx = 0
	// Preselect the current session so the list opens oriented.
	for i, e := range entries {
		if e.ID == sl.SessionID() {
			m.switchIdx = i
			break
		}
	}
	m.switching = true
	m.sync()
}

// switchFiltered returns the switcher entries matching switchQuery, fuzzy
// rank-ordered (title + id + dir); the full list when the query is empty.
func (m *model) switchFiltered() []chat.SessionEntry {
	if m.switchQuery == "" {
		return m.switchEntries
	}
	type hit struct {
		e     chat.SessionEntry
		score int
	}
	var hits []hit
	for _, e := range m.switchEntries {
		if s := fuzzy.Score(e.Title+" "+e.ID+" "+e.Dir, m.switchQuery); s >= 0 {
			hits = append(hits, hit{e, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	out := make([]chat.SessionEntry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out
}

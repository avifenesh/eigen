package tui

// Right panel tabs (Tier 11): the existing right changes panel becomes a
// tabbed panel. First tabs are "changes" (existing content) and "git" (cheap
// read-only status, next slice). The tab skeleton is keyboard/click parity for
// later git/terminal work.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type rightPanelTab int

const (
	rightTabChanges rightPanelTab = iota
	rightTabGit
	rightTabTerminal
)

func (t rightPanelTab) label() string {
	switch t {
	case rightTabGit:
		return "git"
	case rightTabTerminal:
		return "term"
	default:
		return "changes"
	}
}

func (m *model) rightTabs() []rightPanelTab {
	return []rightPanelTab{rightTabChanges, rightTabGit, rightTabTerminal}
}

func (m *model) nextRightTab() {
	tabs := m.rightTabs()
	for i, t := range tabs {
		if t == m.rightTab {
			m.rightTab = tabs[(i+1)%len(tabs)]
			m.changesOn = true
			m.relayout()
			m.note("right panel → " + m.rightTab.label())
			return
		}
	}
	m.rightTab = rightTabChanges
	m.changesOn = true
	m.relayout()
}

func (m *model) setRightTab(t rightPanelTab) {
	m.rightTab = t
	m.changesOn = true
	m.relayout()
}

// rightPanelTitleLine renders the tab bar + close control inside the right
// panel header. The active tab is accent/bold; inactive tabs are dim.
func (m *model) rightPanelTitleLine(width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	for i, t := range m.rightTabs() {
		if i > 0 {
			b.WriteString(" ")
		}
		label := "[" + t.label() + "]"
		if t == m.rightTab {
			b.WriteString(styleAccent.Bold(true).Render(label))
		} else {
			b.WriteString(dim(label))
		}
	}
	left := b.String()
	right := dim(panelCloseGlyph)
	lw := ansi.StringWidth(ansi.Strip(left))
	rw := ansi.StringWidth(panelCloseGlyph)
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	if ansi.StringWidth(ansi.Strip(line)) > width {
		return panelTitleLine(m.rightTab.label(), width, true)
	}
	return line
}

// rightPanelTabAt maps a content-local click on the panel header (after the
// leading "│ " gutter) to a tab action, or actNone. The close [x] is handled by
// panelCloseAt in layout.hitTest before this runs.
func (m *model) rightPanelTabAt(localX, localY, width int) actionID {
	if localY != 0 || width <= 0 {
		return actNone
	}
	col := 0
	for _, t := range m.rightTabs() {
		label := "[" + t.label() + "]"
		lw := ansi.StringWidth(label)
		if localX >= col && localX < col+lw {
			m.setRightTab(t)
			return actNone // state already updated; no dispatch needed
		}
		col += lw + 1
	}
	return actNone
}

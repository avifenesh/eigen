package tui

// Right panel tabs (Tier 11): the existing right changes panel becomes a
// tabbed panel. First tabs are "changes" (existing content) and "git" (cheap
// read-only status, next slice). The tab skeleton is keyboard/click parity for
// later git/terminal work.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// nextRightTab cycles the right panel tab and returns any command needed to
// activate the new tab (e.g. starting the embedded terminal's PTY reader).
func (m *model) nextRightTab() tea.Cmd {
	tabs := m.rightTabs()
	for i, t := range tabs {
		if t == m.rightTab {
			next := tabs[(i+1)%len(tabs)]
			return m.setRightTab(next)
		}
	}
	return m.setRightTab(rightTabChanges)
}

// setRightTab selects a tab. Switching to the terminal tab lazily starts the
// shell (and returns the reader goroutine command); switching away unfocuses
// the terminal so the TUI gets its keys back, but leaves the shell running.
func (m *model) setRightTab(t rightPanelTab) tea.Cmd {
	prev := m.rightTab
	m.rightTab = t
	m.changesOn = true
	if prev == rightTabTerminal && t != rightTabTerminal {
		m.term.focused = false
	}
	m.relayout()
	if t == rightTabTerminal {
		m.term.focused = true
		return m.startTerm(m.termRows())
	}
	return nil
}

// termRows is the emulator's row count given the current transcript height
// (panel header takes one row).
func (m *model) termRows() int {
	r := m.vp.Height - 1
	if r < 1 {
		r = 1
	}
	return r
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
// leading "│ " gutter) to a tab switch, returning any activation command (e.g.
// starting the terminal). The close [x] is handled by panelCloseAt in
// layout.hitTest before this runs. The bool reports whether a tab was hit.
func (m *model) rightPanelTabAt(localX, localY, width int) (tea.Cmd, bool) {
	if localY != 0 || width <= 0 {
		return nil, false
	}
	col := 0
	for _, t := range m.rightTabs() {
		label := "[" + t.label() + "]"
		lw := ansi.StringWidth(label)
		if localX >= col && localX < col+lw {
			return m.setRightTab(t), true
		}
		col += lw + 1
	}
	return nil, false
}

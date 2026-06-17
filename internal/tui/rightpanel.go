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
	rightTabTasks
	rightTabObserve
	rightTabGoal
	rightTabShells
	rightTabNotepad
)

func (t rightPanelTab) label() string {
	switch t {
	case rightTabGit:
		return "git"
	case rightTabTerminal:
		return "term"
	case rightTabTasks:
		return "tasks"
	case rightTabShells:
		return "shells"
	case rightTabObserve:
		return "observe"
	case rightTabGoal:
		return "goal"
	case rightTabNotepad:
		return "notes"
	default:
		return "changes"
	}
}

// shortLabel is the compressed tab label used when the full set no longer
// fits the panel header (4 tabs × default width).
func (t rightPanelTab) shortLabel() string {
	switch t {
	case rightTabChanges:
		return "chg"
	case rightTabTerminal:
		return "trm"
	case rightTabTasks:
		return "tsk"
	case rightTabShells:
		return "sh"
	case rightTabObserve:
		return "obs"
	case rightTabGoal:
		return "go"
	case rightTabNotepad:
		return "nt"
	default:
		return t.label()
	}
}

func (m *model) rightTabs() []rightPanelTab {
	tabs := []rightPanelTab{rightTabChanges, rightTabGit, rightTabTerminal, rightTabTasks}
	if m.rightTab == rightTabObserve {
		tabs = append(tabs, rightTabObserve)
	}
	if m.goalActive() != "" {
		tabs = append(tabs, rightTabGoal)
	}
	// The shells tab appears only when the backend can host background shells
	// AND there are shells to show (keeps the header lean otherwise).
	if len(m.backendShells()) > 0 {
		tabs = append(tabs, rightTabShells)
	}
	tabs = append(tabs, rightTabNotepad)
	return tabs
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
	if t == rightTabTasks {
		m.refreshTasks()
		m.tasks.gen++ // retire any previous tick chain; start a fresh one
		return m.tasksTick()
	}
	if t == rightTabShells {
		m.shells.gen++ // fresh poll tick chain
		return m.shellsTick()
	}
	if t == rightTabNotepad {
		m.loadNotepad() // pull the saved notes for this session (once)
		m.notepad.focused = false
		return m.notepadAutosaveTick()
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

// tabsFit reports whether the tab bar fits width with the given label mode
// (renderer and hit-test share this decision so click math can't drift).
func (m *model) tabsFit(width int, short bool) bool {
	w := 0
	for i, t := range m.rightTabs() {
		if i > 0 {
			w++
		}
		w += ansi.StringWidth("[" + m.tabLabel(t, short) + "]")
	}
	return width-w-ansi.StringWidth(panelCloseGlyph) >= 1
}

// rightPanelTitleLine renders the tab bar + close control inside the right
// panel header. The active tab is accent/bold; inactive tabs are dim. When the
// full labels overflow the width, compressed labels are tried before falling
// back to a single-title header.
func (m *model) rightPanelTitleLine(width int) string {
	if width <= 0 {
		return ""
	}
	for _, short := range []bool{false, true} {
		if !m.tabsFit(width, short) {
			continue // doesn't fit — try compressed labels, then the fallback
		}
		var b strings.Builder
		for i, t := range m.rightTabs() {
			if i > 0 {
				b.WriteString(" ")
			}
			label := "[" + m.tabLabel(t, short) + "]"
			if t == m.rightTab {
				b.WriteString(styleSel.Bold(true).Render(label)) // selected tab — non-brand
			} else {
				b.WriteString(dim(label))
			}
		}
		left := b.String()
		right := dim(panelCloseGlyph)
		gap := width - ansi.StringWidth(ansi.Strip(left)) - ansi.StringWidth(panelCloseGlyph)
		return left + strings.Repeat(" ", gap) + right
	}
	return panelTitleLine(m.rightTab.label(), width, true)
}

// tabLabel picks the full or compressed label for one tab.
func (m *model) tabLabel(t rightPanelTab, short bool) string {
	if short {
		return t.shortLabel()
	}
	return t.label()
}

// rightPanelTabAt maps a content-local click on the panel header (after the
// leading "│ " gutter) to a tab switch, returning any activation command (e.g.
// starting the terminal). The close [x] is handled by panelCloseAt in
// layout.hitTest before this runs. The bool reports whether a tab was hit.
func (m *model) rightPanelTabAt(localX, localY, width int) (tea.Cmd, bool) {
	if localY != 0 || width <= 0 {
		return nil, false
	}
	// Mirror the renderer's label choice exactly (full → short → none).
	short := false
	if !m.tabsFit(width, false) {
		if !m.tabsFit(width, true) {
			return nil, false // single-title fallback: no tab targets
		}
		short = true
	}
	col := 0
	for _, t := range m.rightTabs() {
		label := "[" + m.tabLabel(t, short) + "]"
		lw := ansi.StringWidth(label)
		if localX >= col && localX < col+lw {
			return m.setRightTab(t), true
		}
		col += lw + 1
	}
	return nil, false
}

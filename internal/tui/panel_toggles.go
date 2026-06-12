package tui

// Toggle helpers for Tier 11 side panels. Slash commands, palette entries,
// keyboard shortcuts, and clickable [x]/[◧]/[◨] affordances all route through
// these so close/reopen behavior stays identical. When a panel is toggled ON
// but the terminal is too narrow, the toggle asks the surrounding multiplexer
// (zellij/tmux) to stretch the pane instead of silently doing nothing.

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) toggleRail() tea.Cmd {
	if m.railLister() == nil {
		m.note("the session rail needs a daemon-hosted chat (no siblings in a local chat)")
		return nil
	}
	m.railOn = !m.railOn
	m.relayout()
	switch {
	case !m.railOn:
		m.note("session rail hidden  (header [◧], /rail, or ctrl+b to show)")
	case m.railVisible():
		m.note("session rail shown  ([x], [◧], or /rail to hide)")
	default:
		// On, but it doesn't fit: try to stretch the pane.
		need := m.railNeededWidth()
		m.note(fmt.Sprintf("session rail needs ≥%d cols (terminal is %d) — trying to stretch the pane…", need, m.width))
		return growToWidth(need)
	}
	return nil
}

func (m *model) toggleChanges() tea.Cmd {
	m.changesOn = !m.changesOn
	m.relayout()
	switch {
	case !m.changesOn:
		m.note("right panel hidden  (header [◨], /changes, or ctrl+g to show)")
	case m.rightTab == rightTabChanges && len(m.lastRunChanges()) == 0:
		m.note("right panel on — changes tab shows files edited in the last turn (none yet)")
	case m.changesVisible():
		m.note("right panel shown  ([x], [◨], or /changes to hide)")
	default:
		// On, but it doesn't fit: try to stretch the pane.
		need := m.rightNeededWidth()
		m.note(fmt.Sprintf("right panel needs ≥%d cols (terminal is %d) — trying to stretch the pane…", need, m.width))
		return growToWidth(need)
	}
	return nil
}

// toggleSidebar flips the headerless command-sidebar chrome (Tier 11.5,
// /chrome). Too-narrow terminals keep the classic header; like the panels,
// the toggle is honest and asks the multiplexer for room when it can't fit.
func (m *model) toggleSidebar() tea.Cmd {
	m.sidebarOn = !m.sidebarOn
	m.relayout()
	switch {
	case !m.sidebarOn:
		m.note("sidebar chrome off — classic header restored  (/chrome to switch back)")
	case m.sidebarVisible():
		m.note("sidebar chrome on — header folded into the left column  (/chrome to revert)")
	default:
		need := railMinTerminalWidth
		m.note(fmt.Sprintf("sidebar chrome needs ≥%d cols (terminal is %d) — trying to stretch the pane…", need, m.width))
		return growToWidth(need)
	}
	return nil
}

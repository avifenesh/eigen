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
	case m.changesVisible():
		if m.rightTab == rightTabChanges && len(m.lastRunChanges()) == 0 {
			m.note("right panel shown — [changes][git][term][tasks]; changes fills after an edit ([x]/[◨] to hide)")
		} else {
			m.note("right panel shown  ([x], [◨], or /changes to hide)")
		}
	default:
		// On, but it doesn't fit: try to stretch the pane.
		need := m.rightNeededWidth()
		m.note(fmt.Sprintf("right panel needs ≥%d cols (terminal is %d) — trying to stretch the pane…", need, m.width))
		return growToWidth(need)
	}
	return nil
}

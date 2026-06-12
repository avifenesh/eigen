package tui

// Toggle helpers for Tier 11 side panels. Slash commands, palette entries,
// keyboard shortcuts, and clickable [x] affordances all route through these so
// close/reopen behavior stays identical.

func (m *model) toggleRail() {
	if m.railLister() == nil {
		m.note("the session rail needs a daemon-hosted chat (no siblings in a local chat)")
		return
	}
	m.railOn = !m.railOn
	m.relayout()
	switch {
	case !m.railOn:
		m.note("session rail hidden  (header [◧], /rail, or ctrl+b to show)")
	case m.width < railMinTerminalWidth:
		m.note("session rail on — but hidden on this narrow terminal (needs ≥80 cols)")
	default:
		m.note("session rail shown  ([x], [◧], or /rail to hide)")
	}
}

func (m *model) toggleChanges() {
	m.changesOn = !m.changesOn
	m.relayout()
	switch {
	case !m.changesOn:
		m.note("right panel hidden  (header [◨], /changes, or ctrl+g to show)")
	case m.rightTab == rightTabChanges && len(m.lastRunChanges()) == 0:
		m.note("right panel on — changes tab shows files edited in the last turn (none yet)")
	default:
		m.note("right panel shown  ([x], [◨], or /changes to hide)")
	}
}

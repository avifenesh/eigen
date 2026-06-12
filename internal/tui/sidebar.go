package tui

// Tier 11.5 (Waves 1+2): the headerless left command sidebar, behind /chrome.
// In sidebar mode the top header disappears (the transcript starts at the top)
// and a left column owns the chrome: session title, project breadcrumb, the
// nav actions that lived in the header (home/sessions/+new/config), the right
// panel toggle, and the session rail folded in below. The renderer and the
// click hit-test share ONE row model (sidebarRows) so geometry cannot drift —
// the same convention as the rail's railRows. Below the rail width threshold
// the classic header returns, so chrome stays reachable on narrow terminals.

type sidebarRowKind int

const (
	sbTitle sidebarRowKind = iota
	sbCwd
	sbBlank
	sbNav            // clickable action row
	sbSessionsHeader // "sessions" mini-header above the embedded rail rows
	sbRail           // embedded rail row (project header or session)
)

// sidebarRow is one rendered sidebar line. Nav rows carry the action they
// dispatch; rail rows embed the rail's own row model.
type sidebarRow struct {
	kind   sidebarRowKind
	label  string
	action actionID
	rail   railRow
}

// sidebarVisible reports whether the headerless command sidebar is active:
// toggled on AND the terminal is wide enough. Below the threshold the classic
// header renders instead (graceful narrow degradation).
func (m *model) sidebarVisible() bool {
	return m.sidebarOn && m.width >= railMinTerminalWidth
}

// sidebarRows builds the row model. The renderer (sidebarLines) and the click
// hit-test (sidebarRowAt) both walk exactly this.
func (m *model) sidebarRows() []sidebarRow {
	rows := []sidebarRow{
		{kind: sbTitle, action: actRename},
		{kind: sbCwd},
		{kind: sbBlank},
		{kind: sbNav, label: "⌂ home", action: actHome},
		{kind: sbNav, label: "⇆ sessions", action: actSwitcher},
		{kind: sbNav, label: "+ new", action: actNewSession},
		{kind: sbNav, label: "⚙ config", action: actConfigPanel},
		{kind: sbNav, label: "◨ right panel", action: actChangesToggle},
	}
	if m.railLister() != nil && m.railOn {
		rows = append(rows, sidebarRow{kind: sbBlank}, sidebarRow{kind: sbSessionsHeader})
		for _, r := range m.railRows() {
			rows = append(rows, sidebarRow{kind: sbRail, rail: r})
		}
	}
	return rows
}

// sidebarRowAt maps a sidebar-local y to its row (no header offset — row 0 is
// the title row).
func (m *model) sidebarRowAt(localY int) (sidebarRow, bool) {
	rows := m.sidebarRows()
	if localY < 0 || localY >= len(rows) {
		return sidebarRow{}, false
	}
	return rows[localY], true
}

// sidebarLines renders the sidebar as exactly h lines padded to the rail
// width, mirroring railLines' padding/gutter conventions.
func (m *model) sidebarLines(h int) []string {
	rw := m.railCols()
	contentW := rw - 2 // " │" gutter
	cur := ""
	if sl := m.railLister(); sl != nil {
		cur = sl.SessionID()
	}
	grouped := m.railGrouped()
	lines := make([]string, 0, h)
	for _, r := range m.sidebarRows() {
		if len(lines) >= h {
			break
		}
		switch r.kind {
		case sbTitle:
			lines = append(lines, railPad(styleUser.Render(ansiTrunc(m.headerTitle(), contentW)), rw))
		case sbCwd:
			lines = append(lines, railPad(dim(ansiTrunc(m.headerBreadcrumb(), contentW)), rw))
		case sbBlank:
			lines = append(lines, railPad("", rw))
		case sbNav:
			label := r.label
			// The right-panel toggle reflects its open/closed state, the same
			// lit/dim language as the header's ◨ button.
			if r.action == actChangesToggle && m.changesOn {
				label = styleAccent.Render(ansiTrunc(label, contentW))
			} else {
				label = dim(ansiTrunc(label, contentW))
			}
			lines = append(lines, railPad(label, rw))
		case sbSessionsHeader:
			lines = append(lines, railPad(panelTitleLine("sessions", rw-1, false), rw))
		case sbRail:
			if r.rail.header {
				lines = append(lines, railPad(m.railHeaderLabel(r.rail.dir, contentW), rw))
				continue
			}
			e := m.railEntries[r.rail.entry]
			title := e.Title
			if title == "" {
				title = "(untitled)"
			}
			mark := " "
			if e.ID == cur {
				mark = styleAccent.Render("·")
			}
			indent := ""
			if grouped {
				indent = " "
			}
			label := indent + m.railGlyph(e.Status) + mark + ansiTrunc(title, contentW-3-len(indent))
			lines = append(lines, railPad(label, rw))
		}
	}
	for len(lines) < h {
		lines = append(lines, railPad("", rw))
	}
	return lines
}

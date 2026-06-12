package tui

// Tier 11.5: the headerless left command sidebar — THE design (user-approved).
// The top header is gone on wide terminals; a left column owns the chrome:
// session title, project breadcrumb, the nav actions (home/sessions/+new/
// config), status setters (model/perm/effort/route/context), the right-panel
// toggle, the todo plan, and the session rail folded in below. The renderer
// and the click hit-test share ONE row model (sidebarRows) so geometry cannot
// drift — the same convention as the rail's railRows. Below the rail width
// threshold the classic header returns, so chrome stays reachable on narrow
// terminals.

import (
	"fmt"
	"strings"
)

type sidebarRowKind int

const (
	sbTitle sidebarRowKind = iota
	sbCwd
	sbBlank
	sbNav            // clickable action row
	sbStatus         // clickable status setter row (model/perm/effort/…)
	sbTodoHeader     // "plan (n/m)" header above the todo rows
	sbTodo           // one plan task row
	sbSessionsHeader // "sessions" mini-header above the embedded rail rows
	sbRail           // embedded rail row (project header or session)
)

// sidebarRow is one rendered sidebar line. Nav/status rows carry the action
// they dispatch; rail rows embed the rail's own row model; todo rows carry
// their task index.
type sidebarRow struct {
	kind   sidebarRowKind
	label  string
	action actionID
	rail   railRow
	todo   int
}

// sidebarVisible reports whether the headerless command sidebar is active —
// always, on a wide-enough terminal (this IS the design). Below the threshold
// the classic header renders instead (graceful narrow degradation).
func (m *model) sidebarVisible() bool {
	return m.width >= railMinTerminalWidth
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
	// Background-tasks badge (Tier 12): only when tasks exist — running count
	// keeps delegated work visible without opening the tab; click opens it.
	if lbl := m.tasksBadge(); lbl != "" {
		rows = append(rows, sidebarRow{kind: sbNav, label: lbl, action: actTasksTab})
	}
	// Voice buttons (Tier 15): three features, three buttons — dictate one
	// message (answer stays text), read the last answer aloud, and full
	// conversation mode. Buttons are primary (ctrl+t is dead under zellij).
	rows = append(rows,
		sidebarRow{kind: sbNav, label: "⏺ speak", action: actDictate},
		sidebarRow{kind: sbNav, label: "▶ read answer", action: actSpeakAnswer},
		sidebarRow{kind: sbNav, label: m.micGlyph(), action: actVoiceToggle},
	)
	rows = append(rows, sidebarRow{kind: sbBlank})
	// Status setters (Wave 3): the bottom status bar's segments as rows —
	// click = the same actions; everything stays keyboard-reachable too.
	rows = append(rows, m.sidebarStatusRows()...)
	// Todo plan (Wave 4): folded in as a section instead of a top panel.
	if len(m.todos) > 0 {
		rows = append(rows, sidebarRow{kind: sbBlank}, sidebarRow{kind: sbTodoHeader})
		n := len(m.todos)
		if n > maxTodoRows {
			n = maxTodoRows
		}
		for i := 0; i < n; i++ {
			rows = append(rows, sidebarRow{kind: sbTodo, todo: i})
		}
	}
	if m.railLister() != nil && m.railOn {
		rows = append(rows, sidebarRow{kind: sbBlank}, sidebarRow{kind: sbSessionsHeader})
		for _, r := range m.railRows() {
			rows = append(rows, sidebarRow{kind: sbRail, rail: r})
		}
	}
	return rows
}

// tasksBadge is the sidebar's background-tasks row label: "" when no tasks
// exist (no noise), a running count while work is in flight, or the latest
// terminal state so a finish stays noticeable until viewed.
func (m *model) tasksBadge() string {
	if !m.tasks.loaded {
		m.refreshTasks()
	}
	running, done, failed := 0, 0, 0
	for _, t := range m.tasks.tasks {
		switch t.Status {
		case "running":
			running++
		case "done":
			done++
		case "error", "lost":
			failed++
		}
	}
	switch {
	case running > 0:
		return fmt.Sprintf("⚒ tasks %d●", running)
	case failed > 0:
		return fmt.Sprintf("⚒ tasks %d✗", failed)
	case done > 0:
		return fmt.Sprintf("⚒ tasks %d✓", done)
	}
	return ""
}

// sidebarStatusRows converts the status-bar segments into sidebar rows. The
// brand segment is dropped (the sidebar IS eigen); the rest keep their click
// actions (model picker, perm, effort, search, route, compact).
func (m *model) sidebarStatusRows() []sidebarRow {
	var rows []sidebarRow
	for _, s := range m.statusBarParts() {
		if s.text == "eigen" {
			continue
		}
		rows = append(rows, sidebarRow{kind: sbStatus, label: s.text, action: s.action})
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
			// lit/dim language as the header's ◨ button. The tasks badge is
			// lit while delegated work is running.
			switch {
			case r.action == actChangesToggle && m.changesOn:
				label = styleAccent.Render(ansiTrunc(label, contentW))
			case r.action == actTasksTab && strings.Contains(label, "●"):
				label = styleAccent.Render(ansiTrunc(label, contentW))
			case r.action == actVoiceToggle && m.voiceOn:
				// Conversation mode lit while on; listening pulses via the
				// label text (● listening) from micGlyph.
				label = styleAccent.Render(ansiTrunc(label, contentW))
			default:
				label = dim(ansiTrunc(label, contentW))
			}
			lines = append(lines, railPad(label, rw))
		case sbStatus:
			// Status setter rows keep their status-bar colors (perm amber
			// when auto, ctx by fullness, …) via the original segment style.
			lines = append(lines, railPad(m.sidebarStatusLabel(r.label, contentW), rw))
		case sbTodoHeader:
			done := 0
			for _, t := range m.todos {
				if t.Status == "completed" {
					done++
				}
			}
			hdr := fmt.Sprintf("plan (%d/%d)", done, len(m.todos))
			lines = append(lines, railPad(panelTitleLine(hdr, rw-1, false), rw))
		case sbTodo:
			if r.todo >= 0 && r.todo < len(m.todos) {
				t := m.todos[r.todo]
				content := t.Content
				if t.Status == "completed" {
					content = ""
				}
				label := todoGlyphStyled(t.Status) + " " + ansiTrunc(t.Content, contentW-2)
				if content == "" {
					label = todoGlyphStyled(t.Status) + " " + dim(ansiTrunc(t.Content, contentW-2))
				}
				lines = append(lines, railPad(label, rw))
			}
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

// sidebarStatusLabel renders a status row with the same style its status-bar
// segment used (matched by text — statusBarParts is the single source).
func (m *model) sidebarStatusLabel(text string, w int) string {
	for _, s := range m.statusBarParts() {
		if s.text == text {
			return s.style.Render(ansiTrunc(text, w))
		}
	}
	return dim(ansiTrunc(text, w))
}

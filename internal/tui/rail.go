package tui

// The left session rail (Tier 9 Wave 3): a persistent narrow column down the
// left of the TRANSCRIPT band listing the daemon's sibling sessions with live
// status glyphs, so the other running agents are always in view and one click
// hops the window there. It reuses the EXACT switcher hop path (Detach never
// interrupts a running daemon turn) — there is no second switching code path.
//
// The rail spans only the transcript rows (not the header/input/status), so
// the input-cursor mapping is untouched; only the transcript origin shifts
// right by the rail width, which screenToContent rebases.

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/chat"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// railWidthCols is the rail's default total width (content + a one-column
// gutter). The user can resize it by dragging the separator edge; the live
// width lives in model.railW (0 = this default).
const railWidthCols = 22

// railMinW / railMaxW clamp user resizing to keep the rail usable.
const (
	railMinW = 14
	railMaxW = 44
)

// railMinTerminalWidth is the narrowest terminal that still shows the rail —
// below this the transcript needs the whole width, so the rail hides (it stays
// reachable via alt+s / the [sessions] header button).
const railMinTerminalWidth = 80

// railPollEvery is how often the rail refreshes the session list + statuses.
const railPollEvery = 1200 * time.Millisecond

// railSpinEvery is the faster poll cadence used while any sibling session is
// working, so the rail's activity spinner visibly moves (the list op is a
// cheap local-socket roundtrip).
const railSpinEvery = 300 * time.Millisecond

// railSpinnerFrames animates working sessions in the rail — visibly alive,
// distinct from the static idle ○.
var railSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// railSessionLister is the backend capability the rail needs (daemon-hosted
// chats). Local chats have no siblings, so the rail stays hidden for them.
type railSessionLister interface {
	Sessions() []chat.SessionEntry
	SessionID() string
}

// railLister returns the backend as a session lister, or nil for local chats.
func (m *model) railLister() railSessionLister {
	if sl, ok := m.backend.(railSessionLister); ok {
		return sl
	}
	return nil
}

// railVisible reports whether the rail should render: enabled, a daemon-hosted
// backend with siblings, and a terminal wide enough.
func (m *model) railVisible() bool {
	if !m.railOn || m.width < railMinTerminalWidth {
		return false
	}
	return m.railLister() != nil
}

// railCols is the rail's effective column width: the user-set width (or the
// default), clamped to its bounds and to never starve the transcript.
func (m *model) railCols() int {
	w := m.railW
	if w == 0 {
		w = railWidthCols
	}
	if w < railMinW {
		w = railMinW
	}
	if w > railMaxW {
		w = railMaxW
	}
	if max := m.width - minTranscriptCols; w > max {
		w = max
	}
	return w
}

// railWidth is the rail's column width (0 when hidden).
func (m *model) railWidth() int {
	// Sidebar mode reuses the rail column for the command sidebar (which
	// embeds the rail rows below the nav) — same width, even for local chats
	// (no sibling sessions, still title/nav).
	if m.sidebarVisible() {
		return m.railCols()
	}
	if !m.railVisible() {
		return 0
	}
	return m.railCols()
}

// setRailW applies a user resize (drag or palette action): clamp, store, and
// reflow the transcript around the new width.
func (m *model) setRailW(w int) {
	if w < railMinW {
		w = railMinW
	}
	if w > railMaxW {
		w = railMaxW
	}
	if w == m.railCols() {
		m.railW = w
		return
	}
	m.railW = w
	m.relayout()
}

// railTickMsg drives the periodic rail refresh.
type railTickMsg struct{}

// railTick schedules the next rail refresh (nil when the rail is hidden, so it
// costs nothing for local chats). The cadence speeds up while any sibling
// session is working so the activity spinner animates.
func (m *model) railTick() tea.Cmd {
	if m.railLister() == nil {
		return nil
	}
	every := railPollEvery
	for _, e := range m.railEntries {
		if e.Status == "working" {
			every = railSpinEvery
			break
		}
	}
	return tea.Tick(every, func(time.Time) tea.Msg { return railTickMsg{} })
}

// refreshRail re-reads the session list from the backend (cheap, in-process for
// the daemon client's cached snapshot) and advances the activity spinner.
func (m *model) refreshRail() {
	if sl := m.railLister(); sl != nil {
		m.railEntries = sl.Sessions()
		m.railSpin++
	}
	// Piggyback the background-task badge refresh: the rail poll is the one
	// steady heartbeat the chat has, and the store read is a cheap dir scan.
	// Throttled to the tasks cadence (the rail spins at 300ms while a sibling
	// works — no point rescanning disk that fast).
	if time.Since(m.tasks.refreshed) >= tasksRefresh {
		m.refreshTasks()
	}
}

// railRow is one rendered rail line below the "sessions" panel header: either
// a collapsible project header or a session entry. The renderer and the click
// hit-test share this row model, so geometry can't drift between them.
type railRow struct {
	header bool
	dir    string // project dir (header rows)
	entry  int    // index into railEntries for session rows; -1 for headers
}

// railGrouped reports whether the rail groups sessions under project headers:
// only when the sessions span more than one project dir (a single project
// would make the header pure noise).
func (m *model) railGrouped() bool {
	first := ""
	for i, e := range m.railEntries {
		if i == 0 {
			first = e.Dir
		} else if e.Dir != first {
			return true
		}
	}
	return false
}

// railRows builds the row model: project headers (in first-appearance order,
// newest session first) with their sessions beneath, honoring collapsed state.
func (m *model) railRows() []railRow {
	if !m.railGrouped() {
		rows := make([]railRow, 0, len(m.railEntries))
		for i := range m.railEntries {
			rows = append(rows, railRow{entry: i, dir: m.railEntries[i].Dir})
		}
		return rows
	}
	var order []string
	byDir := map[string][]int{}
	for i, e := range m.railEntries {
		if _, ok := byDir[e.Dir]; !ok {
			order = append(order, e.Dir)
		}
		byDir[e.Dir] = append(byDir[e.Dir], i)
	}
	var rows []railRow
	for _, d := range order {
		rows = append(rows, railRow{header: true, dir: d, entry: -1})
		if m.railCollapsed[d] {
			continue
		}
		for _, i := range byDir[d] {
			rows = append(rows, railRow{entry: i, dir: d})
		}
	}
	return rows
}

// railGlyph maps a session status to its rail glyph; working sessions animate
// (frame advanced by the rail tick) so liveness is visible at a glance.
func (m *model) railGlyph(status string) string {
	if status == "working" {
		return styleAccent.Render(railSpinnerFrames[m.railSpin%len(railSpinnerFrames)])
	}
	return statusGlyph(status)
}

// railProjectOpen reports whether any session of the project has a window
// attached right now (Views > 0) — "open somewhere" highlights the header.
func (m *model) railProjectOpen(dir string) bool {
	for _, e := range m.railEntries {
		if e.Dir == dir && e.Views > 0 {
			return true
		}
	}
	return false
}

// railHeaderLabel renders a project header row: collapse arrow + project name
// (accented when the project is open in some window), plus a session count and
// the most-urgent status glyph when collapsed (so hidden activity still shows).
func (m *model) railHeaderLabel(dir string, w int) string {
	name := filepath.Base(dir)
	if name == "" || name == "." || name == "/" {
		name = dir
	}
	collapsed := m.railCollapsed[dir]
	arrow := "▾"
	suffix := ""
	glyph := ""
	if collapsed {
		arrow = "▸"
		n, worst := 0, ""
		for _, e := range m.railEntries {
			if e.Dir != dir {
				continue
			}
			n++
			if statusRank(e.Status) > statusRank(worst) {
				worst = e.Status
			}
		}
		suffix = fmt.Sprintf(" (%d)", n)
		glyph = " " + m.railGlyph(worst)
	}
	// Truncate the name so arrow+name+suffix+glyph fit the content width.
	nameW := w - 2 - len(suffix)
	if glyph != "" {
		nameW -= 2
	}
	if nameW < 1 {
		nameW = 1
	}
	styledName := ansiTrunc(name, nameW)
	if m.railProjectOpen(dir) {
		styledName = styleAccent.Render(styledName)
	} else {
		styledName = dim(styledName)
	}
	return dim(arrow) + " " + styledName + dim(suffix) + glyph
}

// statusRank orders session statuses by urgency for collapsed-header rollups.
func statusRank(s string) int {
	switch s {
	case "error":
		return 4
	case "approval":
		return 3
	case "working":
		return 2
	case "idle":
		return 1
	}
	return 0
}

// railLines renders the rail as exactly h lines, each padded to the full rail
// width (content + gutter). Sessions group under collapsible project headers
// (when they span projects); the current session is marked; status glyphs use
// the shared ●○◆✗ language with an animated spinner for working.
func (m *model) railLines(h int) []string {
	cur := ""
	if sl := m.railLister(); sl != nil {
		cur = sl.SessionID()
	}
	rw := m.railCols()
	contentW := rw - 3 // leave separator + gutter space + margin
	grouped := m.railGrouped()
	lines := make([]string, 0, h)
	// Header row for the rail, with a visible close affordance.
	lines = append(lines, railPad(panelTitleLine("sessions", contentW, true), rw))
	for _, r := range m.railRows() {
		if len(lines) >= h {
			break
		}
		if r.header {
			lines = append(lines, railPad(m.railHeaderLabel(r.dir, contentW), rw))
			continue
		}
		e := m.railEntries[r.entry]
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
		// indent + glyph + mark + title, truncated to the content width.
		label := indent + m.railGlyph(e.Status) + mark + ansiTrunc(title, contentW-3-len(indent))
		lines = append(lines, railPad(label, rw))
	}
	// Pad the rest of the column with empty gutters.
	for len(lines) < h {
		lines = append(lines, railPad("", rw))
	}
	return lines
}

// railPad pads a (possibly styled) label to width w in display columns and
// appends a dim vertical separator as the gutter's last column.
func railPad(label string, w int) string {
	plainW := ansi.StringWidth(label)
	inner := w - 2 // reserve two columns: the separator and a gutter space
	pad := inner - plainW
	if pad < 0 {
		pad = 0
	}
	return label + strings.Repeat(" ", pad) + dim("│") + " "
}

// transcriptBand renders the transcript viewport, prefixed with the rail column
// (left) and suffixed with the changes panel (right) when each is visible.
// Returns exactly vp.Height rows joined by newlines (no trailing newline), so
// it slots into View where m.vp.View() used to.
func (m *model) transcriptBand() string {
	railOn := m.railWidth() > 0
	chgOn := m.rightPanelWidth() > 0
	if !railOn && !chgOn {
		return m.vp.View()
	}
	vpLines := strings.Split(m.vp.View(), "\n")
	var railLines, chgLines []string
	if railOn {
		if m.sidebarVisible() {
			railLines = m.sidebarLines(m.vp.Height)
		} else {
			railLines = m.railLines(m.vp.Height)
		}
	}
	if chgOn {
		chgLines = m.changesLines(m.vp.Height)
	}
	var b strings.Builder
	for i := 0; i < m.vp.Height; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		if railOn && i < len(railLines) {
			b.WriteString(railLines[i])
		}
		if i < len(vpLines) {
			b.WriteString(vpLines[i])
		}
		if chgOn && i < len(chgLines) {
			b.WriteString(chgLines[i])
		}
	}
	return b.String()
}

// railRowAt maps a rail-local y to the row model built by railRows.
// Row 0 is the "sessions" panel header; rows start at 1.
func (m *model) railRowAt(localY int) (railRow, bool) {
	rows := m.railRows()
	idx := localY - 1 // row 0 is the panel header
	if idx < 0 || idx >= len(rows) {
		return railRow{}, false
	}
	return rows[idx], true
}

// toggleRailProject flips a project header's collapsed state (UI-local).
func (m *model) toggleRailProject(dir string) {
	if m.railCollapsed == nil {
		m.railCollapsed = map[string]bool{}
	}
	m.railCollapsed[dir] = !m.railCollapsed[dir]
}

// toggleRailProjects collapses every project (or expands all when any is
// collapsed) — the keyboard-parity path for the header clicks.
func (m *model) toggleRailProjects() {
	any := false
	for _, c := range m.railCollapsed {
		if c {
			any = true
			break
		}
	}
	if any {
		m.railCollapsed = map[string]bool{}
		return
	}
	if m.railCollapsed == nil {
		m.railCollapsed = map[string]bool{}
	}
	for _, e := range m.railEntries {
		m.railCollapsed[e.Dir] = true
	}
}

// hopToSession leaves this window to the given session id via the SAME path as
// the switcher's enter (Detach in Run keeps the daemon turn running). A click
// on the current session is a no-op.
func (m *model) hopToSession(id string) tea.Cmd {
	if sl := m.railLister(); sl == nil || id == "" || id == sl.SessionID() {
		return nil
	}
	m.switchTo = id
	return tea.Quit
}

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
	"github.com/avifenesh/eigen/internal/theme"
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

// toolSpinnerFrames animates an in-flight TOOL call in the transcript — a
// distinct, faster churn from the breathing-λ "session is working" signature
// (a running tool is a unit of work spinning, not the agent's pulse).
var toolSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

// siblingSessionCount returns how many OTHER daemon sessions are alive besides
// this window's (0 for a local chat or a lone daemon session). Used to warn
// before a production /rebuild interrupts them all.
func (m *model) siblingSessionCount() int {
	sl := m.railLister()
	if sl == nil {
		return 0
	}
	cur := sl.SessionID()
	n := 0
	for _, e := range sl.Sessions() {
		if e.ID != cur && e.Turns > 0 {
			n++
		}
	}
	return n
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

// clampScrollOffset clamps a list scroll offset so the final page is full when
// possible. visible is the number of list rows available below any fixed header.
func clampScrollOffset(offset, total, visible int) int {
	if visible <= 0 || total <= visible {
		return 0
	}
	maxScroll := total - visible
	if offset < 0 {
		return 0
	}
	if offset > maxScroll {
		return maxScroll
	}
	return offset
}

// clampRailScroll keeps the shared rail/session-list scroll valid as sessions
// are added/removed, projects collapse, or the terminal is resized.
func (m *model) clampRailScroll(visible int) int {
	m.railScroll = clampScrollOffset(m.railScroll, len(m.railRows()), visible)
	return m.railScroll
}

// scrollRail moves the rail/session-list viewport by delta rows.
func (m *model) scrollRail(delta, visible int) {
	m.railScroll = clampScrollOffset(m.railScroll+delta, len(m.railRows()), visible)
}

// visibleRailRows returns the rail rows currently visible in a list window of
// visible rows (excluding fixed headers). It also clamps m.railScroll.
func (m *model) visibleRailRows(visible int) []railRow {
	rows := m.railRows()
	if visible <= 0 || len(rows) == 0 {
		m.clampRailScroll(visible)
		return nil
	}
	start := m.clampRailScroll(visible)
	end := start + visible
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}

// railGlyph maps a session status to its rail glyph; working sessions animate
// (frame advanced by the rail tick) so liveness is visible at a glance.
func (m *model) railGlyph(status string) string {
	if status == "working" {
		return workingLambda(m.railSpin) // breathing λ — the one working signature, matches the app
	}
	return statusGlyph(status)
}

// railEntryLabel renders ONE session row, shared by the rail (<80col) and the
// sidebar (≥80col) so the two never drift. The session THIS pane is attached to
// stands out unmistakably — a bold accent ❯ pointer + bright bold name — so
// with several windows open it's instantly clear which one you're driving;
// other sessions get a blank marker + dim name. status glyph leads (●○◆✗/spin).
func (m *model) railEntryLabel(e chat.SessionEntry, cur string, grouped bool, contentW int) string {
	title := e.Title
	if title == "" {
		title = "(untitled)"
	}
	indent := ""
	if grouped {
		indent = " "
	}
	isCur := e.ID == cur
	// Marker: a clear "you are here" pointer for the current session — in the
	// Focus color (NOT brand blue; blue is reserved for structural chrome).
	mark := " "
	if isCur {
		mark = styleFocus.Bold(true).Render("❯")
	}
	// Name color: current = Focus bold (the eye lands here, distinct from the
	// brand-blue chrome); others = dim so they recede.
	nameW := contentW - 3 - len(indent)
	if nameW < 1 {
		nameW = 1
	}
	name := ansiTrunc(title, nameW)
	if isCur {
		name = styleFocus.Bold(true).Render(name)
	} else {
		name = dim(name)
	}
	return indent + m.railGlyph(e.Status) + mark + name
}

// railEntryRow renders a full session row at width rw, choosing its elevation:
// the active session (this pane) sits on the Overlay tint (lifted higher than
// the Surface rail) so "where you are" reads instantly — the clay Focus ❯ +
// name (from railEntryLabel) pop against it. Other sessions sit flat on the
// rail Surface.
func (m *model) railEntryRow(e chat.SessionEntry, cur string, grouped bool, contentW, rw int) string {
	label := m.railEntryLabel(e, cur, grouped, contentW)
	if e.ID == cur {
		return railPadOn(label, rw, surfaceHex(theme.Overlay))
	}
	return railPad(label, rw)
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
		styledName = styleFocus.Render(styledName) // a project open in some pane — non-brand
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
	if h <= 0 {
		return nil
	}
	cur := ""
	if sl := m.railLister(); sl != nil {
		cur = sl.SessionID()
	}
	rw := m.railCols()
	contentW := railContentW(rw)
	grouped := m.railGrouped()
	lines := make([]string, 0, h)
	// Header row for the rail, with a visible close affordance. The session list
	// below it is independently scrollable when it overflows the rail height.
	lines = append(lines, railPad(panelTitleLine("sessions", contentW, true), rw))
	for _, r := range m.visibleRailRows(h - 1) {
		if r.header {
			lines = append(lines, railPad(m.railHeaderLabel(r.dir, contentW), rw))
			continue
		}
		e := m.railEntries[r.entry]
		lines = append(lines, m.railEntryRow(e, cur, grouped, contentW, rw))
	}
	// Pad the rest of the column with empty gutters.
	for len(lines) < h {
		lines = append(lines, railPad("", rw))
	}
	return lines
}

// railPad pads a (possibly styled) label to width w in display columns,
// appends a dim vertical separator as the gutter's last column, and paints the
// whole row on the Surface tint — the elevation that makes the rail read as a
// lifted panel (the "construction" feel) rather than flat fg-on-canvas.
func railPad(label string, w int) string {
	return railPadOn(label, w, surfaceHex(theme.Surface))
}

// railPadOn is railPad with an explicit surface hex, so a single row can sit on
// a different elevation (e.g. the active session / a selected row on Overlay).
// railPadOn renders one rail/sidebar row at total width w on the surface tint:
// a one-column left margin, the label, AT LEAST one space, a dim vertical
// separator, and a trailing gutter space — " <label> … │ ". The guaranteed gap
// before the separator means even a max-width (truncated) label never touches
// the border. Labels should be truncated to railContentW(w) so they fit.
func railPadOn(label string, w int, hex string) string {
	// Layout: the sidebar's surface spans columns 0..w-1 and ENDS exactly at
	// the separator │ (the last cell). The transcript (base) begins at column
	// w — so the separator is the precise boundary between the two looks, with
	// no surface bleeding past it.
	//   " " left margin + label + pad + " " gap + "│"
	plainW := ansi.StringWidth(label)
	inner := w - 3 // 1 left margin + 1 gap + separator (no trailing space)
	if inner < 0 {
		inner = 0
	}
	pad := inner - plainW
	if pad < 0 {
		pad = 0
	}
	row := " " + label + strings.Repeat(" ", pad) + " " + dim("│")
	return fillBG(row, hex, w)
}

// railContentW is the label width a rail row can hold at total width w (so
// callers truncate to the same budget railPadOn reserves).
func railContentW(w int) int {
	if w-3 < 0 {
		return 0
	}
	return w - 3 // matches railPadOn: margin + gap + separator (no trailing)
}

// transcriptBand renders the transcript viewport, prefixed with the rail column
// (left) and suffixed with the changes panel (right) when each is visible.
// Returns exactly vp.Height rows joined by newlines (no trailing newline), so
// it slots into View where m.vp.View() used to.
func (m *model) transcriptBand() string {
	railOn := m.railWidth() > 0
	chgOn := m.rightPanelWidth() > 0
	baseHex := surfaceHex(theme.Base)
	if !railOn && !chgOn {
		// Plain transcript: still paint it on the Base canvas, full width, so
		// eigen owns every pixel (no terminal bg showing through the gaps).
		vpLines := strings.Split(m.vp.View(), "\n")
		var b strings.Builder
		for i := 0; i < m.vp.Height; i++ {
			if i > 0 {
				b.WriteByte('\n')
			}
			line := ""
			if i < len(vpLines) {
				line = vpLines[i]
			}
			b.WriteString(fillBG(line, baseHex, m.vp.Width))
		}
		return b.String()
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
		// The transcript column is painted on the Base canvas and filled to its
		// full width, so it reads as a deliberate deepest layer beneath the
		// Surface panels (the depth difference) — never a terminal-bg gap.
		line := ""
		if i < len(vpLines) {
			line = vpLines[i]
		}
		b.WriteString(fillBG(line, baseHex, m.vp.Width))
		if chgOn && i < len(chgLines) {
			b.WriteString(chgLines[i])
		}
	}
	return b.String()
}

// railRowAt maps a rail-local y to the row model built by railRows, honoring
// the current rail scroll offset. Row 0 is the fixed "sessions" panel header;
// rows start at 1.
func (m *model) railRowAt(localY, railH int) (railRow, bool) {
	rows := m.railRows()
	idx := localY - 1 + m.clampRailScroll(railH-1) // row 0 is the panel header
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

// anyRailCollapsed reports whether any project is currently collapsed (drives
// the collapse-all button's glyph: collapse when none are, expand when some).
func (m *model) anyRailCollapsed() bool {
	for _, c := range m.railCollapsed {
		if c {
			return true
		}
	}
	return false
}

// toggleRailProjects collapses every project (or expands all when any is
// collapsed) — the keyboard-parity path for the header clicks.
func (m *model) toggleRailProjects() {
	if m.anyRailCollapsed() {
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

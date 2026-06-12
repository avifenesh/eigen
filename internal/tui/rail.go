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
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/chat"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// railWidthCols is the rail's total width (content + a one-column gutter).
const railWidthCols = 22

// railMinTerminalWidth is the narrowest terminal that still shows the rail —
// below this the transcript needs the whole width, so the rail hides (it stays
// reachable via alt+s / the [sessions] header button).
const railMinTerminalWidth = 80

// railPollEvery is how often the rail refreshes the session list + statuses.
const railPollEvery = 1200 * time.Millisecond

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

// railWidth is the rail's column width (0 when hidden).
func (m *model) railWidth() int {
	if !m.railVisible() {
		return 0
	}
	return railWidthCols
}

// railTickMsg drives the periodic rail refresh.
type railTickMsg struct{}

// railTick schedules the next rail refresh (nil when the rail is hidden, so it
// costs nothing for local chats).
func (m *model) railTick() tea.Cmd {
	if m.railLister() == nil {
		return nil
	}
	return tea.Tick(railPollEvery, func(time.Time) tea.Msg { return railTickMsg{} })
}

// refreshRail re-reads the session list from the backend (cheap, in-process for
// the daemon client's cached snapshot).
func (m *model) refreshRail() {
	if sl := m.railLister(); sl != nil {
		m.railEntries = sl.Sessions()
	}
}

// railLines renders the rail as exactly h lines, each padded to the full rail
// width (content + gutter). The current session is marked; status glyphs use
// the shared ●○◆✗ language.
func (m *model) railLines(h int) []string {
	cur := ""
	if sl := m.railLister(); sl != nil {
		cur = sl.SessionID()
	}
	contentW := railWidthCols - 2 // leave a 2-col gutter (" │")
	lines := make([]string, 0, h)
	// Header row for the rail, with a visible close affordance.
	lines = append(lines, railPad(panelTitleLine("sessions", railWidthCols-1, true), railWidthCols))
	for _, e := range m.railEntries {
		if len(lines) >= h {
			break
		}
		title := e.Title
		if title == "" {
			title = "(untitled)"
		}
		mark := " "
		if e.ID == cur {
			mark = styleAccent.Render("·")
		}
		// glyph + mark + title, truncated to the content width.
		label := statusGlyph(e.Status) + mark + ansiTrunc(title, contentW-3)
		lines = append(lines, railPad(label, railWidthCols))
	}
	// Pad the rest of the column with empty gutters.
	for len(lines) < h {
		lines = append(lines, railPad("", railWidthCols))
	}
	return lines
}

// railPad pads a (possibly styled) label to width w in display columns and
// appends a dim vertical separator as the gutter's last column.
func railPad(label string, w int) string {
	plainW := ansi.StringWidth(label)
	inner := w - 1 // last column is the separator
	pad := inner - plainW
	if pad < 0 {
		pad = 0
	}
	return label + strings.Repeat(" ", pad) + dim("│")
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
		railLines = m.railLines(m.vp.Height)
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

// railRowAt maps a rail-local (x,y) click to a session entry index, or -1.
// Row 0 is the "sessions" header; entries start at row 1.
func (m *model) railRowAt(localY int) int {
	idx := localY - 1 // row 0 is the header
	if idx < 0 || idx >= len(m.railEntries) {
		return -1
	}
	return idx
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

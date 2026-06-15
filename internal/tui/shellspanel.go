package tui

import (
	"fmt"
	"time"

	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// The [shells] right-panel tab: a human-facing view of the agent's backgrounded
// bash commands (bash background=true / detach). Each row is one shell — id,
// status, command, last output line — and a running shell can be killed from
// the panel. Data comes from the backend (Local reads the in-memory registry;
// Remote reads the daemon SessionState snapshot), so there is NO disk and no
// startup hazard: shells live exactly as long as the daemon session that owns
// them.

const shellsRefresh = 1500 * time.Millisecond

type shellsState struct {
	expanded string // shell id whose output tail is expanded
	sel      int
	ticking  bool
	gen      int
}

type shellsTickMsg struct{ gen int }

// backendShells returns the current shells from the backend (nil-safe).
func (m *model) backendShells() []chat.ShellInfo {
	if m.backend == nil {
		return nil
	}
	return m.backend.Shells()
}

// shellsTick schedules the next poll while the shells tab is visible.
func (m *model) shellsTick() tea.Cmd {
	if m.rightTab != rightTabShells || !m.changesVisible() {
		m.shells.ticking = false
		return nil
	}
	m.shells.ticking = true
	gen := m.shells.gen
	return tea.Tick(shellsRefresh, func(time.Time) tea.Msg { return shellsTickMsg{gen: gen} })
}

// shellRow is one rendered row in the shells panel (renderer + hit-test share
// this model so geometry can't drift).
type shellRow struct {
	kind  int // shellRowItem | shellRowDetail | shellRowKill | shellRowEmpty
	shell chat.ShellInfo
}

const (
	shellRowItem = iota
	shellRowDetail
	shellRowKill
	shellRowEmpty
)

// shellsRows builds the row model for the panel.
func (m *model) shellsRows() []shellRow {
	shells := m.backendShells()
	if len(shells) == 0 {
		return []shellRow{{kind: shellRowEmpty}}
	}
	if m.shells.sel >= len(shells) {
		m.shells.sel = len(shells) - 1
	}
	if m.shells.sel < 0 {
		m.shells.sel = 0
	}
	var rows []shellRow
	for _, s := range shells {
		rows = append(rows, shellRow{kind: shellRowItem, shell: s})
		if s.ID == m.shells.expanded {
			rows = append(rows, shellRow{kind: shellRowDetail, shell: s})
			if s.Status == "running" {
				rows = append(rows, shellRow{kind: shellRowKill, shell: s})
			}
		}
	}
	return rows
}

func shellGlyph(status string) string {
	switch status {
	case "running":
		return theme.StatusWorking
	case "killed":
		return theme.StatusError
	default: // exited
		return theme.StatusIdle
	}
}

// shellsLines renders the panel body (height h). Mirrors tasksLines: the title
// line first, each row padded full-width on Surface, padded to height h.
func (m *model) shellsLines(h int) []string {
	pw := m.rightCols()
	contentW := pw - 4
	if contentW < 8 {
		contentW = 8
	}
	lines := make([]string, 0, h)
	lines = append(lines, changesPad(m.rightPanelTitleLine(pw-2), pw))
	rows := m.shellsRows()
	for i := 0; i < len(rows) && len(lines) < h; i++ {
		r := rows[i]
		var s string
		switch r.kind {
		case shellRowEmpty:
			s = dim("no background shells — the agent")
			lines = append(lines, changesPad(s, pw))
			if len(lines) < h {
				lines = append(lines, changesPad(dim("backgrounds long commands here"), pw))
			}
			continue
		case shellRowItem:
			head := fmt.Sprintf("%s %s", shellGlyph(r.shell.Status), r.shell.ID)
			if r.shell.Status != "running" {
				head += fmt.Sprintf(" (exit %d)", r.shell.ExitCode)
			}
			body := head + "  " + dim(compact(r.shell.Command))
			s = selectLine(r.shell.ID == m.selectedShellID(), ansiTrunc(body, contentW))
		case shellRowDetail:
			last := r.shell.LastLine
			if last == "" {
				last = "(no output yet)"
			}
			s = "    " + dim(ansiTrunc(last, contentW-4))
		case shellRowKill:
			s = "    " + styleErr.Render("[kill]")
		}
		lines = append(lines, changesPad(ansiTrunc(s, contentW+2), pw))
	}
	for len(lines) < h {
		lines = append(lines, changesPad("", pw))
	}
	return lines
}

// selectedShellID returns the id of the currently-selected shell ("" if none).
func (m *model) selectedShellID() string {
	shells := m.backendShells()
	if m.shells.sel >= 0 && m.shells.sel < len(shells) {
		return shells[m.shells.sel].ID
	}
	return ""
}

// killBgShell stops a shell via the backend and flashes feedback.
func (m *model) killBgShell(id string) {
	if m.backend == nil {
		return
	}
	if m.backend.KillShell(id) {
		m.showFlash("killed " + id)
	} else {
		m.showFlashTone("can't kill "+id+" (not running?)", flashWarn)
	}
}

// shellsRowAt maps a content-local click in the shells panel to a row action:
// clicking an item selects/expands it; clicking the [kill] row kills it. The
// panel's first line (localY 0) is the tab header, so body rows start at 1.
func (m *model) shellsRowAt(localY int) (selectIdx int, killID string, ok bool) {
	if localY <= 0 {
		return 0, "", false
	}
	rows := m.shellsRows()
	i := localY - 1
	if i < 0 || i >= len(rows) {
		return 0, "", false
	}
	r := rows[i]
	switch r.kind {
	case shellRowItem:
		idx := 0
		for j := 0; j <= i; j++ {
			if rows[j].kind == shellRowItem {
				if j == i {
					return idx, "", true
				}
				idx++
			}
		}
	case shellRowKill:
		return 0, r.shell.ID, true
	}
	return 0, "", false
}

// shellsClick handles a click in the shells panel: select+expand an item, or
// kill via the [kill] row.
func (m *model) shellsClick(localY int) tea.Cmd {
	sel, killID, ok := m.shellsRowAt(localY)
	if !ok {
		return nil
	}
	if killID != "" {
		m.killBgShell(killID)
		return nil
	}
	m.shells.sel = sel
	shells := m.backendShells()
	if sel < len(shells) {
		id := shells[sel].ID
		if m.shells.expanded == id {
			m.shells.expanded = ""
		} else {
			m.shells.expanded = id
		}
	}
	return nil
}

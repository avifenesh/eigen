package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/remote"
)

// machinesState is the Machines page: remote eigen targets (saved hosts +
// auto-detected ~/.ssh/config aliases), next to Projects. Drill into a machine
// (enter) to SEE its remote sessions, then open one — or open a session
// directly. Opening runs a view on that machine over ssh (ActionRemote).
type machinesState struct {
	list   list
	clicks clickMap

	// drill-in: viewing one machine's remote sessions
	inside   bool
	mach     int  // index into data.Machines
	loading  bool // fetching the remote session list
	loadErr  string
	sessions []daemon.SessionInfo
	inner    list
}

func (s *machinesState) init(d *Data) { s.list.count = len(d.Machines) }

// machineSessionsMsg delivers a remote machine's session list (async ssh peek).
type machineSessionsMsg struct {
	mach     int
	sessions []daemon.SessionInfo
	err      string
}

// fetchMachineSessions lists a remote daemon's sessions over a transient ssh
// connection (read-only peek), off the UI goroutine.
func fetchMachineSessions(mach int, target string) tea.Cmd {
	return func() tea.Msg {
		infos, err := remote.ListSessions(target)
		msg := machineSessionsMsg{mach: mach, sessions: infos}
		if err != nil {
			msg.err = err.Error()
		}
		return msg
	}
}

func (s *machinesState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	visible := m.height - 6
	if s.inside {
		return s.updateInside(m, key, visible)
	}
	s.list.count = len(m.data.Machines)
	if s.list.key(key, visible) {
		return m, nil
	}
	switch key {
	case "enter":
		if s.list.cursor < len(m.data.Machines) {
			return s.openMachine(m, s.list.cursor)
		}
	case "n":
		// New session on the highlighted machine (skip the list).
		if s.list.cursor < len(m.data.Machines) {
			m.result = Result{Action: ActionRemote, Host: m.data.Machines[s.list.cursor].Name}
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// openMachine drills into a machine and kicks off the async session fetch.
func (s *machinesState) openMachine(m *Model, idx int) (tea.Model, tea.Cmd) {
	s.inside = true
	s.mach = idx
	s.loading = true
	s.loadErr = ""
	s.sessions = nil
	s.inner = list{}
	return m, fetchMachineSessions(idx, m.data.Machines[idx].SSH)
}

func (s *machinesState) updateInside(m *Model, key string, visible int) (tea.Model, tea.Cmd) {
	s.inner.count = len(s.sessions)
	if s.inner.key(key, visible) {
		return m, nil
	}
	mc := m.data.Machines[s.mach]
	switch key {
	case "esc", "backspace":
		s.inside = false
	case "n":
		// New session on this machine.
		m.result = Result{Action: ActionRemote, Host: mc.Name}
		m.quitting = true
		return m, tea.Quit
	case "enter":
		if s.loading {
			return m, nil
		}
		if s.inner.cursor < len(s.sessions) {
			m.result = Result{Action: ActionRemote, Host: mc.Name, SessionID: s.sessions[s.inner.cursor].ID}
			m.quitting = true
			return m, tea.Quit
		}
		// No sessions → enter starts a new one.
		m.result = Result{Action: ActionRemote, Host: mc.Name}
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (s *machinesState) view(m *Model, w, h int) string {
	d := m.data
	s.clicks.reset()
	if s.inside && s.mach < len(d.Machines) {
		return s.viewInside(m, w, h)
	}
	out := pageTitle("machines", fmt.Sprintf("%d known", len(d.Machines)), w)
	if len(d.Machines) == 0 {
		return out + sFaint.Render("  none — add one with `eigen remote add <name> <user@host>`\n  or define a Host in ~/.ssh/config (auto-detected)")
	}
	visible := h - 5
	from, to := s.list.window(visible)
	nameW := 16
	for i := from; i < to; i++ {
		mc := d.Machines[i]
		line := fmt.Sprintf("%s %s %s",
			pad(truncate(mc.Name, nameW), nameW),
			sDim.Render(pad(truncate(mc.SSH, w-40), w-40)),
			machineBadges(mc))
		s.clicks.mark(lineCount(out), i)
		out += row(i == s.list.cursor, line) + "\n"
	}
	out += "\n" + sFaint.Render("  enter open machine (see its sessions) · n new session there")
	return out
}

func (s *machinesState) viewInside(m *Model, w, h int) string {
	mc := m.data.Machines[s.mach]
	out := pageTitle("‹ "+mc.Name, mc.SSH, w)
	if s.loading {
		return out + sFaint.Render("  connecting over ssh, listing sessions…")
	}
	if s.loadErr != "" {
		return out + sErr.Render("  "+truncate(s.loadErr, w-4)) + "\n\n" + sFaint.Render("  enter new session here · esc back")
	}
	if len(s.sessions) == 0 {
		return out + sFaint.Render("  no sessions on this machine yet\n\n  enter start one · esc back")
	}
	visible := h - 5
	from, to := s.inner.window(visible)
	titleW := w - 30
	if titleW < 16 {
		titleW = 16
	}
	for i := from; i < to; i++ {
		in := s.sessions[i]
		title := in.Title
		if title == "" {
			title = "(untitled)"
		}
		line := fmt.Sprintf("%s %s %s %s",
			liveGlyph(in.Status, 0),
			pad(truncate(title, titleW), titleW),
			sViolet.Render(pad(fmt.Sprintf("%d", in.Turns), 4)),
			sDim.Render(relTime(in.Updated)))
		s.clicks.mark(lineCount(out), i)
		out += row(i == s.inner.cursor, line) + "\n"
	}
	out += "\n" + sFaint.Render("  enter open session · n new session here · esc back")
	return out
}

// machineBadges renders source/default tags for a machine row.
func machineBadges(mc remote.Machine) string {
	var b string
	if mc.Saved {
		b += sOk.Render("saved ")
	}
	if mc.Detected {
		b += sViolet.Render("ssh-config ")
	}
	if mc.Model != "" {
		b += sFaint.Render(mc.Model + " ")
	}
	return b
}

// clickAt selects a row; click-again (or enter) drills in / opens.
func (s *machinesState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	idx, ok := s.clicks.at(localY)
	if !ok {
		return nil, false
	}
	if s.inside {
		if idx < 0 || idx >= len(s.sessions) {
			return nil, false
		}
		if s.inner.cursor == idx {
			mc := m.data.Machines[s.mach]
			m.result = Result{Action: ActionRemote, Host: mc.Name, SessionID: s.sessions[idx].ID}
			m.quitting = true
			return tea.Quit, true
		}
		s.inner.cursor = idx
		return nil, true
	}
	if idx < 0 || idx >= len(m.data.Machines) {
		return nil, false
	}
	if s.list.cursor == idx {
		_, cmd := s.openMachine(m, idx)
		return cmd, true
	}
	s.list.cursor = idx
	return nil, true
}

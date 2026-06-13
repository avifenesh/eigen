package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/remote"
)

// machinesState is the Machines page: remote eigen targets (saved hosts +
// auto-detected ~/.ssh/config aliases), next to Projects. Opening one runs a
// session on that machine over ssh (ActionRemote → `eigen --remote <name>`).
type machinesState struct {
	list   list
	clicks clickMap
}

func (s *machinesState) init(d *Data) { s.list.count = len(d.Machines) }

func (s *machinesState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	s.list.count = len(m.data.Machines)
	visible := m.height - 6
	if s.list.key(key, visible) {
		return m, nil
	}
	switch key {
	case "enter":
		if s.list.cursor < len(m.data.Machines) {
			m.result = Result{Action: ActionRemote, Host: m.data.Machines[s.list.cursor].Name}
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (s *machinesState) view(m *Model, w, h int) string {
	d := m.data
	s.clicks.reset()
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
	out += "\n" + sFaint.Render("  enter open a session on this machine (over ssh) · saved hosts carry model/dir defaults")
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

// clickAt selects a machine row; click-again (or enter) opens a session on it.
func (s *machinesState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	idx, ok := s.clicks.at(localY)
	if !ok {
		return nil, false
	}
	if idx < 0 || idx >= len(m.data.Machines) {
		return nil, false
	}
	if s.list.cursor == idx {
		m.result = Result{Action: ActionRemote, Host: m.data.Machines[idx].Name}
		m.quitting = true
		return tea.Quit, true
	}
	s.list.cursor = idx
	return nil, true
}

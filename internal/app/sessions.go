package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// sessionsState: the flat all-sessions list (newest first), resume on enter.
type sessionsState struct {
	list list
}

func (s *sessionsState) init(d *Data) { s.list.count = len(d.Sessions) }

func (s *sessionsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	visible := m.height - 6
	if s.list.key(key, visible) {
		return m, nil
	}
	switch key {
	case "enter":
		if s.list.cursor < len(m.data.Sessions) {
			r := m.data.Sessions[s.list.cursor]
			m.result = Result{Action: ActionResume, SessionID: r.ID, Dir: r.Dir}
			m.quitting = true
			return m, tea.Quit
		}
	case "n":
		m.result = Result{Action: ActionOpenChat}
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (s *sessionsState) view(m *Model, w, h int) string {
	d := m.data
	out := pageTitle("sessions", fmt.Sprintf("%d across all sources", len(d.Sessions)), w)
	if len(d.Sessions) == 0 {
		return out + sFaint.Render("  none yet — press n to start one")
	}
	visible := h - 5
	from, to := s.list.window(visible)
	titleW := w - 36
	if titleW < 16 {
		titleW = 16
	}
	for i := from; i < to; i++ {
		r := d.Sessions[i]
		src := sFaint.Render(pad(r.Source, 9))
		line := fmt.Sprintf("%s %s %s %s",
			pad(truncate(r.Title, titleW), titleW),
			src,
			sViolet.Render(pad(fmt.Sprintf("%d", r.Msgs), 5)),
			sDim.Render(relTime(r.Updated)))
		out += row(i == s.list.cursor, line) + "\n"
	}
	if to < len(d.Sessions) {
		out += sFaint.Render(fmt.Sprintf("  … %d more", len(d.Sessions)-to)) + "\n"
	}
	return out
}

// projectsState: projects (sessions grouped by dir); drill into one with enter.
type projectsState struct {
	list   list
	inside bool // viewing one project's sessions
	proj   int  // index into data.Projects
	inner  list // session list inside a project
}

func (p *projectsState) init(d *Data) { p.list.count = len(d.Projects) }

func (p *projectsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	visible := m.height - 6
	if p.inside {
		proj := m.data.Projects[p.proj]
		p.inner.count = len(proj.Sessions)
		if p.inner.key(key, visible) {
			return m, nil
		}
		switch key {
		case "esc", "backspace":
			p.inside = false
		case "enter":
			if p.inner.cursor < len(proj.Sessions) {
				r := proj.Sessions[p.inner.cursor]
				m.result = Result{Action: ActionResume, SessionID: r.ID, Dir: r.Dir}
				m.quitting = true
				return m, tea.Quit
			}
		case "n":
			m.result = Result{Action: ActionOpenChat, Dir: proj.Dir}
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	if p.list.key(key, visible) {
		return m, nil
	}
	switch key {
	case "enter":
		if p.list.cursor < len(m.data.Projects) {
			p.proj = p.list.cursor
			p.inside = true
			p.inner = list{}
		}
	case "n":
		if p.list.cursor < len(m.data.Projects) {
			m.result = Result{Action: ActionOpenChat, Dir: m.data.Projects[p.list.cursor].Dir}
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (p *projectsState) view(m *Model, w, h int) string {
	d := m.data
	if p.inside && p.proj < len(d.Projects) {
		proj := d.Projects[p.proj]
		out := pageTitle(proj.Name, proj.Dir, w)
		visible := h - 5
		from, to := p.inner.window(visible)
		titleW := w - 26
		if titleW < 16 {
			titleW = 16
		}
		for i := from; i < to; i++ {
			r := proj.Sessions[i]
			line := fmt.Sprintf("%s %s %s",
				pad(truncate(r.Title, titleW), titleW),
				sViolet.Render(pad(fmt.Sprintf("%d", r.Msgs), 5)),
				sDim.Render(relTime(r.Updated)))
			out += row(i == p.inner.cursor, line) + "\n"
		}
		out += "\n" + sFaint.Render("  enter resume · n new session here · esc back")
		return out
	}
	out := pageTitle("projects", fmt.Sprintf("%d known", len(d.Projects)), w)
	if len(d.Projects) == 0 {
		return out + sFaint.Render("  none yet — projects appear as you run sessions in them")
	}
	visible := h - 5
	from, to := p.list.window(visible)
	nameW := 24
	for i := from; i < to; i++ {
		pr := d.Projects[i]
		line := fmt.Sprintf("%s %s %s %s",
			pad(truncate(pr.Name, nameW), nameW),
			sViolet.Render(pad(fmt.Sprintf("%d", len(pr.Sessions)), 4)),
			sDim.Render(pad(relTime(pr.Updated), 5)),
			sFaint.Render(truncate(pr.Dir, w-40)))
		out += row(i == p.list.cursor, line) + "\n"
	}
	out += "\n" + sFaint.Render("  enter open project · n new session in project")
	return out
}

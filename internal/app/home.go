package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// homeState is the landing page: identity, quick stats, recent sessions, and
// (later) the proactive action feed.
type homeState struct {
	list list
}

func (h *homeState) init(d *Data) {
	h.list.count = min(len(d.Sessions), 8)
}

func (h *homeState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if h.list.key(key, 8) {
		return m, nil
	}
	switch key {
	case "enter":
		if h.list.count > 0 && h.list.cursor < len(m.data.Sessions) {
			r := m.data.Sessions[h.list.cursor]
			m.result = openAction(r)
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

func (h *homeState) view(m *Model, w, _ int) string {
	d := m.data
	s := pageTitle("eigen", "your agent, everywhere", w)

	// Quick stats line: informative at a glance.
	s += fmt.Sprintf("  %s   %s   %s\n\n",
		countLabel(len(d.Sessions), "session"),
		countLabel(len(d.Projects), "project"),
		countLabel(d.Skills.Len(), "skill"))

	s += sDim.Render("  recent") + "\n"
	if len(d.Sessions) == 0 {
		s += sFaint.Render("  no sessions yet — press n to start one") + "\n"
		return s
	}
	limit := min(len(d.Sessions), 8)
	for i := 0; i < limit; i++ {
		r := d.Sessions[i]
		line := fmt.Sprintf("%s %s %s",
			pad(truncate(r.Title, w-30), w-30),
			sViolet.Render(pad(fmt.Sprintf("%d msg", r.Msgs), 8)),
			sDim.Render(relTime(r.Updated)))
		s += row(i == h.list.cursor, line) + "\n"
	}
	s += "\n" + sFaint.Render("  enter resume · n new session · s all sessions · p projects")
	return s
}

// relTime renders a compact relative timestamp ("3h", "2d", "now").
func relTime(unixNano int64) string {
	if unixNano == 0 {
		return ""
	}
	d := time.Since(time.Unix(0, unixNano))
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	}
}

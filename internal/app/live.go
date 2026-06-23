package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/avifenesh/eigen/internal/daemon"
)

// live page: the daemon's running sessions — concurrent work at a glance.
// enter attaches a view; n starts a new daemon session in the cwd; x stops a
// session (with confirm); i interrupts a running turn. Without a daemon the
// page explains how to start one.

type liveState struct {
	list        list
	confirmStop bool
	notice      string
	clicks      clickMap
}

// livePollMsg refreshes the daemon session list while the page is visible.
type livePollMsg struct{}

func livePoll() tea.Cmd {
	return tea.Tick(1200*time.Millisecond, func(time.Time) tea.Msg { return livePollMsg{} })
}

func (s *liveState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	d := m.data
	visible := m.height - 7
	cur := func() *daemon.SessionInfo {
		if s.list.cursor >= 0 && s.list.cursor < len(d.Live) {
			return &d.Live[s.list.cursor]
		}
		return nil
	}
	if s.confirmStop {
		switch key {
		case "y", "Y":
			if in := cur(); in != nil && d.Daemon != nil {
				if err := d.Daemon.Remove(in.ID); err != nil {
					s.notice = "stop failed: " + err.Error()
				} else {
					s.notice = "stopped " + in.ID
					d.refreshLive()
					s.list.count = len(d.Live)
					s.list.clamp()
				}
			}
		default:
			s.notice = "stop cancelled"
		}
		s.confirmStop = false
		return m, nil
	}
	s.list.count = len(d.Live)
	if s.list.key(key, visible) {
		s.notice = ""
		return m, nil
	}
	switch key {
	case "enter":
		if in := cur(); in != nil {
			return m.quitWith(Result{Action: ActionAttach, SessionID: in.ID, Dir: in.Dir})
		}
	case "n":
		if d.Daemon != nil {
			cwd, _ := os.Getwd()
			id, err := d.Daemon.New(cwd, "") // root the session where the app runs
			if err != nil {
				s.notice = "new failed: " + err.Error()
			} else {
				return m.quitWith(Result{Action: ActionAttach, SessionID: id})
			}
		}
	case "i":
		if in := cur(); in != nil && d.Daemon != nil {
			_ = d.Daemon.Interrupt(in.ID)
			s.notice = "interrupted " + in.ID
			d.refreshLive()
		}
	case "x":
		if cur() != nil {
			s.confirmStop = true
		}
	}
	return m, nil
}

// clickAt handles a content-local click on the live page: select a session, or
// attach to it if already selected.
func (s *liveState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	idx, ok := s.clicks.at(localY)
	if !ok {
		return nil, false
	}
	d := m.data
	if idx < 0 || idx >= len(d.Live) {
		return nil, false
	}
	if s.list.cursor == idx {
		in := d.Live[idx]
		_, cmd := m.quitWith(Result{Action: ActionAttach, SessionID: in.ID, Dir: in.Dir})
		return cmd, true
	}
	s.list.cursor = idx
	s.notice = ""
	return nil, true
}

func (s *liveState) view(m *Model, w, h int) string {
	d := m.data
	out := sTitle.Render(" live sessions") + "\n"
	if d.Daemon == nil {
		out += "\n" + sDim.Render("  no daemon running") + "\n\n"
		out += sFaint.Render("  start one with: eigen daemon &") + "\n"
		out += sFaint.Render("  sessions hosted there keep running with no window attached") + "\n"
		return out
	}
	if len(d.Live) == 0 {
		out += "\n" + sDim.Render("  daemon running — no live sessions yet") + "\n\n"
		out += sFaint.Render("  n  start a session here") + "\n"
		return out
	}
	out += liveSummaryLine(d.Live, w) + "\n\n"
	visible := h - 7
	if visible < 3 {
		visible = 3
	}
	s.clicks.reset()
	s.list.count = len(d.Live)
	s.list.clamp() // a poll may have shrunk d.Live; keep the cursor in range
	from, to := s.list.window(visible)
	for i := from; i < to; i++ {
		in := d.Live[i]
		marker := "  "
		style := sRowDim
		if i == s.list.cursor {
			marker = lipgloss.NewStyle().Foreground(cSel).Render("▎ ")
			style = sRowSel
		}
		label := liveLabel(in)
		line := fmt.Sprintf("%s %s %s %s",
			liveGlyph(in.Status, m.liveSpin),
			pad(label, 16),
			sFaint.Render(pad(string(in.Status), 9)),
			sFaint.Render(truncate(in.Dir, w-36)))
		s.clicks.mark(lineCount(out), i)
		out += marker + style.Render(line) + "\n"
	}
	if s.list.cursor >= 0 && s.list.cursor < len(d.Live) {
		out += "\n" + liveSelectedDetail(d.Live[s.list.cursor], w) + "\n"
	}
	out += "\n"
	// a poll may have shrunk d.Live out from under the prompt — cancel it
	if s.confirmStop && (s.list.cursor < 0 || s.list.cursor >= len(d.Live)) {
		s.confirmStop = false
	}
	switch {
	case s.confirmStop:
		in := d.Live[s.list.cursor]
		out += sWarn.Render("  stop session " + in.ID + "? y/n")
	case s.notice != "":
		out += sDim.Render("  " + s.notice)
	default:
		out += sFaint.Render("  enter attach · n new · i interrupt · x stop")
	}
	return out
}

func liveSummaryLine(live []daemon.SessionInfo, w int) string {
	var working, approval, errors, idle, views int
	for _, in := range live {
		views += in.Views
		switch in.Status {
		case daemon.StatusWorking:
			working++
		case daemon.StatusApproval:
			approval++
		case daemon.StatusError:
			errors++
		default:
			idle++
		}
	}
	parts := []string{
		sViolet.Render(fmt.Sprintf("%d sessions", len(live))),
		sOk.Render(fmt.Sprintf("%d working", working)),
		sFaint.Render(fmt.Sprintf("%d idle", idle)),
	}
	if approval > 0 {
		parts = append(parts, sWarn.Render(fmt.Sprintf("%d needs approval", approval)))
	}
	if errors > 0 {
		parts = append(parts, sErr.Render(fmt.Sprintf("%d errors", errors)))
	}
	if views > 0 {
		parts = append(parts, sAccent.Render(fmt.Sprintf("%d view(s)", views)))
	}
	return truncate("  "+strings.Join(parts, sFaint.Render("  ·  ")), w)
}

func liveSelectedDetail(in daemon.SessionInfo, w int) string {
	updated := "unknown"
	if in.Updated > 0 {
		updated = time.Since(time.Unix(0, in.Updated)).Round(time.Second).String() + " ago"
	}
	line := fmt.Sprintf("selected: %s · %s · %d turn(s) · %d view(s) · updated %s", in.ID, in.Model, in.Turns, in.Views, updated)
	return sFaint.Render("  " + truncate(line, max(20, w-2)))
}

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/transcript"
	tea "github.com/charmbracelet/bubbletea"
)

// sessionsState: the flat all-sessions list (newest first), resume on enter.
// Tier 13: type-to-search (/), source filter (s), recency cutoff with show-all
// (a). The cursor walks the FILTERED view; all actions resolve rows through
// it (operate on the row, never a raw index into the full slice).
type sessionsState struct {
	list       list
	filter     sessionFilter
	visIdx     []int // filtered indices into d.Sessions (refreshed each render)
	hidden     int   // rows removed by the recency cutoff
	confirmDel bool  // awaiting y/n to delete the selected session
	notice     string
	clicks     clickMap
}

func (s *sessionsState) init(d *Data) {
	s.filter = sessionFilter{}
	s.refresh(d)
}

// refresh recomputes the filtered view and clamps the cursor.
func (s *sessionsState) refresh(d *Data) {
	s.visIdx, s.hidden = s.filter.filtered(d.Sessions)
	s.list.count = len(s.visIdx)
	s.list.clamp()
}

func (s *sessionsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	visible := m.height - 7
	d := m.data
	cur := func() *SessionRow {
		if s.list.cursor >= 0 && s.list.cursor < len(s.visIdx) {
			return &d.Sessions[s.visIdx[s.list.cursor]]
		}
		return nil
	}
	// Delete confirmation gate.
	if s.confirmDel {
		switch key {
		case "y", "Y":
			if r := cur(); r != nil {
				if r.Source == "daemon" {
					// Durable daemon session: remove via the daemon when one
					// is up (interrupts turns, releases resources) and always
					// clear the disk files.
					if d.Daemon != nil {
						_ = d.Daemon.Remove(r.ID)
					}
					daemon.DeletePersisted(r.ID)
					s.notice = "deleted: " + r.Title
				} else if d.Store != nil && d.Store.Delete(r.ID) {
					s.notice = "deleted: " + r.Title
				}
				d.reloadSessions()
				s.refresh(d)
			}
			s.confirmDel = false
		default:
			s.confirmDel = false
			s.notice = "delete cancelled"
		}
		return m, nil
	}
	// Search/filter keys come before list navigation (while searching they
	// capture EVERYTHING — typing "q" must extend the query, not quit).
	if s.filter.key(key) {
		s.notice = ""
		s.refresh(d)
		return m, nil
	}
	if s.list.key(key, visible) {
		s.notice = ""
		return m, nil
	}
	switch key {
	case "enter":
		if r := cur(); r != nil {
			m.result = openAction(*r)
			m.quitting = true
			return m, tea.Quit
		}
	case "n":
		m.result = Result{Action: ActionOpenChat}
		m.quitting = true
		return m, tea.Quit
	case "d":
		if cur() != nil {
			s.confirmDel = true
		}
	case "e":
		if r := cur(); r != nil {
			dest := exportPath(r)
			if r.Source == "daemon" {
				// The durable transcript is already eigen-native JSONL.
				if msgs, err := transcript.Load(daemon.PersistedTranscriptPath(r.ID)); err != nil {
					s.notice = "export failed: " + err.Error()
				} else if err := transcript.Save(dest, msgs); err != nil {
					s.notice = "export failed: " + err.Error()
				} else {
					s.notice = "exported → " + dest
				}
			} else if d.Store != nil {
				if err := d.Store.Export(r.ID, dest); err != nil {
					s.notice = "export failed: " + err.Error()
				} else {
					s.notice = "exported → " + dest
				}
			}
		}
	}
	return m, nil
}

func (s *sessionsState) view(m *Model, w, h int) string {
	d := m.data
	s.clicks.reset()
	s.visIdx, s.hidden = s.filter.filtered(d.Sessions)
	s.list.count = len(s.visIdx)
	s.list.clamp()
	sub := fmt.Sprintf("%d across all sources", len(d.Sessions))
	if s.filter.active() {
		sub = fmt.Sprintf("%d of %d", len(s.visIdx), len(d.Sessions))
	}
	out := pageTitle("sessions", sub, w)
	if len(d.Sessions) == 0 {
		return out + sFaint.Render("  none yet — press n to start one")
	}
	out += sessionsSummaryLine(d.Sessions, len(s.visIdx), s.hidden, w) + "\n\n"
	visible := h - 8
	from, to := s.list.window(visible)
	titleW := w - 36
	if titleW < 16 {
		titleW = 16
	}
	for i := from; i < to; i++ {
		r := d.Sessions[s.visIdx[i]]
		src := sFaint.Render(pad(r.Source, 9))
		line := fmt.Sprintf("%s %s %s %s",
			pad(truncate(r.Title, titleW), titleW),
			src,
			sViolet.Render(pad(fmt.Sprintf("%d", r.Msgs), 5)),
			sDim.Render(relTime(r.Updated)))
		s.clicks.mark(lineCount(out), i) // this row's content-local line
		out += row(i == s.list.cursor, line) + "\n"
	}
	switch {
	case to < len(s.visIdx):
		out += sFaint.Render(fmt.Sprintf("  … %d more", len(s.visIdx)-to)) + "\n"
	case s.hidden > 0 && !s.filter.active():
		out += sFaint.Render(fmt.Sprintf("  … %d older than 7d — a shows all", s.hidden)) + "\n"
	}
	if s.list.cursor >= 0 && s.list.cursor < len(s.visIdx) {
		out += "\n" + sessionSelectedDetail(d.Sessions[s.visIdx[s.list.cursor]], w) + "\n"
	}
	out += "\n"
	switch {
	case s.confirmDel:
		r := d.Sessions[s.visIdx[s.list.cursor]]
		out += sWarn.Render("  delete \"" + truncate(r.Title, 40) + "\"? y/n")
	case s.filter.searching || s.filter.active():
		out += sViolet.Render("  " + s.filter.statusLine())
		if !s.filter.searching {
			out += sFaint.Render("   esc clear")
		}
	case s.notice != "":
		out += sDim.Render("  " + s.notice)
	default:
		out += sFaint.Render("  enter resume · n new · / search · s source · a all · e export · d delete")
	}
	return out
}

func sessionsSummaryLine(rows []SessionRow, visible, hidden, w int) string {
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Source]++
	}
	parts := []string{fmt.Sprintf("%d total", len(rows)), fmt.Sprintf("%d visible", visible)}
	for _, src := range []string{"daemon", "codex", "eigen"} {
		if counts[src] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", src, counts[src]))
			delete(counts, src)
		}
	}
	for src, n := range counts {
		if src == "" {
			src = "unknown"
		}
		parts = append(parts, fmt.Sprintf("%s %d", src, n))
	}
	if hidden > 0 {
		parts = append(parts, fmt.Sprintf("%d older hidden", hidden))
	}
	return sFaint.Render("  " + truncate(strings.Join(parts, "  ·  "), max(20, w-2)))
}

func sessionSelectedDetail(r SessionRow, w int) string {
	dir := r.Dir
	if dir == "" {
		dir = "unknown dir"
	}
	line := fmt.Sprintf("selected: %s · %s · %d message(s) · %s", r.Source, r.ID, r.Msgs, dir)
	return sFaint.Render("  " + truncate(line, max(20, w-2)))
}

// clickAt handles a content-local click: select the row under the cursor, and
// open it if it was already selected (single click selects, click-again opens;
// enter also opens). Returns (cmd, handled).
func (s *sessionsState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	idx, ok := s.clicks.at(localY)
	if !ok {
		return nil, false
	}
	d := m.data
	if idx < 0 || idx >= len(s.visIdx) {
		return nil, false
	}
	if s.list.cursor == idx {
		// Second click on the selected row → open it.
		m.result = openAction(d.Sessions[s.visIdx[idx]])
		m.quitting = true
		return tea.Quit, true
	}
	s.list.cursor = idx
	s.notice = ""
	return nil, true
}

// exportPath is the default destination for an exported session.
func exportPath(r *SessionRow) string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".eigen", "exports")
	_ = os.MkdirAll(dir, 0o755)
	name := slug(r.Title)
	if name == "" {
		name = r.ID
	}
	return filepath.Join(dir, name+".eigen.jsonl")
}

// slug makes a filesystem-safe short name from a title.
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
		if b.Len() >= 48 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// projectsState: projects (sessions grouped by dir); drill into one with enter.
type projectsState struct {
	list   list
	inside bool // viewing one project's sessions
	proj   int  // index into data.Projects
	inner  list // feed items + sessions inside a project
	feedN  int  // feed items shown inside (cursor 0..feedN-1)
	clicks clickMap
}

func (p *projectsState) init(d *Data) { p.list.count = len(d.Projects) }

func (p *projectsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	visible := m.height - 6
	if p.inside {
		proj := m.data.Projects[p.proj]
		pf := m.data.feedFor(proj.Dir)
		p.feedN = len(pf)
		p.inner.count = p.feedN + len(proj.Sessions)
		if p.inner.key(key, visible) {
			return m, nil
		}
		switch key {
		case "esc", "backspace":
			p.inside = false
		case "enter":
			c := p.inner.cursor
			switch {
			case c < p.feedN:
				it := pf[c]
				m.result = Result{Action: ActionOpenChat, Dir: it.Dir, Task: it.Task}
				m.quitting = true
				return m, tea.Quit
			case c-p.feedN < len(proj.Sessions):
				r := proj.Sessions[c-p.feedN]
				m.result = openAction(r)
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
	p.clicks.reset()
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
			p.clicks.mark(lineCount(out), i)
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
		p.clicks.mark(lineCount(out), i)
		out += row(i == p.list.cursor, line) + "\n"
	}
	out += "\n" + sFaint.Render("  enter open project · n new session in project")
	return out
}

// clickAt handles a content-local click on the projects page. Outside a
// project: select a project, or drill in if already selected. Inside: select a
// session, or resume it if already selected.
func (p *projectsState) clickAt(m *Model, localY int) (tea.Cmd, bool) {
	idx, ok := p.clicks.at(localY)
	if !ok {
		return nil, false
	}
	d := m.data
	if p.inside && p.proj < len(d.Projects) {
		proj := d.Projects[p.proj]
		if idx < 0 || idx >= len(proj.Sessions) {
			return nil, false
		}
		if p.inner.cursor == idx {
			m.result = openAction(proj.Sessions[idx])
			m.quitting = true
			return tea.Quit, true
		}
		p.inner.cursor = idx
		return nil, true
	}
	if idx < 0 || idx >= len(d.Projects) {
		return nil, false
	}
	if p.list.cursor == idx {
		p.proj = idx
		p.inside = true
		p.inner = list{}
		return nil, true
	}
	p.list.cursor = idx
	return nil, true
}

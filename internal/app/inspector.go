package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// inspectorDetail renders the wide-breakpoint right inspector: a contextual
// detail of the active page's currently-selected row. Each page contributes a
// titled key/value block; pages with no meaningful selection fall back to a
// calm hint. Width-bounded (wrapText / truncate) so it never overflows its
// column.
func (m *Model) inspectorDetail(w int) string {
	if w <= 0 {
		return ""
	}
	title, rows, body := m.inspectorFor()
	if title == "" {
		return sFaint.Render("details") + "\n" +
			sFaint.Render(strings.Repeat("─", min(w, 20))) + "\n" +
			sDim.Render(wrapText("select an item to inspect it here", w))
	}
	var b strings.Builder
	b.WriteString(sTitle.Render(truncate(title, w)) + "\n")
	b.WriteString(sFaint.Render(strings.Repeat("─", min(w, 28))) + "\n")
	for _, kv := range rows {
		if kv.v == "" {
			continue
		}
		// "key  value" — key dimmed, value wrapped under a hanging indent when long.
		key := sDim.Render(kv.k)
		line := key + " " + kv.v
		if lipgloss.Width(line) <= w {
			b.WriteString(line + "\n")
			continue
		}
		b.WriteString(key + "\n")
		for _, wl := range strings.Split(wrapText(kv.v, w), "\n") {
			b.WriteString("  " + wl + "\n")
		}
	}
	if body != "" {
		b.WriteString("\n")
		b.WriteString(sDim.Render(wrapText(body, w)))
	}
	return strings.TrimRight(b.String(), "\n")
}

type kv struct{ k, v string }

// inspectorFor returns (title, key/value rows, optional body) for the active
// page's selection. Returns an empty title when there's nothing to inspect.
func (m *Model) inspectorFor() (string, []kv, string) {
	d := m.data
	switch m.active {
	case PageSessions:
		s := &m.sessions
		if s.list.cursor < 0 || s.list.cursor >= len(s.visIdx) {
			return "", nil, ""
		}
		r := d.Sessions[s.visIdx[s.list.cursor]]
		title := r.Title
		if title == "" {
			title = "(untitled)"
		}
		return title, []kv{
			{"source", r.Source},
			{"project", projShort(r.Dir)},
			{"messages", fmt.Sprintf("%d", r.Msgs)},
			{"updated", relTime(r.Updated)},
			{"id", r.ID},
		}, ""

	case PageModels:
		s := &m.models
		if s.list.cursor < 0 || s.list.cursor >= len(s.rows) {
			return "", nil, ""
		}
		r := s.rows[s.list.cursor]
		avail := sErr.Render("unavailable")
		if r.Available {
			avail = sOk.Render("available")
		}
		return r.ID, []kv{
			{"provider", r.Provider},
			{"context", fmt.Sprintf("%dk", r.Window/1000)},
			{"status", avail},
			{"caps", r.Tags},
		}, ""

	case PageProviders:
		s := &m.providers
		if s.list.cursor < 0 || s.list.cursor >= len(s.rows) {
			return "", nil, ""
		}
		r := s.rows[s.list.cursor]
		status := sErr.Render("no credentials")
		if r.Available {
			status = sOk.Render("ready")
		}
		return r.Name, []kv{
			{"status", status},
			{"default", r.Default},
			{"models", fmt.Sprintf("%d", r.Models)},
		}, ""

	case PageCrons:
		s := &m.crons
		if s.list.cursor < 0 || s.list.cursor >= len(s.rows) {
			return "", nil, ""
		}
		r := s.rows[s.list.cursor]
		state := sDim.Render("inactive")
		if r.Active {
			state = sOk.Render("active")
		}
		return r.Name, []kv{
			{"kind", r.Kind},
			{"state", state},
			{"next", r.Next},
			{"last", r.Last},
		}, r.Command

	case PagePlugins:
		s := &m.plugins
		s.load()
		switch s.tab {
		case pluginsTabInstalled:
			if s.list.cursor < 0 || s.list.cursor >= len(s.installed) {
				return "", nil, ""
			}
			pl := s.installed[s.list.cursor]
			reg, _ := appPluginRegistry()
			state := sOk.Render("enabled")
			if !pluginEnabled(pl, s.rows, reg) {
				state = sDim.Render("disabled")
			}
			return pl.Name, []kv{
				{"state", state},
				{"market", pl.Marketplace},
				{"version", pl.Version},
				{"components", pluginCounts(pl)},
				{"root", pl.Root},
			}, pl.Description
		case pluginsTabMarketplace:
			if s.list.cursor < 0 || s.list.cursor >= len(s.markets) {
				return "", nil, ""
			}
			mk := s.markets[s.list.cursor]
			state := sOk.Render("enabled")
			if mk.Disabled {
				state = sDim.Render("disabled")
			}
			return mk.Name, []kv{
				{"state", state},
				{"source", mk.Source},
				{"owner", mk.Owner},
				{"added", dateLabel(mk.Added)},
				{"updated", dateLabel(mk.Updated)},
			}, "space enable/disable · enter/U refresh catalog · X delete marketplace"
		case pluginsTabExtensions, pluginsTabHooks:
			r, ok := s.selectedExtensionRow()
			if !ok {
				return "", nil, ""
			}
			state := sOk.Render("enabled")
			if r.Disabled {
				state = sDim.Render("disabled")
			}
			return r.Name, []kv{
				{"kind", r.Kind},
				{"state", state},
				{"source", r.Source},
			}, r.Detail
		}

	case PageProjects:
		s := &m.projects
		if s.list.cursor < 0 || s.list.cursor >= len(d.Projects) {
			return "", nil, ""
		}
		r := d.Projects[s.list.cursor]
		return r.Name, []kv{
			{"sessions", fmt.Sprintf("%d", len(r.Sessions))},
			{"updated", relTime(r.Updated)},
			{"dir", r.Dir},
		}, ""

	case PageSkills:
		skills := d.Skills.List()
		if m.skills.list.cursor < 0 || m.skills.list.cursor >= len(skills) {
			return "", nil, ""
		}
		sk := skills[m.skills.list.cursor]
		return sk.Name, []kv{
			{"path", sk.Path},
		}, sk.Description

	case PageHome:
		// The home cursor walks feed items (0..feedN-1) then recent sessions.
		h := &m.home
		c := h.list.cursor
		switch {
		case c >= 0 && c < h.feedN:
			it := h.feed[c]
			return it.Title, []kv{
				{"kind", it.Kind},
				{"detail", it.Detail},
				{"project", dirLabel(it.Dir)},
				{"url", it.URL},
			}, it.Task // the offered task is the body — the full "what enter does"
		case c >= h.feedN && c-h.feedN < h.sessionN:
			r := d.Sessions[c-h.feedN]
			title := r.Title
			if title == "" {
				title = "(untitled)"
			}
			return title, []kv{
				{"source", r.Source},
				{"messages", fmt.Sprintf("%d", r.Msgs)},
				{"updated", relTime(r.Updated)},
				{"project", dirLabel(r.Dir)},
			}, ""
		}
		return "", nil, ""

	case PageMachines:
		mc := &m.machines
		if mc.list.cursor < 0 || mc.list.cursor >= len(d.Machines) {
			return "", nil, ""
		}
		r := d.Machines[mc.list.cursor]
		src := "saved + ssh config"
		switch {
		case r.Saved && !r.Detected:
			src = "saved (hosts.json)"
		case r.Detected && !r.Saved:
			src = "ssh config"
		}
		return r.Name, []kv{
			{"ssh", r.SSH},
			{"address", r.Addr},
			{"source", src},
			{"model", r.Model},
			{"dir", dirLabel(r.Dir)},
			{"perm", r.Perm},
		}, "enter: open a remote session · i: install eigen here"
	}
	return "", nil, ""
}

// dirLabel renders a directory path for the inspector — the base name with the
// home dir abbreviated, or "—" when empty.
func dirLabel(dir string) string {
	if dir == "" {
		return "—"
	}
	return dir
}

// projShort renders a project dir as its base name (full path is too wide for
// the inspector column); empty dir → "—".
func projShort(dir string) string {
	if dir == "" {
		return "—"
	}
	if i := strings.LastIndexByte(dir, '/'); i >= 0 && i < len(dir)-1 {
		return dir[i+1:]
	}
	return dir
}

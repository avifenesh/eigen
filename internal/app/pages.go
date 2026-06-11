package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/dream"
)

// configState shows AND edits the persistent defaults: cursor over keys,
// enter opens an inline value editor, config.Set validates, Save persists.
type configState struct {
	list    list
	editing bool
	input   string // value being typed
	err     string // last validation error
	saved   string // last saved key (flash feedback)
}

func (c *configState) init(*Data) { c.list.count = len(config.Keys()) }

func (c *configState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	keys := config.Keys()
	c.list.count = len(keys)
	key := msg.String()

	if c.editing {
		switch key {
		case "esc":
			c.editing, c.input, c.err = false, "", ""
		case "enter":
			k := keys[c.list.cursor]
			cfg := m.data.Config
			if err := config.Set(&cfg, k, strings.TrimSpace(c.input)); err != nil {
				c.err = err.Error()
				return m, nil
			}
			if err := config.Save(cfg); err != nil {
				c.err = err.Error()
				return m, nil
			}
			m.data.Config = cfg
			c.editing, c.input, c.err = false, "", ""
			c.saved = k
		case "backspace":
			if len(c.input) > 0 {
				c.input = c.input[:len(c.input)-1]
			}
		default:
			if len(msg.Runes) > 0 {
				c.input += string(msg.Runes)
			} else if key == "space" || key == " " {
				c.input += " "
			}
		}
		return m, nil
	}

	if c.list.key(key, m.height-6) {
		c.saved = ""
		return m, nil
	}
	if key == "enter" {
		c.editing = true
		c.input = config.Get(m.data.Config, keys[c.list.cursor])
		c.err, c.saved = "", ""
	}
	return m, nil
}

func (c *configState) view(m *Model, w, _ int) string {
	out := pageTitle("config", config.Path(), w)
	keys := config.Keys()
	for i, k := range keys {
		v := config.Get(m.data.Config, k)
		if v == "" {
			v = sFaint.Render("(unset)")
		} else {
			v = sText.Render(v)
		}
		if c.editing && i == c.list.cursor {
			v = sAccent.Render(c.input + "▏")
		}
		line := sDim.Render(pad(k, 16)) + v
		if i == c.list.cursor && c.saved == k {
			line += sOk.Render("  ✓ saved")
		}
		out += row(i == c.list.cursor, line) + "\n"
	}
	if c.err != "" {
		out += "\n" + sErr.Render("  "+truncate(c.err, w-4)) + "\n"
	}
	if c.editing {
		out += "\n" + sFaint.Render("  enter save · esc cancel")
	} else {
		out += "\n" + sFaint.Render("  enter edit · defaults for NEW sessions (live: /model /perm /effort)")
	}
	return out
}

// skillsState lists discovered skills with preview on enter.
type skillsState struct {
	list    list
	preview string // body being previewed ("" = list view)
	name    string
}

func (s *skillsState) init(d *Data) { s.list.count = d.Skills.Len() }

func (s *skillsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if s.preview != "" {
		if key == "esc" || key == "backspace" || key == "enter" {
			s.preview, s.name = "", ""
		}
		return m, nil
	}
	if s.list.key(key, m.height-6) {
		return m, nil
	}
	if key == "enter" {
		skills := m.data.Skills.List()
		if s.list.cursor < len(skills) {
			sk := skills[s.list.cursor]
			if body, err := m.data.Skills.Body(sk.Name); err == nil {
				s.preview, s.name = body, sk.Name
			}
		}
	}
	return m, nil
}

func (s *skillsState) view(m *Model, w, h int) string {
	if s.preview != "" {
		out := pageTitle(s.name, "skill", w)
		lines := strings.Split(s.preview, "\n")
		limit := min(len(lines), h-5)
		for _, l := range lines[:limit] {
			out += "  " + sText.Render(truncate(l, w-4)) + "\n"
		}
		if limit < len(lines) {
			out += sFaint.Render(fmt.Sprintf("  … %d more lines", len(lines)-limit)) + "\n"
		}
		out += "\n" + sFaint.Render("  esc back")
		return out
	}
	skills := m.data.Skills.List()
	out := pageTitle("skills", fmt.Sprintf("%d discovered", len(skills)), w)
	if len(skills) == 0 {
		return out + sFaint.Render("  none — add SKILL.md under ~/.eigen/skills/<name>/ or `eigen skill add`")
	}
	visible := h - 5
	from, to := s.list.window(visible)
	for i := from; i < to; i++ {
		sk := skills[i]
		line := pad(sk.Name, 24) + sDim.Render(truncate(sk.Description, w-28))
		out += row(i == s.list.cursor, line) + "\n"
	}
	out += "\n" + sFaint.Render("  enter preview · eigen skill add <src> to install")
	return out
}

// modelsState lists the catalog with availability + capability tags.
type modelsState struct {
	list list
	rows []ModelRow
}

func (s *modelsState) init(*Data) {
	s.rows = Models()
	s.list.count = len(s.rows)
}

func (s *modelsState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s.list.key(msg.String(), m.height-6)
	return m, nil
}

func (s *modelsState) view(m *Model, w, h int) string {
	out := pageTitle("models", fmt.Sprintf("%d in catalog", len(s.rows)), w)
	visible := h - 5
	from, to := s.list.window(visible)
	for i := from; i < to; i++ {
		r := s.rows[i]
		dot := sErr.Render("●")
		if r.Available {
			dot = sOk.Render("●")
		}
		win := fmt.Sprintf("%dk", r.Window/1000)
		line := fmt.Sprintf("%s %s %s %s %s",
			dot,
			pad(truncate(r.ID, 36), 36),
			sDim.Render(pad(r.Provider, 10)),
			sViolet.Render(pad(win, 6)),
			sFaint.Render(r.Tags))
		out += row(i == s.list.cursor, line) + "\n"
	}
	out += "\n" + sFaint.Render("  ● available (credentialed) · default set via /config model")
	return out
}

// providersState shows credential status per provider.
type providersState struct {
	list list
	rows []ProviderRow
}

func (s *providersState) init(*Data) {
	s.rows = Providers()
	s.list.count = len(s.rows)
}

func (s *providersState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s.list.key(msg.String(), m.height-6)
	return m, nil
}

func (s *providersState) view(m *Model, w, h int) string {
	out := pageTitle("providers", "", w)
	from, to := s.list.window(h - 5)
	for i := from; i < to; i++ {
		r := s.rows[i]
		status := sErr.Render("no credentials")
		if r.Available {
			status = sOk.Render("available")
		}
		line := fmt.Sprintf("%s %s %s %s",
			pad(r.Name, 12),
			pad(status, 16),
			sViolet.Render(pad(fmt.Sprintf("%d models", r.Models), 10)),
			sDim.Render("default: "+r.Default))
		out += row(i == s.list.cursor, line) + "\n"
	}
	return out
}

// memoryState shows global memory as selectable bullets: d deletes one
// (with confirm), c consolidates via the small model (async), j/k move.
// Project memory lives with each project.
type memoryState struct {
	list      list
	bullets   []string
	loaded    bool
	confirm   bool   // pending delete confirmation for the selected bullet
	status    string // transient feedback ("consolidating…", errors)
	consoling bool   // consolidation running in the background
}

func (s *memoryState) init(*Data) {}

func (s *memoryState) load(d *Data) {
	if s.loaded {
		return
	}
	s.bullets = nil
	if d.GlobalMem != nil {
		s.bullets = memoryBullets(d.GlobalMem.Read())
	}
	s.list.count = len(s.bullets)
	s.list.clamp()
	s.loaded = true
}

// memoryBullets splits a memory file into its top-level "- " bullets
// (continuation lines belong to the bullet above).
func memoryBullets(content string) []string {
	var bullets []string
	var cur strings.Builder
	flush := func() {
		if b := strings.TrimRight(cur.String(), "\n"); strings.TrimSpace(b) != "" {
			bullets = append(bullets, b)
		}
		cur.Reset()
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "- ") {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
	}
	flush()
	return bullets
}

// consolidateDoneMsg reports the background consolidation result.
type consolidateDoneMsg struct{ err error }

func (s *memoryState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s.load(m.data)
	key := msg.String()

	if s.confirm {
		switch key {
		case "y", "enter":
			s.confirm = false
			if err := s.deleteSelected(m.data); err != nil {
				s.status = "delete failed: " + err.Error()
			} else {
				s.status = "deleted (backup kept)"
				s.loaded = false
				s.load(m.data)
			}
		default:
			s.confirm = false
			s.status = ""
		}
		return m, nil
	}

	if s.list.key(key, m.height-7) {
		s.status = ""
		return m, nil
	}
	switch key {
	case "d":
		if len(s.bullets) > 0 {
			s.confirm = true
		}
	case "C":
		// Consolidate via the small model (capital: c is the config page jump).
		if s.consoling {
			break
		}
		if m.data.Small == nil || m.data.GlobalMem == nil {
			s.status = "consolidation needs the small model (credentials missing?)"
			break
		}
		s.consoling = true
		s.status = "consolidating…"
		gm, small := m.data.GlobalMem, m.data.Small
		return m, func() tea.Msg {
			cur := gm.Read()
			out, err := dream.Consolidate(context.Background(), small, cur)
			if err != nil {
				return consolidateDoneMsg{err: err}
			}
			if _, err := gm.Snapshot(); err != nil {
				return consolidateDoneMsg{err: err}
			}
			return consolidateDoneMsg{err: gm.Rewrite(out)}
		}
	case "R":
		s.loaded = false
		s.load(m.data)
		s.status = ""
	}
	return m, nil
}

// deleteSelected removes the selected bullet, snapshots first, rewrites.
func (s *memoryState) deleteSelected(d *Data) error {
	if d.GlobalMem == nil || s.list.cursor >= len(s.bullets) {
		return fmt.Errorf("nothing selected")
	}
	if _, err := d.GlobalMem.Snapshot(); err != nil {
		return err
	}
	kept := make([]string, 0, len(s.bullets)-1)
	for i, b := range s.bullets {
		if i != s.list.cursor {
			kept = append(kept, b)
		}
	}
	return d.GlobalMem.Rewrite(strings.Join(kept, "\n") + "\n")
}

func (s *memoryState) view(m *Model, w, h int) string {
	s.load(m.data)
	out := pageTitle("memory", "global (cross-project)", w)
	if m.data.GlobalMem == nil {
		return out + sFaint.Render("  unavailable")
	}
	if len(s.bullets) == 0 {
		return out + sFaint.Render("  empty — global notes accumulate from sessions (scope=global)")
	}
	visible := h - 7
	from, to := s.list.window(visible)
	for i := from; i < to; i++ {
		// One line per bullet: first line, truncated.
		first := strings.TrimPrefix(strings.SplitN(s.bullets[i], "\n", 2)[0], "- ")
		out += row(i == s.list.cursor, sText.Render(truncate(first, w-6))) + "\n"
	}
	if s.list.cursor < len(s.bullets) {
		// Detail pane: the selected bullet, wrapped, a few lines.
		out += "\n"
		detail := s.bullets[s.list.cursor]
		lines := strings.Split(detail, "\n")
		for i, l := range lines {
			if i >= 3 {
				out += sFaint.Render(fmt.Sprintf("  … %d more lines", len(lines)-i)) + "\n"
				break
			}
			out += "  " + sDim.Render(truncate(l, w-4)) + "\n"
		}
	}
	switch {
	case s.confirm:
		out += "\n" + sErr.Render("  delete this note? y confirm · any other key cancels")
	case s.status != "":
		out += "\n" + sWarn.Render("  "+s.status)
	default:
		out += "\n" + sFaint.Render("  d delete note · C consolidate (small model) · R refresh")
	}
	return out
}

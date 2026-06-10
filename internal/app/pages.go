package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/config"
)

// configState shows the persistent defaults (read-only v1; edit via /config).
type configState struct{}

func (c *configState) init(*Data) {}
func (c *configState) update(m *Model, _ tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m, nil
}
func (c *configState) view(m *Model, w, _ int) string {
	out := pageTitle("config", config.Path(), w)
	for _, line := range strings.Split(strings.TrimRight(config.View(m.data.Config), "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			out += "  " + sText.Render(line) + "\n"
			continue
		}
		out += "  " + sDim.Render(pad(strings.TrimSpace(k), 16)) + sText.Render(strings.TrimSpace(v)) + "\n"
	}
	out += "\n" + sFaint.Render("  edit: /config <key> <value> in a chat, or edit the file")
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

// memoryState shows global memory (project memory lives with each project).
type memoryState struct {
	scroll int
}

func (s *memoryState) init(*Data) {}

func (s *memoryState) update(m *Model, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		s.scroll++
	case "k", "up":
		if s.scroll > 0 {
			s.scroll--
		}
	case "g", "home":
		s.scroll = 0
	}
	return m, nil
}

func (s *memoryState) view(m *Model, w, h int) string {
	out := pageTitle("memory", "global (cross-project)", w)
	if m.data.GlobalMem == nil {
		return out + sFaint.Render("  unavailable")
	}
	content := m.data.GlobalMem.Read()
	if strings.TrimSpace(content) == "" {
		return out + sFaint.Render("  empty — global notes accumulate from sessions (scope=global)")
	}
	lines := strings.Split(content, "\n")
	if s.scroll >= len(lines) {
		s.scroll = len(lines) - 1
	}
	visible := h - 5
	end := min(s.scroll+visible, len(lines))
	for _, l := range lines[s.scroll:end] {
		out += "  " + sText.Render(truncate(l, w-4)) + "\n"
	}
	if end < len(lines) {
		out += sFaint.Render(fmt.Sprintf("  … %d more (j to scroll)", len(lines)-end)) + "\n"
	}
	out += "\n" + sFaint.Render("  curate: eigen memory show|consolidate [--global]")
	return out
}

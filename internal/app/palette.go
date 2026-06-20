package app

import (
	"sort"
	"strings"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

// command palette: a fuzzy launcher over pages and global actions, opened with
// ':' or ctrl+k. It overlays the active page; Enter runs the selected command.

// paletteCmd is one entry in the palette.
type paletteCmd struct {
	name string // shown + matched
	hint string // right-aligned context (e.g. "page", "action")
	run  func(m *Model) tea.Cmd
}

// palette is the command-palette overlay state.
type palette struct {
	open    bool
	query   string
	cursor  int
	matches []int // indices into cmds matching query
	cmds    []paletteCmd
}

func (p *palette) build(m *Model) {
	p.cmds = p.cmds[:0]
	for _, pg := range pages {
		page := pg.page
		name := pg.name
		p.cmds = append(p.cmds, paletteCmd{
			name: "go: " + name,
			hint: "page",
			run: func(m *Model) tea.Cmd {
				m.setActive(page)
				return nil
			},
		})
	}
	for _, role := range agent.PluginRoleNames() {
		role := role
		p.cmds = append(p.cmds, paletteCmd{
			name: "plugin agent role: " + strings.ReplaceAll(role, "-", " ") + " · " + role,
			hint: "role",
			run: func(m *Model) tea.Cmd {
				m.setActive(PagePlugins)
				m.plugins.setTab(pluginsTabInstalled)
				m.plugins.selectPluginWithAgent(role)
				return nil
			},
		})
	}
	p.cmds = append(p.cmds,
		paletteCmd{name: "new session", hint: "action", run: func(m *Model) tea.Cmd {
			_, cmd := m.quitWith(Result{Action: ActionOpenChat})
			return cmd
		}},
		paletteCmd{name: "quit", hint: "action", run: func(m *Model) tea.Cmd {
			_, cmd := m.quitWith(Result{Action: ActionQuit})
			return cmd
		}},
	)
}

func (p *palette) openPalette(m *Model) {
	p.build(m)
	p.open = true
	p.query = ""
	p.cursor = 0
	p.filter()
}

// filter recomputes matches for the current query (subsequence fuzzy match).
func (p *palette) filter() {
	p.matches = p.matches[:0]
	q := strings.ToLower(p.query)
	type scored struct {
		idx, score int
	}
	var hits []scored
	for i, c := range p.cmds {
		if s, ok := fuzzyScore(strings.ToLower(c.name), q); ok {
			hits = append(hits, scored{i, s})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	for _, h := range hits {
		p.matches = append(p.matches, h.idx)
	}
	if p.cursor >= len(p.matches) {
		p.cursor = len(p.matches) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// update handles keys while the palette is open; returns (consumed, cmd).
func (p *palette) update(m *Model, key string, r []rune) (bool, tea.Cmd) {
	if !p.open {
		return false, nil
	}
	switch key {
	case "esc":
		p.open = false
	case "enter":
		if p.cursor < len(p.matches) {
			cmd := p.cmds[p.matches[p.cursor]].run(m)
			p.open = false
			return true, cmd
		}
		p.open = false
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "ctrl+n":
		if p.cursor < len(p.matches)-1 {
			p.cursor++
		}
	case "backspace":
		if p.query != "" {
			p.query = p.query[:len(p.query)-1]
			p.filter()
		}
	default:
		if len(r) == 1 && r[0] >= 32 {
			p.query += string(r)
			p.filter()
		}
	}
	return true, nil
}

// view renders the palette overlay (a centered box). w is the content width.
func (p *palette) view(w int) string {
	boxW := w - 8
	if boxW > 60 {
		boxW = 60
	}
	if boxW < 24 {
		boxW = 24
	}
	var b strings.Builder
	b.WriteString(sAccent.Render("┌─ ") + sTitle.Render("command") + sAccent.Render(" "+strings.Repeat("─", max(boxW-12, 0))+"┐") + "\n")
	b.WriteString(sAccent.Render("│ ") + sText.Render("› "+p.query) + sFaint.Render("▏") + "\n")
	shown := 0
	for i, idx := range p.matches {
		if shown >= 8 {
			break
		}
		c := p.cmds[idx]
		label := truncate(c.name, boxW-14)
		line := pad(label, boxW-14) + sFaint.Render(pad(c.hint, 8))
		if i == p.cursor {
			b.WriteString(sAccent.Render("│ ") + sRowSel.Render("▸ "+line) + "\n")
		} else {
			b.WriteString(sAccent.Render("│ ") + sRowDim.Render("  "+line) + "\n")
		}
		shown++
	}
	if len(p.matches) == 0 {
		b.WriteString(sAccent.Render("│ ") + sFaint.Render("no matches") + "\n")
	}
	b.WriteString(sAccent.Render("└" + strings.Repeat("─", max(boxW-1, 0)) + "┘"))
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// fuzzyScore returns a subsequence-match score (higher = better), or false if
// q is not a subsequence of s. Empty query matches everything (score 0).
func fuzzyScore(s, q string) (int, bool) {
	if q == "" {
		return 0, true
	}
	score, qi, streak := 0, 0, 0
	for i := 0; i < len(s) && qi < len(q); i++ {
		if s[i] == q[qi] {
			streak++
			score += streak // consecutive matches score more
			if i == 0 || s[i-1] == ' ' || s[i-1] == ':' {
				score += 5 // word-boundary bonus
			}
			qi++
		} else {
			streak = 0
		}
	}
	if qi != len(q) {
		return 0, false
	}
	return score, true
}

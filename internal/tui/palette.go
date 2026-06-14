package tui

// The command palette (Tier 9 Wave 5): one fuzzy launcher (ctrl+k) over every
// action — the registry actions plus the chrome toggles and a few common slash
// commands — so everything clickable is also reachable from the keyboard
// without memorizing bindings (and without fighting tmux/zellij over modifier
// keys). It is the keyboard-parity surface the review asked to pull early.

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/fuzzy"
)

// paletteCmd is one launchable entry: a label, an optional key hint, and how to
// run it — either an action id (validated through dispatch) or a slash command.
type paletteCmd struct {
	label string
	hint  string
	id    actionID // when != actNone, dispatched through m.dispatch
	slash string   // when id == actNone, run as a command line
}

// palette holds the launcher state ("" inactive). query filters the entries;
// idx is the highlighted match.
type palette struct {
	active  bool
	query   string
	idx     int
	matches []paletteCmd
}

// paletteCatalog is the full set of launchable commands, in a sensible default
// order (most-reached first). Actions reuse the registry's gating via dispatch;
// slash entries cover the rest.
func (m *model) paletteCatalog() []paletteCmd {
	return []paletteCmd{
		{label: "switch session", hint: "alt+s", id: actSwitcher},
		{label: "new session", id: actNewSession},
		{label: "home (app shell)", id: actHome},
		{label: "rename session", id: actRename},
		{label: "change model", hint: "ctrl+o", id: actModelPicker},
		{label: "permission posture", hint: "ctrl+a", id: actPermPicker},
		{label: "reasoning effort", hint: "ctrl+e", id: actEffortCycle},
		{label: "live search", id: actSearchCycle},
		{label: "auto-router", id: actRouteToggle},
		{label: "compact conversation", id: actCompactPrompt},
		{label: "config panel", id: actConfigPanel},
		{label: "read answers aloud", id: actReadAloudToggle},
		{label: "voice conversation mode", id: actVoiceToggle, slash: "/voice"},
		{label: "mute mic (conversation mode)", id: actVoiceMute, slash: "/mute"},
		{label: "dictate (speak one message)", id: actDictate, slash: "/dictate"},
		{label: "read last answer aloud", id: actSpeakAnswer, slash: "/speak"},
		{label: "toggle session rail", id: actRailToggle, slash: "/rail"},
		{label: "collapse/expand rail projects", id: actRailCollapse},
		{label: "widen session rail", id: actRailWiden},
		{label: "narrow session rail", id: actRailNarrow},
		{label: "toggle right panel", id: actChangesToggle, slash: "/changes"},
		{label: "widen right panel", id: actPanelWiden},
		{label: "narrow right panel", id: actPanelNarrow},
		{label: "next right panel tab", id: actRightTabNext},
		{label: "terminal command panel", id: actTerminalTab},
		{label: "home (app shell)", id: actHome, slash: "/home"},
		{label: "move running turn to background", id: actBackgroundTurn, slash: "/background"},
		{label: "background tasks panel", id: actTasksTab, slash: "/tasks"},
		{label: "notifications / approvals tray", id: actTray, slash: "/tray"},
		{label: "run a workflow", slash: "/workflow "},
		{label: "find in transcript", slash: "/find "},
		{label: "copy last answer", slash: "/copy"},
		{label: "compact (skip confirm)", slash: "/compact"},
		{label: "clear conversation", slash: "/clear"},
		{label: "set a goal", slash: "/goal "},
		{label: "loop a prompt", slash: "/loop "},
		{label: "skills", slash: "/skills"},
		{label: "tools", slash: "/tools"},
		{label: "export to markdown", slash: "/export"},
		{label: "cross-vendor review", slash: "/review"},
		{label: "rebuild eigen", slash: "/rebuild"},
		{label: "help", slash: "/help"},
		{label: "quit", slash: "/quit"},
	}
}

// openPalette opens the fuzzy launcher.
func (m *model) openPalette() {
	m.pal = palette{active: true}
	m.refilterPalette()
	m.relayout()
}

// refilterPalette recomputes the visible matches for the current query,
// ranking by a simple subsequence/substring score.
func (m *model) refilterPalette() {
	q := strings.ToLower(strings.TrimSpace(m.pal.query))
	all := m.paletteCatalog()
	if q == "" {
		m.pal.matches = all
		m.pal.idx = clampInt(m.pal.idx, len(all))
		return
	}
	type scored struct {
		c paletteCmd
		s int
	}
	var hits []scored
	for _, c := range all {
		if s := fuzzy.Score(c.label, q); s >= 0 {
			hits = append(hits, scored{c, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s < hits[j].s })
	m.pal.matches = make([]paletteCmd, len(hits))
	for i, h := range hits {
		m.pal.matches[i] = h.c
	}
	m.pal.idx = clampInt(m.pal.idx, len(m.pal.matches))
}

// paletteKey handles a key while the palette is active. Returns (cmd, handled).
func (m *model) paletteKey(key string) (tea.Cmd, bool) {
	if !m.pal.active {
		return nil, false
	}
	switch key {
	case "esc", "ctrl+k":
		m.pal = palette{}
		m.relayout()
		return nil, true
	case "up", "ctrl+p", "alt+up", "shift+up":
		if m.pal.idx > 0 {
			m.pal.idx--
		}
		return nil, true
	case "down", "ctrl+n", "alt+down", "shift+down":
		if m.pal.idx < len(m.pal.matches)-1 {
			m.pal.idx++
		}
		return nil, true
	case "enter":
		var sel *paletteCmd
		if m.pal.idx >= 0 && m.pal.idx < len(m.pal.matches) {
			c := m.pal.matches[m.pal.idx]
			sel = &c
		}
		m.pal = palette{}
		m.relayout()
		if sel == nil {
			return nil, true
		}
		if sel.id != actNone {
			return m.dispatch(sel.id), true
		}
		// Slash entries that take an argument (trailing space) prefill the
		// input rather than running immediately.
		if strings.HasSuffix(sel.slash, " ") {
			m.ti.SetValue(sel.slash)
			m.ti.CursorEnd()
			m.resizeInput()
			return nil, true
		}
		return m.command(strings.TrimSpace(sel.slash)), true
	case "backspace":
		if r := []rune(m.pal.query); len(r) > 0 {
			m.pal.query = string(r[:len(r)-1])
			m.refilterPalette()
		}
		return nil, true
	case "ctrl+u":
		m.pal.query = ""
		m.refilterPalette()
		return nil, true
	case "space":
		m.pal.query += " "
		m.refilterPalette()
		return nil, true
	default:
		if key != "" && !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+") {
			m.pal.query += key
			m.refilterPalette()
		}
		return nil, true
	}
}

// paletteView renders the launcher: a query line then the ranked matches.
func (m *model) paletteView() string {
	var b strings.Builder
	b.WriteString(styleUser.Render("⌘ command") + dim("   type to filter · ↑↓ move · enter run · esc cancel") + "\n\n")
	b.WriteString(styleAccent.Render("› ") + m.pal.query + styleAccent.Render("▌") + "\n\n")
	rows := m.height - 6
	if rows < 1 {
		rows = 1
	}
	start := 0
	if m.pal.idx >= rows {
		start = m.pal.idx - rows + 1
	}
	end := start + rows
	if end > len(m.pal.matches) {
		end = len(m.pal.matches)
	}
	for i := start; i < end; i++ {
		c := m.pal.matches[i]
		// Dim entries whose action is currently disabled.
		disabled := false
		if c.id != actNone {
			if a, ok := actionRegistry[c.id]; ok && a.enabled != nil && !a.enabled(m) {
				disabled = true
			}
		}
		label := c.label
		if c.hint != "" {
			label += "  " + dim("("+c.hint+")")
		}
		var line string
		switch {
		case i == m.pal.idx:
			line = selectLine(true, c.label)
			if c.hint != "" {
				line += "  " + dim("("+c.hint+")")
			}
		case disabled:
			line = "  " + dim(c.label)
		default:
			line = "  " + label
		}
		b.WriteString(line + "\n")
	}
	if len(m.pal.matches) == 0 {
		b.WriteString(dim("  (no matches)\n"))
	}
	return b.String()
}

// clampInt clamps idx into [0, n) (or 0 when n==0).
func clampInt(idx, n int) int {
	if n == 0 {
		return 0
	}
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}

package tui

// In-session config panel: bare /config opens a live, editable settings panel
// (the same interaction language as the app shell's config page) instead of a
// read-only table. Cursor over config.Fields(); space cycles small enums in
// place; enter opens a dropdown for closed sets or an inline editor for free
// text; route_providers is a multi-select. Edits validate via config.Set and
// persist via config.Save — these are defaults for NEW sessions (the live
// session changes via /model /perm /effort, noted in the footer).

import (
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/llm"
)

// confPanel holds the panel's transient state.
type confPanel struct {
	active bool
	idx    int    // cursor over config.Fields()
	err    string // last validation error
	saved  string // last saved key (flash feedback)

	editing bool // inline editor (free-text fields)
	input   string

	picking  bool // dropdown (closed-set fields)
	choices  []string
	pickIdx  int
	multiSel map[string]bool // multi-select state (route_providers)
}

// openConfigPanel resets and shows the panel.
func (m *model) openConfigPanel() {
	m.conf = confPanel{active: true}
	m.sync()
}

// confOptionsFor resolves a field's option set (static or dynamic catalog).
func confOptionsFor(f config.Field) []string {
	if len(f.Options) > 0 {
		return f.Options
	}
	switch f.Dynamic {
	case "providers":
		var out []string
		seen := map[string]bool{}
		for _, mi := range llm.Models() {
			if !seen[mi.Provider] {
				seen[mi.Provider] = true
				out = append(out, mi.Provider)
			}
		}
		return out
	case "models":
		var out []string
		for _, mi := range llm.Models() {
			out = append(out, mi.ID)
		}
		return out
	}
	return nil
}

// confSetAndSave validates + persists one key, updating panel feedback.
func (m *model) confSetAndSave(key, value string) bool {
	c := config.Load()
	if err := config.Set(&c, key, value); err != nil {
		m.conf.err = err.Error()
		return false
	}
	if err := config.Save(c); err != nil {
		m.conf.err = err.Error()
		return false
	}
	m.conf.err = ""
	m.conf.saved = key
	return true
}

// confPanelKey handles a key while the panel is open. Returns true when the
// key was consumed.
func (m *model) confPanelKey(key string) bool {
	fields := config.Fields()
	if m.conf.idx >= len(fields) {
		m.conf.idx = 0
	}
	fld := fields[m.conf.idx]

	// Dropdown captures keys while open.
	if m.conf.picking {
		switch key {
		case "esc", "q":
			m.conf.picking = false
		case "up", "k", "ctrl+p":
			if m.conf.pickIdx > 0 {
				m.conf.pickIdx--
			}
		case "down", "j", "ctrl+n":
			if m.conf.pickIdx < len(m.conf.choices)-1 {
				m.conf.pickIdx++
			}
		case " ", "space":
			if fld.Multi { // toggle membership, stay open
				v := m.conf.choices[m.conf.pickIdx]
				m.conf.multiSel[v] = !m.conf.multiSel[v]
			}
		case "enter":
			if fld.Multi {
				var sel []string
				for _, v := range m.conf.choices { // option order, not map order
					if m.conf.multiSel[v] {
						sel = append(sel, v)
					}
				}
				if m.confSetAndSave(fld.Key, strings.Join(sel, " ")) {
					m.conf.picking = false
				}
			} else if m.confSetAndSave(fld.Key, m.conf.choices[m.conf.pickIdx]) {
				m.conf.picking = false
			}
		}
		return true
	}

	// Inline editor captures keys while open.
	if m.conf.editing {
		switch key {
		case "esc":
			m.conf.editing, m.conf.input, m.conf.err = false, "", ""
		case "enter":
			if m.confSetAndSave(fld.Key, strings.TrimSpace(m.conf.input)) {
				m.conf.editing, m.conf.input = false, ""
			}
		case "backspace":
			if len(m.conf.input) > 0 {
				m.conf.input = m.conf.input[:len(m.conf.input)-1]
			}
		case " ", "space":
			m.conf.input += " "
		default:
			if len([]rune(key)) == 1 { // literal character
				m.conf.input += key
			}
		}
		return true
	}

	switch key {
	case "esc", "q":
		m.conf = confPanel{}
		m.sync()
	case "up", "k", "ctrl+p":
		if m.conf.idx > 0 {
			m.conf.idx--
		}
		m.conf.saved, m.conf.err = "", ""
	case "down", "j", "ctrl+n":
		if m.conf.idx < len(fields)-1 {
			m.conf.idx++
		}
		m.conf.saved, m.conf.err = "", ""
	case "enter":
		opts := confOptionsFor(fld)
		if len(opts) == 0 { // free text → inline editor
			m.conf.editing = true
			m.conf.input = config.Get(config.Load(), fld.Key)
			m.conf.err, m.conf.saved = "", ""
			return true
		}
		// Closed set → dropdown, preselected on the current value.
		m.conf.choices = opts
		m.conf.pickIdx = 0
		cur := config.Get(config.Load(), fld.Key)
		if fld.Multi {
			m.conf.multiSel = map[string]bool{}
			for _, v := range strings.Fields(cur) {
				m.conf.multiSel[v] = true
			}
		} else {
			for i, v := range opts {
				if v == cur {
					m.conf.pickIdx = i
					break
				}
			}
		}
		m.conf.picking = true
		m.conf.err, m.conf.saved = "", ""
	case " ", "space":
		// Cycle small static enums in place (gated→auto, true→false).
		if len(fld.Options) > 0 && !fld.Multi {
			cur := config.Get(config.Load(), fld.Key)
			next := fld.Options[0]
			for i, v := range fld.Options {
				if v == cur {
					next = fld.Options[(i+1)%len(fld.Options)]
					break
				}
			}
			m.confSetAndSave(fld.Key, next)
		}
	}
	return true
}

// configPanelView renders the panel (full-screen, like the model picker).
func (m *model) configPanelView() string {
	var b strings.Builder
	cfg := config.Load()
	fields := config.Fields()

	if m.conf.picking {
		fld := fields[m.conf.idx]
		b.WriteString(styleUser.Render("config: "+fld.Key) + dim("   ↑↓ move · enter choose · esc cancel") + "\n\n")
		rows := m.height - 6
		if rows < 3 {
			rows = 3
		}
		start := 0
		if m.conf.pickIdx >= rows {
			start = m.conf.pickIdx - rows + 1
		}
		end := start + rows
		if end > len(m.conf.choices) {
			end = len(m.conf.choices)
		}
		cur := config.Get(cfg, fld.Key)
		for i := start; i < end; i++ {
			v := m.conf.choices[i]
			mark := "  "
			if fld.Multi {
				mark = "○ "
				if m.conf.multiSel[v] {
					mark = "● "
				}
			} else if v == cur {
				mark = "· "
			}
			line := "   " + mark + v
			if i == m.conf.pickIdx {
				line = styleAsk.Render(" › " + mark + v)
			}
			b.WriteString(line + "\n")
		}
		if m.conf.err != "" {
			b.WriteString("\n" + styleErr.Render("  "+m.conf.err) + "\n")
		}
		if fld.Multi {
			b.WriteString("\n" + dim("  space toggle · enter save selection · esc cancel"))
		}
		return b.String()
	}

	b.WriteString(styleUser.Render("config") + dim("  "+config.Path()+"   ↑↓ move · enter edit · esc close") + "\n\n")
	for i, f := range fields {
		v := config.Get(cfg, f.Key)
		if v == "" {
			v = "(unset)"
		}
		if m.conf.editing && i == m.conf.idx {
			v = m.conf.input + "▏"
		}
		raw := fmt.Sprintf("%-16s %s", f.Key, v)
		var line string
		switch {
		case i == m.conf.idx:
			line = styleAsk.Render(" › " + raw)
		default:
			line = "   " + raw
		}
		if i == m.conf.idx && m.conf.saved == f.Key {
			line += styleStatus.Render("  ✓ saved")
		}
		b.WriteString(line + "\n")
	}
	// Description pane for the selected field.
	if m.conf.idx < len(fields) {
		f := fields[m.conf.idx]
		b.WriteString("\n" + dim("  "+f.Desc) + "\n")
	}
	if m.conf.err != "" {
		b.WriteString(styleErr.Render("  "+m.conf.err) + "\n")
	}
	switch {
	case m.conf.editing:
		b.WriteString(dim("  enter save · esc cancel"))
	default:
		f := fields[m.conf.idx]
		switch {
		case f.Multi:
			b.WriteString(dim("  enter pick providers · defaults for NEW sessions (live: /model /perm /effort)"))
		case len(f.Options) > 0:
			b.WriteString(dim("  space next value · enter dropdown · defaults for NEW sessions (live: /model /perm /effort)"))
		case len(confOptionsFor(f)) > 0:
			b.WriteString(dim("  enter dropdown · defaults for NEW sessions (live: /model /perm /effort)"))
		default:
			b.WriteString(dim("  enter edit (free text) · defaults for NEW sessions (live: /model /perm /effort)"))
		}
	}
	return b.String()
}

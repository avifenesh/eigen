package tui

// A small reusable bottom-line overlay (Tier 9 Wave 0/1) for actions that must
// not fire silently: a confirm (y/n) before a destructive change, or a one-line
// text entry (e.g. rename). It captures keys while active and renders a single
// line above the status bar — deliberately lighter than the full pickers, and
// shared by every action that needs "are you sure?" or "type a value".

import (
	"strings"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

type promptKind int

const (
	promptConfirm promptKind = iota // y/n
	promptText                      // free text + enter
)

// overlay is the active confirm/text prompt ("" inactive). onAccept runs with
// the entered text (empty for a confirm) when the user accepts; cancel just
// closes it.
type overlay struct {
	active   bool
	kind     promptKind
	message  string
	value    string
	onAccept func(m *model, value string) tea.Cmd
}

// openConfirm shows a y/n confirmation; run fires on "y".
func (m *model) openConfirm(message string, run func(m *model) tea.Cmd) {
	m.ov = overlay{active: true, kind: promptConfirm, message: message, onAccept: func(m *model, _ string) tea.Cmd { return run(m) }}
	m.relayout()
}

// openText shows a single-line text prompt prefilled with value; accept fires
// with the final text on enter.
func (m *model) openText(message, value string, accept func(m *model, value string) tea.Cmd) {
	m.ov = overlay{active: true, kind: promptText, message: message, value: value, onAccept: accept}
	m.relayout()
}

// overlayKey handles a key while the overlay is active. Returns (cmd, handled).
func (m *model) overlayKey(key string) (tea.Cmd, bool) {
	if !m.ov.active {
		return nil, false
	}
	switch m.ov.kind {
	case promptConfirm:
		switch key {
		case "y", "Y":
			accept := m.ov.onAccept
			m.ov = overlay{}
			m.relayout()
			if accept != nil {
				return accept(m, ""), true
			}
			return nil, true
		case "n", "N", "esc", "q":
			m.ov = overlay{}
			m.relayout()
			return nil, true
		}
		return nil, true // swallow other keys while confirming
	case promptText:
		switch key {
		case "enter":
			accept := m.ov.onAccept
			val := m.ov.value
			m.ov = overlay{}
			m.relayout()
			if accept != nil {
				return accept(m, val), true
			}
			return nil, true
		case "esc":
			m.ov = overlay{}
			m.relayout()
			return nil, true
		case "backspace":
			if r := []rune(m.ov.value); len(r) > 0 {
				m.ov.value = string(r[:len(r)-1])
			}
			return nil, true
		case "ctrl+u":
			m.ov.value = ""
			return nil, true
		case "space", " ":
			m.ov.value += " "
			return nil, true
		default:
			// A single printable rune, or a pasted/typed run of characters
			// (terminals deliver fast input or bracketed paste as one event).
			if key != "" && !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+") {
				m.ov.value += key
			}
			return nil, true
		}
	}
	return nil, true
}

// overlayView renders the active prompt as one line.
func (m *model) overlayView() string {
	if !m.ov.active {
		return ""
	}
	switch m.ov.kind {
	case promptConfirm:
		return styleAsk.Render(m.ov.message) + dim("  [y]es / [n]o")
	case promptText:
		return styleUser.Render(m.ov.message+" ") + m.ov.value + styleAccent.Render("▌") + dim("   enter ok · esc cancel")
	}
	return ""
}

// openPermPicker confirms a permission posture change (security-sensitive, so a
// click confirms rather than blind-toggles). The ctrl+a key keeps its fast path.
func (m *model) openPermPicker() {
	if m.backend == nil {
		return
	}
	cur := m.backend.Perm()
	target := agent.PermAuto
	if cur == agent.PermAuto {
		target = agent.PermGated
	}
	m.openConfirm("permission "+string(cur)+" → "+string(target)+"?", func(m *model) tea.Cmd {
		m.togglePerm()
		return nil
	})
}

// openCompactPrompt confirms an on-demand compaction (it rewrites the
// conversation, so never fire it from a stray click).
func (m *model) openCompactPrompt() {
	if m.backend == nil {
		return
	}
	m.openConfirm("compact the conversation now?", func(m *model) tea.Cmd {
		m.state = stRunning
		m.status = "compacting…"
		m.relayout()
		return tea.Batch(m.sp.Tick, m.compactCmd())
	})
}

// openRename opens a text prompt to rename the session (the single rename
// surface, used by the header title click and /rename's interactive form).
func (m *model) openRename() {
	if m.backend == nil {
		return
	}
	m.openText("rename session:", m.backend.Title(), func(m *model, value string) tea.Cmd {
		m.backend.SetTitle(value)
		m.saveMeta()
		if value == "" {
			m.note("title cleared (reverts to the first-message preview)")
		} else {
			m.note("renamed → " + value)
		}
		return nil
	})
}

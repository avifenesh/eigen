package tui

// The action layer (Tier 9 Wave 0). Keys, slash commands, clickable chrome, and
// the future command palette all dispatch the SAME action ids through ONE
// validated handler — so a click can never bypass the checks a key press
// honors (e.g. "can't mutate mid-turn"). Each action carries a label, an
// enablement predicate over the current model state, and a handler that returns
// an optional tea.Cmd.

import tea "github.com/charmbracelet/bubbletea"

// actionID identifies a dispatchable action. Clickable chrome maps a hit to one
// of these; the registry validates + runs it.
type actionID int

const (
	actNone actionID = iota
	actModelPicker
	actPermPicker
	actEffortCycle
	actSearchCycle
	actRouteToggle
	actCompactPrompt
	actReadAloudToggle
	actVoiceToggle
	actHome
	actSwitcher
	actNewSession
	actConfigPanel
	actRename
	actRailToggle
	actRailCollapse
	actChangesToggle
	actRightTabNext
	actTerminalTab
)

// action is a registry entry: what it's called, whether it's currently allowed,
// and how to run it. enabled guards destructive/unsafe-while-running actions so
// every dispatch path (key/click/palette) is gated identically.
type action struct {
	id      actionID
	label   string
	enabled func(m *model) bool
	run     func(m *model) tea.Cmd
}

// always is an enablement predicate that is always true.
func always(*model) bool { return true }

// idleOnly permits an action only when no turn is running (mutating/replacing
// actions that would race the agent goroutine).
func idleOnly(m *model) bool { return m.state == stInput }

// hasBackend requires a live backend.
func hasBackend(m *model) bool { return m.backend != nil }

// actionRegistry is the single table of dispatchable actions. Keyed by id.
var actionRegistry = map[actionID]action{
	actModelPicker: {
		id: actModelPicker, label: "model",
		enabled: func(m *model) bool { return m.newProvider != nil },
		run:     func(m *model) tea.Cmd { return m.command("/model") },
	},
	actPermPicker: {
		id: actPermPicker, label: "perm",
		enabled: hasBackend,
		// Permission is security-sensitive: a click opens the explicit picker
		// rather than blind-toggling, so an accidental click can't silently
		// drop the gate. (The ctrl+a key keeps its fast toggle.)
		run: func(m *model) tea.Cmd { m.openPermPicker(); return nil },
	},
	actEffortCycle: {
		id: actEffortCycle, label: "effort",
		enabled: func(m *model) bool { return m.backend != nil && m.backend.Effort() != "" },
		run:     func(m *model) tea.Cmd { m.cycleEffort(); return nil },
	},
	actSearchCycle: {
		id: actSearchCycle, label: "search",
		enabled: func(m *model) bool { return m.backend != nil && m.backend.SearchMode() != "" },
		run:     func(m *model) tea.Cmd { m.cycleSearch(); return nil },
	},
	actRouteToggle: {
		id: actRouteToggle, label: "route",
		enabled: func(m *model) bool { return m.router != nil },
		run: func(m *model) tea.Cmd {
			if m.router.Enabled() {
				return m.command("/route off")
			}
			return m.command("/route on")
		},
	},
	actCompactPrompt: {
		id: actCompactPrompt, label: "compact",
		// Compaction replaces the conversation: refuse mid-turn, and even when
		// idle confirm first (a click on the context meter must not silently
		// rewrite history).
		enabled: idleOnly,
		run:     func(m *model) tea.Cmd { m.openCompactPrompt(); return nil },
	},
	actReadAloudToggle: {
		id: actReadAloudToggle, label: "read-aloud",
		enabled: always,
		run:     func(m *model) tea.Cmd { return m.command("/read") },
	},
	actVoiceToggle: {
		id: actVoiceToggle, label: "voice",
		enabled: always,
		run:     func(m *model) tea.Cmd { m.toggleVoice(); return nil },
	},
	actHome: {
		id: actHome, label: "home",
		enabled: always,
		run:     func(m *model) tea.Cmd { return m.command("/home") },
	},
	actSwitcher: {
		id: actSwitcher, label: "sessions",
		enabled: always,
		run:     func(m *model) tea.Cmd { m.openSwitcher(); return nil },
	},
	actNewSession: {
		id: actNewSession, label: "+new",
		enabled: always,
		run:     func(m *model) tea.Cmd { m.openApp = true; return tea.Quit }, // app handles "new"
	},
	actConfigPanel: {
		id: actConfigPanel, label: "config",
		enabled: idleOnly,
		run:     func(m *model) tea.Cmd { m.openConfigPanel(); return nil },
	},
	actRename: {
		id: actRename, label: "rename",
		enabled: hasBackend,
		run:     func(m *model) tea.Cmd { m.openRename(); return nil },
	},
	actRailToggle: {
		id: actRailToggle, label: "session rail",
		enabled: func(m *model) bool { return m.railLister() != nil },
		run: func(m *model) tea.Cmd {
			m.toggleRail()
			return nil
		},
	},
	actRailCollapse: {
		id: actRailCollapse, label: "collapse rail projects",
		enabled: func(m *model) bool { return m.railLister() != nil && m.railGrouped() },
		run: func(m *model) tea.Cmd {
			m.toggleRailProjects()
			return nil
		},
	},
	actChangesToggle: {
		id: actChangesToggle, label: "right panel",
		enabled: always,
		run: func(m *model) tea.Cmd {
			m.toggleChanges()
			return nil
		},
	},
	actRightTabNext: {
		id: actRightTabNext, label: "right panel tab",
		enabled: always,
		run: func(m *model) tea.Cmd {
			return m.nextRightTab()
		},
	},
	actTerminalTab: {
		id: actTerminalTab, label: "terminal panel",
		enabled: always,
		run: func(m *model) tea.Cmd {
			return m.setRightTab(rightTabTerminal)
		},
	},
}

// dispatch runs an action by id through the single validated path: unknown or
// disabled actions are a no-op (with a hint for the disabled case), so clicks
// and keys are gated identically. Returns the action's tea.Cmd (may be nil).
func (m *model) dispatch(id actionID) tea.Cmd {
	if id == actNone {
		return nil
	}
	a, ok := actionRegistry[id]
	if !ok {
		return nil
	}
	if a.enabled != nil && !a.enabled(m) {
		m.note(a.label + " isn't available right now" + disabledHint(m, a.id))
		return nil
	}
	if a.run == nil {
		return nil
	}
	return a.run(m)
}

// disabledHint adds a short reason when an action is blocked, so a click that
// does nothing still explains itself.
func disabledHint(m *model, id actionID) string {
	switch id {
	case actCompactPrompt, actConfigPanel:
		if m.state != stInput {
			return " — press esc to interrupt the running turn first"
		}
	}
	return ""
}

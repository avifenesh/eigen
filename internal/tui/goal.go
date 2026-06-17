package tui

import (
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/chat"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// goalActive returns the current persistent goal, if any. Empty means no goal
// is blocking idle.
func (m *model) goalActive() string {
	if m == nil || m.backend == nil {
		return ""
	}
	return strings.TrimSpace(m.backend.Goal())
}

// goalBackendDrives reports whether the backend itself enforces goal wakeups.
// Daemon sessions do so even when no TUI is attached; local in-process sessions
// need the TUI to auto-continue them.
func (m *model) goalBackendDrives() bool {
	_, ok := m.backend.(*chat.Remote)
	return ok
}

func (m *model) goalJudgeAvailable() bool {
	if m == nil || m.backend == nil {
		return false
	}
	for _, t := range m.backend.Tools() {
		if t.Name == "goal_achieved" {
			return true
		}
	}
	return false
}

// maybeStartGoalOnInit wakes a resumed/attached idle session that already has
// a goal. This covers goals restored from metadata or set by another surface:
// merely rendering "goal active" is not enough; the agent must receive a turn.
func (m *model) maybeStartGoalOnInit() tea.Cmd {
	if m.goalActive() == "" || !m.goalJudgeAvailable() || m.initialTask != "" || m.state != stInput {
		return nil
	}
	return func() tea.Msg { return submitMsg{task: agent.GoalStartInstruction} }
}

// maybeContinueGoal blocks local sessions from going idle while a goal remains
// active. The goal_achieved tool clears the goal only after judge confirmation;
// until then every completed turn schedules the next one. User queued input has
// priority and provider/tool errors are left visible for the user to handle.
func (m *model) maybeContinueGoal(turnErr error) tea.Cmd {
	goal := m.goalActive()
	if goal == "" || !m.goalJudgeAvailable() || turnErr != nil || m.state != stInput || m.goalBackendDrives() {
		return nil
	}
	m.note("goal active — continuing until judge confirms it achieved")
	return m.submit(agent.GoalContinueInstruction)
}

func (m *model) markGoalBackendWorking() bool {
	if m.backend == nil || !m.backend.Running() {
		return false
	}
	m.state = stRunning
	m.attachedRunning = true
	m.status = "working on goal"
	m.turnStarted = time.Now()
	m.relayout()
	return true
}

func (m *model) openGoalPanel() tea.Cmd {
	m.changesOn = true
	return m.setRightTab(rightTabGoal)
}

func (m *model) openGoalEditor() {
	if m.backend == nil {
		return
	}
	m.openText("edit goal:", m.backend.Goal(), func(m *model, value string) tea.Cmd {
		value = strings.TrimSpace(value)
		m.backend.SetGoal(value)
		m.saveMeta()
		if value == "" {
			m.note("goal cleared")
			m.idleGen++
			return nil
		}
		m.note("goal → " + value)
		if m.state == stInput && m.goalJudgeAvailable() {
			if m.goalBackendDrives() {
				if !m.markGoalBackendWorking() {
					m.note("goal active — waiting for daemon to start work")
				}
				return nil
			}
			return m.submit(agent.GoalStartInstruction)
		}
		m.idleGen++
		return nil
	})
}

func (m *model) clearGoalFromPanel() tea.Cmd {
	if m.backend == nil {
		return nil
	}
	m.backend.SetGoal("")
	m.saveMeta()
	m.idleGen++
	m.note("goal cleared")
	return nil
}

type goalRowKind int

const (
	grStatus goalRowKind = iota
	grText
	grBlank
	grActionEdit
	grActionClear
	grHint
)

type goalRow struct {
	kind goalRowKind
	text string
}

func (m *model) goalRows(contentW int) []goalRow {
	goal := m.goalActive()
	if goal == "" {
		return []goalRow{{kind: grStatus, text: "no active goal"}, {kind: grHint, text: "/goal <text> to set a persistent north star"}}
	}
	var rows []goalRow
	rows = append(rows, goalRow{kind: grStatus, text: "◆ goal active"})
	rows = append(rows, goalRow{kind: grHint, text: "agent will not idle until goal_achieved is judge-confirmed"})
	rows = append(rows, goalRow{kind: grBlank})
	wrapped := ansi.Hardwrap(goal, contentW, true)
	for _, ln := range strings.Split(wrapped, "\n") {
		rows = append(rows, goalRow{kind: grText, text: ln})
	}
	rows = append(rows, goalRow{kind: grBlank})
	rows = append(rows, goalRow{kind: grActionEdit, text: "[edit goal]"})
	rows = append(rows, goalRow{kind: grActionClear, text: "[clear]"})
	return rows
}

func (m *model) goalLines(h int) []string {
	pw := m.rightCols()
	contentW := pw - 4
	if contentW < 1 {
		contentW = 1
	}
	rows := m.goalRows(contentW)
	lines := make([]string, 0, h)
	lines = append(lines, changesPad(m.rightPanelTitleLine(pw-2), pw))
	for _, r := range rows {
		if len(lines) >= h {
			break
		}
		label := r.text
		switch r.kind {
		case grStatus:
			if m.goalActive() != "" {
				label = styleAsk.Bold(true).Render(label)
			} else {
				label = dim(label)
			}
		case grText:
			label = styleUser.Render(label)
		case grActionEdit:
			label = styleSel.Bold(true).Render(label)
		case grActionClear:
			label = styleAsk.Render(label)
		case grHint:
			label = dim(label)
		}
		lines = append(lines, changesPad(label, pw))
	}
	for len(lines) < h {
		lines = append(lines, changesPad("", pw))
	}
	return lines
}

func (m *model) goalClick(localY int) tea.Cmd {
	if localY < 1 {
		return nil
	}
	rows := m.goalRows(m.rightCols() - 4)
	idx := localY - 1
	if idx < 0 || idx >= len(rows) {
		return nil
	}
	switch rows[idx].kind {
	case grActionEdit:
		m.openGoalEditor()
	case grActionClear:
		return m.clearGoalFromPanel()
	}
	return nil
}

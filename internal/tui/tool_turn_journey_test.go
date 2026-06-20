package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIToolTurnDrivesPlanChangesAndTaskPanels(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 34})
	typeRunes(m, "update the roadmap")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.state != stRunning {
		t.Fatalf("submitted turn should be running, got %v", m.state)
	}

	m.Update(agentEvent{e: agent.Event{Kind: agent.EventReasoningDelta, Text: "plan then edit"}})
	todos := json.RawMessage(`{"todos":[{"content":"inspect GUI","status":"completed","priority":"high"},{"content":"patch docs","status":"in_progress","priority":"high"}]}`)
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "todo", ToolArgs: todos}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolResult, ToolName: "todo", Result: "updated"}})

	editArgs := json.RawMessage(`{"path":"docs/roadmap.md","old_string":"old phase","new_string":"new premium GUI phase"}`)
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "edit", ToolArgs: editArgs}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolResult, ToolName: "edit", Result: "ok"}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "Updated roadmap and plan."}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventDone, Text: "Updated roadmap and plan."}})
	m.Update(turnDoneMsg{})

	plain := ansi.Strip(m.View())
	for _, want := range []string{"update the roadmap", "Updated roadmap and plan", "inspect GUI", "patch docs"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("tool turn transcript/plan missing %q:\n%s", want, plain)
		}
	}
	if len(m.todos) != 2 || m.todos[1].Status != "in_progress" {
		t.Fatalf("todo tool should populate plan panel, todos=%+v", m.todos)
	}
	changes := m.lastRunChanges()
	if len(changes) != 1 || changes[0].path != "docs/roadmap.md" || changes[0].adds == 0 || changes[0].dels == 0 {
		t.Fatalf("edit tool should populate changes panel, changes=%+v", changes)
	}

	m.setRightTab(rightTabChanges)
	assertViewContains(t, m, "changes after tool turn", "docs/roadmap.md", "new premium GUI phase")
	assertViewFits(t, m, "changes after tool turn")
	m.setRightTab(rightTabTasks)
	assertViewContains(t, m, "tasks after tool turn", "nothing running")
	assertViewFits(t, m, "tasks after tool turn")
}

package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIPlanPanelFeatureJourney(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 24}) // classic chrome shows top plan panel
	args := json.RawMessage(`{"todos":[{"content":"ship gui","status":"in_progress"},{"content":"write tests","status":"pending"}]}`)
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "todo", ToolArgs: args}})
	if len(m.todos) != 2 || m.topHeight() == 0 {
		t.Fatalf("todo tool should populate visible plan panel, todos=%v top=%d", m.todos, m.topHeight())
	}
	plain := ansi.Strip(m.View())
	for _, want := range []string{"plan", "ship gui", "write tests"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plan journey missing %q:\n%s", want, plain)
		}
	}
}

func TestTUILeftRailFeatureJourney(t *testing.T) {
	m := switcherModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.changesOn = false
	m.refreshRail()
	plain := ansi.Strip(m.View())
	for _, want := range []string{"sessions", "first", "current", "third"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("left rail journey missing %q:\n%s", want, plain)
		}
	}
	m.command("/rail")
	if m.railOn {
		t.Fatal("/rail should fold/hide the sessions rail")
	}
	m.command("/rail")
	if !m.railOn {
		t.Fatal("/rail should unfold/show the sessions rail")
	}
	row := -1
	for i, r := range m.sidebarRows() {
		if r.kind == sbRail && !r.rail.header && m.railEntries[r.rail.entry].ID == "s1" {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatal("expected s1 in sidebar rail rows")
	}
	_, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: row})
	if cmd == nil || m.switchTo != "s1" {
		t.Fatalf("clicking rail session should hop to s1, switchTo=%q cmd=%v", m.switchTo, cmd)
	}
}

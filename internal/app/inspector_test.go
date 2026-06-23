package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/avifenesh/eigen/internal/daemon"
)

func TestInspectorSessionsDetail(t *testing.T) {
	m := New(testData())
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40}) // wide → inspector shown
	m.active = PageSessions
	m.sessions.init(m.data)
	// Refresh the filtered index (view does this; mimic by opening the page).
	m.renderPage(160, 40)
	out := m.inspectorDetail(30)
	if !strings.Contains(out, "fix the parser") {
		t.Fatalf("inspector should show the selected session title:\n%s", out)
	}
	if !strings.Contains(out, "messages") || !strings.Contains(out, "source") {
		t.Fatalf("inspector should show session fields:\n%s", out)
	}
}

func TestInspectorModelsDetail(t *testing.T) {
	m := New(testData())
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.active = PageModels
	out := m.inspectorDetail(30)
	if out == "" || !strings.Contains(out, "provider") {
		t.Fatalf("models inspector should show a model's provider:\n%s", out)
	}
}

func TestInspectorEmptyHint(t *testing.T) {
	m := New(testData())
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	// A page with no inspector contribution (memory) falls back to the hint.
	m.active = PageMemory
	out := m.inspectorDetail(30)
	if !strings.Contains(out, "select an item") {
		t.Fatalf("a page with no inspector should fall back to the hint:\n%s", out)
	}
	// Home WITH a selection shows real detail (the selected recent session).
	m.active = PageHome
	m.home.syncFeed(m.data)
	m.home.list.cursor = 0
	if d := m.inspectorDetail(30); strings.Contains(d, "select an item") || !strings.Contains(d, "fix the parser") {
		t.Fatalf("home should inspect the selected row, got:\n%s", d)
	}
}

func TestInspectorLiveDetail(t *testing.T) {
	d := testData()
	d.Live = []daemon.SessionInfo{
		{ID: "live-1", Title: "build feature", Dir: "/home/u/proj-a", Model: "opus", Status: daemon.StatusWorking, Turns: 3, Views: 1, Updated: 2000},
	}
	m := New(d)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.active = PageLive
	out := m.inspectorDetail(30)
	if strings.Contains(out, "select an item") {
		t.Fatalf("live inspector should show the highlighted session, got the hint:\n%s", out)
	}
	if !strings.Contains(out, "build feature") || !strings.Contains(out, "model") || !strings.Contains(out, "opus") {
		t.Fatalf("live inspector should show the session label and model:\n%s", out)
	}
}

func TestInspectorConfigDetail(t *testing.T) {
	m := New(testData())
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.active = PageConfig
	m.config.init(m.data)
	out := m.inspectorDetail(30)
	if strings.Contains(out, "select an item") {
		t.Fatalf("config inspector should show the selected field, got the hint:\n%s", out)
	}
	if !strings.Contains(out, "value") || !strings.Contains(out, "type") {
		t.Fatalf("config inspector should show value and type rows:\n%s", out)
	}
}

func TestInspectorProfileDetail(t *testing.T) {
	m := New(testData())
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.active = PageProfile
	out := m.inspectorDetail(30)
	if strings.Contains(out, "select an item") {
		t.Fatalf("profile inspector should show usage totals, got the hint:\n%s", out)
	}
	if !strings.Contains(out, "sessions") || !strings.Contains(out, "projects") {
		t.Fatalf("profile inspector should show usage totals:\n%s", out)
	}
}

func TestInspectorWidthBounded(t *testing.T) {
	m := New(testData())
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m.active = PageSessions
	m.renderPage(160, 40)
	for _, line := range strings.Split(m.inspectorDetail(24), "\n") {
		if w := lipgloss.Width(line); w > 24 {
			t.Fatalf("inspector line exceeds width 24 (%d): %q", w, line)
		}
	}
}

package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	m.active = PageHome // home has no inspector selection
	out := m.inspectorDetail(30)
	if !strings.Contains(out, "select an item") {
		t.Fatalf("home should fall back to the inspect hint:\n%s", out)
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

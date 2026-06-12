package app

// Tests for the Tier 10 Wave 3 page-local content clicks.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// rowLine returns the content-local line a clickMap recorded for item idx (or
// -1). Tests render the page first so the map is populated.
func rowLine(c *clickMap, idx int) int {
	for line, i := range c.line2idx {
		if i == idx {
			return line
		}
	}
	return -1
}

func TestSessionsClickSelectsThenOpens(t *testing.T) {
	d := feedData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageSessions
	l := m.computeLayout()
	_ = m.sessions.view(m, l.inner.w, l.inner.h) // populate the click map

	// Click row index 1 (not currently selected → selects it).
	line := rowLine(&m.sessions.clicks, 1)
	if line < 0 {
		t.Fatal("sessions row 1 should be in the click map")
	}
	absY := l.inner.y + line
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: absY})
	if m.sessions.list.cursor != 1 {
		t.Fatalf("first click should select row 1, got cursor=%d", m.sessions.list.cursor)
	}
	if m.quitting {
		t.Fatal("first click should only select, not open")
	}
	// Re-render (cursor moved) and click the same row again → opens (quits).
	_ = m.sessions.view(m, l.inner.w, l.inner.h)
	line = rowLine(&m.sessions.clicks, 1)
	absY = l.inner.y + line
	_, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: absY})
	if !m.quitting || cmd == nil {
		t.Fatal("second click on the selected session should open it (quit)")
	}
	if m.result.Action != ActionResume && m.result.Action != ActionAttach {
		t.Fatalf("opening a session should resume/attach, got %v", m.result.Action)
	}
}

func TestHomeClickFeedItemOpens(t *testing.T) {
	d := feedData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageHome
	l := m.computeLayout()
	_ = m.home.view(m, l.inner.w, l.inner.h)
	if m.home.feedN == 0 {
		t.Skip("no feed items in test data")
	}
	// Select feed item 0, then click again to open it.
	line := rowLine(&m.home.clicks, 0)
	if line < 0 {
		t.Fatal("home feed item 0 should be in the click map")
	}
	absY := l.inner.y + line
	m.home.list.cursor = 0 // pre-select so the click activates
	_, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: absY})
	if !m.quitting || cmd == nil {
		t.Fatal("clicking the selected feed item should open a chat")
	}
	if m.result.Action != ActionOpenChat {
		t.Fatalf("feed item should open a chat, got %v", m.result.Action)
	}
}

func TestProjectsClickDrillsIn(t *testing.T) {
	d := feedData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageProjects
	if len(d.Projects) == 0 {
		t.Skip("no projects in test data")
	}
	l := m.computeLayout()
	_ = m.projects.view(m, l.inner.w, l.inner.h)
	line := rowLine(&m.projects.clicks, 0)
	if line < 0 {
		t.Fatal("project 0 should be in the click map")
	}
	m.projects.list.cursor = 0 // pre-select so the click drills in
	absY := l.inner.y + line
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: absY})
	if !m.projects.inside {
		t.Fatal("clicking the selected project should drill into it")
	}
}

func TestContentClickOutsideRowsIsNoop(t *testing.T) {
	d := feedData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageSessions
	l := m.computeLayout()
	_ = m.sessions.view(m, l.inner.w, l.inner.h)
	before := m.sessions.list.cursor
	// Click far below the last row (in the content panel but past the list).
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: l.inner.y + l.inner.h - 1})
	if m.sessions.list.cursor != before || m.quitting {
		t.Fatal("clicking empty content space should be a no-op")
	}
}

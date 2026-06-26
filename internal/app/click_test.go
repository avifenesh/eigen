package app

// Tests for the Tier 10 Wave 3 page-local content clicks.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/observe"
	"github.com/avifenesh/eigen/internal/skill"
	tea "github.com/charmbracelet/bubbletea"
)

// seedSkillsDir writes two minimal SKILL.md files into a temp dir and returns
// the root, so skill.Discover yields a deterministic two-skill set.
func seedSkillsDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, sk := range []struct{ name, desc string }{
		{"alpha-skill", "first test skill"},
		{"beta-skill", "second test skill"},
	} {
		dir := filepath.Join(root, sk.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir skill dir: %v", err)
		}
		body := "---\nname: " + sk.name + "\ndescription: " + sk.desc + "\n---\n\nbody for " + sk.name + "\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write SKILL.md: %v", err)
		}
	}
	return root
}

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

func contentLineY(m *Model, l appLayout, line int) int {
	return l.inner.y + m.contentMissionHeight(l.inner.w) + line
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
	absY := contentLineY(m, l, line)
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
	absY = contentLineY(m, l, line)
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
	absY := contentLineY(m, l, line)
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
	absY := contentLineY(m, l, line)
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: absY})
	if !m.projects.inside {
		t.Fatal("clicking the selected project should drill into it")
	}
}

func TestMissionStripClickIsNoopAndPinned(t *testing.T) {
	d := feedData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageSessions
	l := m.computeLayout()
	v := m.View()
	if !strings.Contains(v, "focus: resume or export history") || !strings.Contains(v, "next: resume session") {
		t.Fatalf("sessions view should show the pinned mission strip:\n%s", v)
	}
	before := m.sessions.list.cursor
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: l.inner.y})
	if m.sessions.list.cursor != before || m.quitting {
		t.Fatalf("clicking mission strip should be a no-op, cursor %d -> %d quitting=%v", before, m.sessions.list.cursor, m.quitting)
	}
	m.scrollContent(5)
	if v := m.View(); !strings.Contains(v, "focus: resume or export history") {
		t.Fatalf("mission strip should stay pinned after scroll:\n%s", v)
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

// TestHomeClickWithLiveSection verifies the home click map stays aligned when a
// "working now" section is inserted above the feed (the live section pushes the
// feed + recent rows down; clicks.mark records actual lines, so the mapping
// must self-adjust).
func TestHomeClickWithLiveSection(t *testing.T) {
	d := feedData()
	d.Live = []daemon.SessionInfo{
		{ID: "s1", Title: "working one", Dir: "/home/u/proj-a", Status: daemon.StatusWorking},
		{ID: "s2", Title: "idle two", Dir: "/home/u/proj-b", Status: daemon.StatusIdle},
	}
	m := New(d)
	m.width, m.height = 120, 36
	m.active = PageHome
	l := m.computeLayout()
	_ = m.home.view(m, l.inner.w, l.inner.h)
	if m.home.feedN == 0 {
		t.Skip("no feed items")
	}
	// Feed item 0's recorded line must point at a row that, when clicked,
	// selects feed item 0 (not a live-section row).
	line := rowLine(&m.home.clicks, 0)
	if line < 0 {
		t.Fatal("feed item 0 should still be in the click map with a live section")
	}
	m.home.list.cursor = 0
	_, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: contentLineY(m, l, line)})
	if !m.quitting || cmd == nil || m.result.Action != ActionOpenChat {
		t.Fatalf("clicking the mapped feed line should open the feed item's chat, got action=%v quitting=%v", m.result.Action, m.quitting)
	}
}

// TestModelsClickSelectsRow verifies a content click on a model row moves the
// selection (the catalog is read-only, so there is no second-click open).
func TestModelsClickSelectsRow(t *testing.T) {
	d := testData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageModels
	if len(m.models.rows) < 2 {
		t.Skip("need at least two catalog models")
	}
	l := m.computeLayout()
	_ = m.models.view(m, l.inner.w, l.inner.h)
	line := rowLine(&m.models.clicks, 1)
	if line < 0 {
		t.Fatal("model row 1 should be in the click map")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: contentLineY(m, l, line)})
	if m.models.list.cursor != 1 {
		t.Fatalf("click should select model row 1, got cursor=%d", m.models.list.cursor)
	}
	if m.quitting {
		t.Fatal("selecting a model must not quit")
	}
}

// TestProvidersClickSelectsRow verifies a content click on a provider row moves
// the selection (credentials are managed externally; no second-click open).
func TestProvidersClickSelectsRow(t *testing.T) {
	d := testData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageProviders
	if len(m.providers.rows) < 2 {
		t.Skip("need at least two providers")
	}
	l := m.computeLayout()
	_ = m.providers.view(m, l.inner.w, l.inner.h)
	line := rowLine(&m.providers.clicks, 1)
	if line < 0 {
		t.Fatal("provider row 1 should be in the click map")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: contentLineY(m, l, line)})
	if m.providers.list.cursor != 1 {
		t.Fatalf("click should select provider row 1, got cursor=%d", m.providers.list.cursor)
	}
}

// TestSkillsClickSelectsThenPreviews verifies first click selects a skill and a
// second click on the selected row opens its preview (same as enter).
func TestSkillsClickSelectsThenPreviews(t *testing.T) {
	d := testData()
	d.Skills = skill.Discover(seedSkillsDir(t)) // deterministic two-skill set
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageSkills
	skills := d.Skills.List()
	if len(skills) < 2 {
		t.Fatalf("seeded skills dir should yield two skills, got %d", len(skills))
	}
	l := m.computeLayout()
	_ = m.skills.view(m, l.inner.w, l.inner.h)
	line := rowLine(&m.skills.clicks, 1)
	if line < 0 {
		t.Fatal("skill row 1 should be in the click map")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: contentLineY(m, l, line)})
	if m.skills.list.cursor != 1 {
		t.Fatalf("first click should select skill row 1, got cursor=%d", m.skills.list.cursor)
	}
	if m.skills.preview != "" {
		t.Fatal("first click should only select, not preview")
	}
	_ = m.skills.view(m, l.inner.w, l.inner.h)
	line = rowLine(&m.skills.clicks, 1)
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: contentLineY(m, l, line)})
	if m.skills.preview == "" {
		t.Fatal("second click on the selected skill should open its preview")
	}
}

// TestCronsClickSelectsThenToggles verifies first click selects a cron row and
// a second click toggles it (a crontab row has no toggle, so it stays selected;
// we assert the select + idempotent re-click path without shelling out).
func TestCronsClickSelectsThenToggles(t *testing.T) {
	d := testData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageCrons
	// Seed deterministic rows so the test never depends on the host's systemd
	// timers or crontab; mark loaded so view() skips the real loader.
	m.crons.rows = []CronRow{
		{Name: "alpha", Kind: "crontab", Next: "@daily", Active: true, Command: "echo a"},
		{Name: "beta", Kind: "crontab", Next: "@hourly", Active: true, Command: "echo b"},
	}
	m.crons.list.count = len(m.crons.rows)
	m.crons.loaded = true
	l := m.computeLayout()
	_ = m.crons.view(m, l.inner.w, l.inner.h)
	line := rowLine(&m.crons.clicks, 1)
	if line < 0 {
		t.Fatal("cron row 1 should be in the click map")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: contentLineY(m, l, line)})
	if m.crons.list.cursor != 1 {
		t.Fatalf("first click should select cron row 1, got cursor=%d", m.crons.list.cursor)
	}
	// Second click on the selected crontab row routes through the toggle path
	// (which reports crontab rows are file-managed) and keeps it selected.
	_ = m.crons.view(m, l.inner.w, l.inner.h)
	line = rowLine(&m.crons.clicks, 1)
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: contentLineY(m, l, line)})
	if m.crons.list.cursor != 1 {
		t.Fatalf("second click should keep cron row 1 selected, got cursor=%d", m.crons.list.cursor)
	}
}

// TestObserveSectionClickScrolls verifies clicking a recorded section line snaps
// that section toward the top of the content viewport.
func TestObserveSectionClickScrolls(t *testing.T) {
	d := testData()
	d.Observe = observe.Summary{
		Records: 99,
		Errors:  map[string]int{"denied": 3, "timeout": 2},
		Notes:   map[string]int{"route": 4, "background": 3},
		Models: map[string]observe.ModelSummary{
			"gpt-5.5": {Turns: 2, InTokens: 100}, "fast": {Turns: 1}, "claude": {Turns: 3},
		},
		Tools:  map[string]observe.ToolSummary{"bash": {Calls: 2}, "read": {Calls: 5}, "edit": {Calls: 3}},
		Hooks:  map[string]observe.HookSummary{"session_start": {Done: 1}, "pre_tool": {Done: 2}},
		Skills: map[string]observe.SkillSummary{"frontend": {Calls: 2}, "backend": {Calls: 1}},
	}
	m := NewAt(d, PageObserve)
	m.width, m.height = 90, 14 // short viewport so the page overflows and scroll is live
	l := m.computeLayout()
	_ = m.observe.view(m, l.inner.w, m.contentBodyHeight(l))
	// Pick a section recorded well below the fold (errors is section index 1+).
	target := -1
	for line, idx := range m.observe.clicks.line2idx {
		if idx >= 2 && (target < 0 || line < target) {
			target = line
		}
	}
	if target <= 0 {
		t.Skip("not enough sections rendered to test a below-fold click")
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.inner.x + 2, Y: contentLineY(m, l, target)})
	if m.contentScroll == 0 {
		t.Fatalf("clicking a below-fold section should scroll the content, scroll=%d target line=%d", m.contentScroll, target)
	}
}

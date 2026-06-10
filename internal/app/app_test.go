package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/skill"
)

func testData() *Data {
	return &Data{
		Sessions: []SessionRow{
			{ID: "s1", Title: "fix the parser", Source: "eigen", Dir: "/home/u/proj-a", Msgs: 10, Updated: 2000},
			{ID: "s2", Title: "add tests", Source: "eigen", Dir: "/home/u/proj-a", Msgs: 4, Updated: 1500},
			{ID: "s3", Title: "research", Source: "claude", Msgs: 7, Updated: 1000},
		},
		Projects: groupProjects([]SessionRow{
			{ID: "s1", Title: "fix the parser", Dir: "/home/u/proj-a", Updated: 2000},
			{ID: "s2", Title: "add tests", Dir: "/home/u/proj-a", Updated: 1500},
		}),
		Skills: skill.Discover(), // empty set
	}
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestAppNavigation(t *testing.T) {
	m := New(testData())
	m.width, m.height = 100, 30

	// Quick-jump to sessions.
	m.Update(key("s"))
	if m.active != PageSessions {
		t.Fatalf("s should jump to sessions, on %v", m.active)
	}
	// Tab cycles forward.
	m.Update(key("tab"))
	if m.active != PageConfig {
		t.Fatalf("tab should cycle to config, on %v", m.active)
	}
	// Jump home.
	m.Update(key("h"))
	if m.active != PageHome {
		t.Fatal("h should jump home")
	}
}

func TestAppResumeFromSessions(t *testing.T) {
	m := New(testData())
	m.width, m.height = 100, 30
	m.Update(key("s"))
	m.Update(key("j")) // move to second session
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter should quit with a resume action")
	}
	if m.result.Action != ActionResume || m.result.SessionID != "s2" {
		t.Fatalf("expected resume s2, got %+v", m.result)
	}
	if m.result.Dir != "/home/u/proj-a" {
		t.Fatalf("resume should carry the project dir, got %q", m.result.Dir)
	}
}

func TestAppProjectDrillAndNewSession(t *testing.T) {
	m := New(testData())
	m.width, m.height = 100, 30
	m.Update(key("p"))
	if m.active != PageProjects {
		t.Fatal("p should jump to projects")
	}
	// Drill into the project.
	m.Update(key("enter"))
	if !m.projects.inside {
		t.Fatal("enter should open the project page")
	}
	// New session in this project.
	_, cmd := m.Update(key("n"))
	if cmd == nil || m.result.Action != ActionOpenChat || m.result.Dir != "/home/u/proj-a" {
		t.Fatalf("n should open a chat in the project dir, got %+v", m.result)
	}
}

func TestAppQuit(t *testing.T) {
	m := New(testData())
	m.width, m.height = 100, 30
	_, cmd := m.Update(key("q"))
	if cmd == nil || m.result.Action != ActionQuit {
		t.Fatal("q should quit")
	}
}

func TestGroupProjects(t *testing.T) {
	rows := []SessionRow{
		{ID: "1", Dir: "/a", Updated: 5},
		{ID: "2", Dir: "/a", Updated: 9},
		{ID: "3", Dir: "/b", Updated: 7},
		{ID: "4", Dir: "", Updated: 99}, // no dir → not grouped
	}
	got := groupProjects(rows)
	if len(got) != 2 {
		t.Fatalf("want 2 projects, got %d", len(got))
	}
	// /a is most recent (9 > 7) → first.
	if got[0].Dir != "/a" || got[0].Updated != 9 || len(got[0].Sessions) != 2 {
		t.Fatalf("project /a wrong: %+v", got[0])
	}
}

func TestViewRendersAllPages(t *testing.T) {
	m := New(testData())
	m.width, m.height = 100, 30
	for _, p := range pages {
		m.active = p.page
		v := m.View()
		if v == "" {
			t.Fatalf("page %s rendered empty", p.name)
		}
		if !strings.Contains(v, "eigen") {
			t.Fatalf("page %s missing rail", p.name)
		}
	}
}

func TestRelTime(t *testing.T) {
	if relTime(0) != "" {
		t.Error("zero time should be empty")
	}
}

func TestSlugAndExportPath(t *testing.T) {
	if slug("Fix the Parser Bug!") != "fix-the-parser-bug" {
		t.Errorf("slug = %q", slug("Fix the Parser Bug!"))
	}
	if slug("") != "" {
		t.Error("empty slug")
	}
	r := &SessionRow{ID: "x", Title: "My Session"}
	p := exportPath(r)
	if !strings.HasSuffix(p, "my-session.eigen.jsonl") {
		t.Errorf("export path = %q", p)
	}
}

func TestSessionsDeleteConfirmFlow(t *testing.T) {
	m := New(testData())
	m.width, m.height = 100, 30
	m.active = PageSessions
	// d arms the confirm; a non-y cancels (no store, so nothing deleted).
	m.Update(key("d"))
	if !m.sessions.confirmDel {
		t.Fatal("d should arm delete confirm")
	}
	m.Update(key("n"))
	if m.sessions.confirmDel {
		t.Fatal("n should cancel the confirm")
	}
}

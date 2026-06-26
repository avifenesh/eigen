package app

// APP-003: the projects drill-in promises p.feedN feed rows above the session
// rows (the count + enter math depend on it). These tests pin that the feed
// rows actually render and that the unified cursor resolves feed-vs-session
// correctly on enter.

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/config"
	"github.com/avifenesh/eigen/internal/feed"
	"github.com/avifenesh/eigen/internal/skill"
)

// drillData builds a project whose dir has one feed item (a loose end) plus two
// sessions — exercising both halves of the feedN + sessions list.
func drillData(t *testing.T) *Data {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // isolate dismissals from the real cache
	return &Data{
		Config: config.Config{},
		Skills: skill.Discover(),
		Projects: []ProjectRow{{
			Dir:  "/repo",
			Name: "repo",
			Sessions: []SessionRow{
				{ID: "s1", Title: "first session", Dir: "/repo", Msgs: 3, Updated: 2},
				{ID: "s2", Title: "second session", Dir: "/repo", Msgs: 1, Updated: 1},
			},
		}},
		Feed: feed.Feed{Items: []feed.Item{
			{Kind: "git", Title: "repo: 2 uncommitted file(s)", Detail: "commit them", Dir: "/repo", Task: "commit the changes"},
			{Kind: "github", Title: "other repo issue", Dir: "/elsewhere", Task: "ignored"},
		}},
		FeedFresh: true,
	}
}

func TestProjectsDrillInRendersFeedRows(t *testing.T) {
	d := drillData(t)
	m := NewAt(d, PageProjects)
	m.width, m.height = 120, 30
	m.projects.update(m, key("enter")) // drill into /repo
	if !m.projects.inside {
		t.Fatal("enter should drill into the project")
	}

	v := m.projects.view(m, 110, 24)
	// The feed row scoped to /repo must be visible above the sessions.
	if !strings.Contains(v, "uncommitted file(s)") {
		t.Fatalf("drill-in view must render the project's feed row:\n%s", v)
	}
	if !strings.Contains(v, "first session") || !strings.Contains(v, "second session") {
		t.Fatalf("drill-in view must still render the session rows:\n%s", v)
	}
	// Feed items from other dirs must not leak in.
	if strings.Contains(v, "other repo issue") {
		t.Fatalf("only this project's feed items should show:\n%s", v)
	}
	if m.projects.feedN != 1 {
		t.Fatalf("feedN should count the one /repo feed item, got %d", m.projects.feedN)
	}
	if m.projects.inner.count != 1+2 {
		t.Fatalf("inner count should be feedN + sessions = 3, got %d", m.projects.inner.count)
	}
}

func TestProjectsDrillInEnterSelectsFeedThenSession(t *testing.T) {
	d := drillData(t)
	m := NewAt(d, PageProjects)
	m.width, m.height = 120, 30
	m.projects.update(m, key("enter")) // drill in; cursor at 0 = the feed item

	// Cursor 0 is the feed row: enter opens a chat with its task pre-submitted.
	_, cmd := m.projects.update(m, key("enter"))
	if cmd == nil || m.result.Action != ActionOpenChat || m.result.Task != "commit the changes" || m.result.Dir != "/repo" {
		t.Fatalf("enter on the feed row should open chat with the task, result=%+v cmd=%v", m.result, cmd)
	}

	// Reset and move past the feed row onto the first session row.
	d = drillData(t)
	m = NewAt(d, PageProjects)
	m.width, m.height = 120, 30
	m.projects.update(m, key("enter")) // drill in
	m.projects.update(m, key("j"))     // cursor 0 (feed) -> 1 (session s1)
	_, cmd = m.projects.update(m, key("enter"))
	if cmd == nil || m.result.SessionID != "s1" {
		t.Fatalf("enter one past the feed row should resume the first session, result=%+v cmd=%v", m.result, cmd)
	}
}

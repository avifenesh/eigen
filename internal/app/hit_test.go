package app

// Tests for the Tier 10 Wave 2 app-shell mouse hit-testing + dispatch.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/daemon"
)

func TestHitTestRegions(t *testing.T) {
	m := layoutModel(t, 120, 30)
	l := m.computeLayout()
	if h := m.hitTest(l.title.x+1, 0); h.region != hitTitle {
		t.Fatalf("title row should hit hitTitle, got %v", h.region)
	}
	if h := m.hitTest(1, l.status.y); h.region != hitStatus {
		t.Fatalf("status row should hit hitStatus, got %v", h.region)
	}
	if h := m.hitTest(l.railInner.x, l.railInner.y); h.region != hitRail {
		t.Fatalf("rail inner should hit hitRail, got %v", h.region)
	}
	if h := m.hitTest(l.inner.x+1, l.inner.y+1); h.region != hitContent {
		t.Fatalf("content inner should hit hitContent, got %v", h.region)
	}
	// Far off-screen → none.
	if h := m.hitTest(1, m.height+5); h.region != hitNone {
		t.Fatalf("offscreen should be hitNone, got %v", h.region)
	}
}

func TestRailHitMapsToPages(t *testing.T) {
	m := layoutModel(t, 120, 30)
	l := m.computeLayout()
	// Each page row maps to its page, top-down in `pages` order.
	for i, p := range pages {
		y := l.railInner.y + i
		if y >= l.railInner.y+l.railInner.h {
			break // not enough rows in this terminal
		}
		h := m.hitTest(l.railInner.x, y)
		if h.region != hitRail || h.page != p.page {
			t.Fatalf("rail row %d should map to page %v, got region=%v page=%v", i, p.page, h.region, h.page)
		}
	}
}

func TestClickRailSwitchesPage(t *testing.T) {
	m := layoutModel(t, 120, 30)
	l := m.computeLayout()
	// Find the "config" page row.
	var configRow int
	for i, p := range pages {
		if p.page == PageConfig {
			configRow = i
		}
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.railInner.x, Y: l.railInner.y + configRow})
	if m.active != PageConfig {
		t.Fatalf("clicking the config rail row should switch to config, got %v", m.active)
	}
}

func TestClickLiveEntryAttaches(t *testing.T) {
	d := testData()
	d.Live = []daemon.SessionInfo{
		{ID: "s1", Dir: "/tmp/a", Status: daemon.StatusWorking},
		{ID: "s2", Dir: "/tmp/b", Status: daemon.StatusIdle},
	}
	m := New(d)
	m.width, m.height = 120, 30
	l := m.computeLayout()
	// Live entries start after the page rows + the "live" divider row.
	liveRow := l.railInner.y + len(pages) + 1
	_, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.railInner.x, Y: liveRow})
	if m.result.Action != ActionAttach || m.result.SessionID != "s1" {
		t.Fatalf("clicking the first live entry should attach s1, got %+v", m.result)
	}
	if cmd == nil {
		t.Fatal("attaching should quit (to hand off to main)")
	}
}

func TestClickRailBorderIsNoop(t *testing.T) {
	m := layoutModel(t, 120, 30)
	// x=0 is the rail's left border cell (outside railInner) — a no-op.
	before := m.active
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: 3})
	if m.active != before {
		t.Fatal("clicking the rail border should not switch pages")
	}
}

func TestWheelOverContentScrollsList(t *testing.T) {
	d := feedData()
	m := New(d)
	m.width, m.height = 120, 30
	m.active = PageSessions
	l := m.computeLayout()
	// Wheel down over the content moves the sessions list cursor.
	before := m.sessions.list.cursor
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown, X: l.inner.x + 1, Y: l.inner.y + 1})
	if m.sessions.list.cursor == before && len(d.Sessions) > 1 {
		t.Fatalf("wheel down over content should move the list cursor (was %d)", before)
	}
}

func TestMotionAndRightClickIgnored(t *testing.T) {
	m := layoutModel(t, 120, 30)
	l := m.computeLayout()
	before := m.active
	// Motion must not switch pages.
	m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: l.railInner.x, Y: l.railInner.y + 4})
	if m.active != before {
		t.Fatal("motion should be ignored")
	}
	// Right-click must not switch pages.
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: l.railInner.x, Y: l.railInner.y + 4})
	if m.active != before {
		t.Fatal("right-click should be ignored")
	}
}

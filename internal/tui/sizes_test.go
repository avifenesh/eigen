package tui

// Size-sweep regression tests: the View must fit ANY terminal size — extra
// lines scroll the screen and a too-wide line wraps, both visibly breaking the
// TUI. The sweep crosses widths (incl. the rail/panel breakpoints) with
// heights (incl. very short), and exercises the chrome-heavy state: header +
// rail + changes panel + status bar.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func checkViewFits(t *testing.T, m *model, w, h int) {
	t.Helper()
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) > h {
		t.Errorf("%dx%d: View renders %d lines (> height %d)", w, h, len(lines), h)
		return
	}
	for i, ln := range lines {
		if lw := ansi.StringWidth(ansi.Strip(ln)); lw > w {
			t.Errorf("%dx%d: line %d is %d cols wide (> width): %q", w, h, i, lw, ansi.Strip(ln))
			return
		}
	}
}

func TestViewFitsAllSizes(t *testing.T) {
	for _, w := range []int{20, 38, 40, 50, 60, 66, 72, 79, 80, 82, 88, 96, 100, 120, 140, 180, 211} {
		for _, h := range []int{6, 8, 10, 12, 16, 20, 24, 30, 40, 52} {
			m := switcherModel(t)
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			m.refreshRail()
			m.text("user", "use the edit tool to change beta")
			m.push(editBlock("note.txt", "beta", "beta two"))
			m.text("assistant", "Done — replaced beta with beta two in note.txt which is a long line that should wrap correctly")
			m.relayout()
			checkViewFits(t, m, w, h)
		}
	}
}

func TestViewFitsWhileRunningWithPlan(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 140} {
		for _, h := range []int{8, 12, 24, 40} {
			m := switcherModel(t)
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			m.refreshRail()
			m.todos = []todoItem{{Content: "first task", Status: "in_progress"}, {Content: "second", Status: "pending"}}
			m.state = stRunning
			m.status = "thinking…"
			m.text("user", "do the thing with a fairly long prompt that wraps on narrow terminals")
			m.relayout()
			checkViewFits(t, m, w, h)
		}
	}
}

func TestHeaderDropsBorderOnShortTerminals(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	if m.headerHeight() != 1 {
		t.Fatalf("short terminal should use the 1-row header, got %d", m.headerHeight())
	}
	if strings.Contains(m.headerView(), "╭") {
		t.Fatal("borderless header must not draw a frame")
	}
	// Clicking the single row still resolves actions.
	if a := m.headerActionAt(2, 0); a != actRename {
		t.Fatalf("title click on the borderless header should rename, got %v", a)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.headerHeight() != 3 {
		t.Fatalf("normal terminal keeps the bordered header, got %d", m.headerHeight())
	}
}

func TestHeaderButtonsDropWhenNarrow(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 24, Height: 24})
	btns := m.visibleHeaderButtons(22)
	if len(btns) >= len(m.headerButtons()) {
		t.Fatal("narrow header should drop buttons from the right")
	}
	v := m.headerView()
	for _, ln := range strings.Split(v, "\n") {
		if lw := ansi.StringWidth(ansi.Strip(ln)); lw > 24 {
			t.Fatalf("header line overflows narrow width: %d cols", lw)
		}
	}
	// Wide enough: all buttons visible.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if len(m.visibleHeaderButtons(98)) != len(m.headerButtons()) {
		t.Fatal("wide header shows all buttons")
	}
}

package app

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestAppKeyboardE2ENavigatePaletteAndOpen(t *testing.T) {
	m := New(feedData())
	m.width, m.height = 120, 30
	steps := []tea.KeyMsg{key("tab"), key("tab"), key("h"), key(":"), key("m"), key("o"), key("d"), key("enter")}
	for i, step := range steps {
		m.Update(step)
		v := m.View()
		lines := strings.Split(v, "\n")
		if len(lines) != m.height {
			t.Fatalf("step %d rendered %d rows, want %d", i, len(lines), m.height)
		}
		for row, ln := range lines {
			if w := lipgloss.Width(ln); w != m.width {
				t.Fatalf("step %d row %d width=%d want %d", i, row, w, m.width)
			}
		}
	}
	if m.active != PageModels {
		t.Fatalf("palette flow should land on models page, got %v", m.active)
	}
	if m.palette.open {
		t.Fatal("palette should close after enter")
	}
}

func TestAppKeyboardE2EOpenFeedTaskCancelsWork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := New(feedData())
	m.width, m.height = 120, 30
	m.home.syncFeed(m.data)
	_, cmd := m.Update(key("enter"))
	if cmd == nil || m.result.Action != ActionOpenChat {
		t.Fatalf("enter on feed should open chat, got result=%+v cmd=%v", m.result, cmd)
	}
	if m.result.Dir != "/home/u/proj-a" || !strings.Contains(m.result.Task, "git status") {
		t.Fatalf("feed open should carry dir+task, got %+v", m.result)
	}
	if err := m.ctx.Err(); err != context.Canceled {
		t.Fatalf("opening feed task should cancel app work, got %v", err)
	}
}

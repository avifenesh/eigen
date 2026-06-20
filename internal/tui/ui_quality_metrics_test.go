package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIObjectiveUIQualityMetrics(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 34})
	for _, tab := range []rightPanelTab{rightTabChanges, rightTabGit, rightTabTerminal, rightTabTasks, rightTabObserve, rightTabNotepad} {
		m.setRightTab(tab)
		if tab == rightTabTerminal {
			m.term.focused = false // quality check is about render chrome, not PTY lifecycle.
		}
		view := m.View()
		plain := ansi.Strip(view)
		lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
		if len(lines) > m.height {
			t.Fatalf("tab %s renders %d rows, terminal height %d", tab.label(), len(lines), m.height)
		}
		for i, line := range lines {
			if ansi.StringWidth(line) > m.width {
				t.Fatalf("tab %s line %d overflows: width=%d terminal=%d line=%q", tab.label(), i+1, ansi.StringWidth(line), m.width, line)
			}
		}
		for _, token := range []string{"eigen", tab.shortLabel()} {
			if !strings.Contains(plain, token) {
				t.Fatalf("tab %s missing UI token %q in view:\n%s", tab.label(), token, plain)
			}
		}
		if strings.Contains(plain, "TODO") || strings.Contains(plain, "lorem") || strings.Contains(plain, "undefined") {
			t.Fatalf("tab %s contains placeholder/debug copy:\n%s", tab.label(), plain)
		}
	}
}

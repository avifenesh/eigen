package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIEveryRightPanelTabKeyboardJourney(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	m.changesOn = true
	tabs := m.rightTabs()
	if len(tabs) < 5 {
		t.Fatalf("expected major right-panel tabs, got %v", tabs)
	}
	for _, tab := range tabs {
		t.Run(tab.label(), func(t *testing.T) {
			cmd := m.setRightTab(tab)
			if tab == rightTabTerminal && cmd == nil {
				t.Fatal("terminal tab should return a start command")
			}
			if m.rightTab != tab {
				t.Fatalf("setRightTab(%v) landed on %v", tab, m.rightTab)
			}
			plain := ansi.Strip(m.View())
			if !strings.Contains(strings.ToLower(plain), "eigen") {
				t.Fatalf("right panel %s missing shell identity:\n%s", tab.label(), plain)
			}
			if !strings.Contains(strings.ToLower(plain), strings.ToLower(m.tabLabel(tab, true))) {
				t.Fatalf("right panel %s missing selected tab label %q:\n%s", tab.label(), m.tabLabel(tab, true), plain)
			}
		})
	}
}

func TestTUIRightPanelCycleKeyboardJourney(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	m.changesOn = true
	seen := map[rightPanelTab]bool{}
	for i := 0; i < 3; i++ { // ctrl+r intentionally cycles the primary visible tabs
		seen[m.rightTab] = true
		m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	}
	for _, tab := range []rightPanelTab{rightTabChanges, rightTabGit, rightTabTerminal} {
		if !seen[tab] {
			t.Fatalf("ctrl+r cycle did not visit %s; seen=%v", tab.label(), seen)
		}
	}
}

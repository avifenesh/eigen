package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIKeyboardE2EPalettePanelAndNotepad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	m.setRightTab(rightTabNotepad)
	m.notepad.focused = true
	if m.rightTab != rightTabNotepad || !m.notepad.focused {
		t.Fatalf("notepad should start focused, tab=%v focused=%v", m.rightTab, m.notepad.focused)
	}
	for _, r := range "e2e note" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if !m.notepad.dirty {
		t.Fatal("typing in notepad should make it dirty")
	}

	m.setRightTab(rightTabChanges) // leaving notepad via tab switch
	if m.rightTab == rightTabNotepad || m.notepad.focused || m.notepad.dirty {
		t.Fatalf("leaving notepad should unfocus and save; tab=%v focused=%v dirty=%v", m.rightTab, m.notepad.focused, m.notepad.dirty)
	}
	out := ansi.Strip(m.View())
	for _, want := range []string{"eigen", "sessions", "right panel"} {
		if !strings.Contains(out, want) {
			t.Fatalf("E2E view missing %q:\n%s", want, out)
		}
	}
}

func TestTUIKeyboardE2EHomeAndBackgroundActions(t *testing.T) {
	d := switcherModel(t)
	d.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	d.state = stRunning
	d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}, Alt: true})
	if !d.openApp {
		t.Fatal("alt+z while running should return to app shell")
	}

	d2 := switcherModel(t)
	d2.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	d2.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	for _, r := range "home" {
		d2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := d2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !d2.openApp {
		t.Fatalf("palette home should quit to app shell, openApp=%v cmd=%v", d2.openApp, cmd)
	}
}

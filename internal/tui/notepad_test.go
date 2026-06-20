package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typeRunes feeds a string to the focused notepad as key events.
func (m *model) notepadType(s string) {
	for _, r := range s {
		if r == '\n' {
			m.notepadKey("enter", tea.KeyMsg{Type: tea.KeyEnter})
			continue
		}
		m.notepadKey(string(r), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestNotepadEditPersistReload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})

	// Switch to the notepad tab + focus it.
	m.setRightTab(rightTabNotepad)
	if m.rightTab != rightTabNotepad {
		t.Fatal("should be on the notepad tab")
	}
	m.notepad.focused = true

	// Type two lines.
	m.notepadType("hello\nworld")
	if got := strings.Join(m.notepad.lines, "|"); got != "hello|world" {
		t.Fatalf("buffer wrong: %q", got)
	}

	// ctrl+g releases focus AND flushes to disk.
	m.notepadKey("ctrl+g", tea.KeyMsg{Type: tea.KeyCtrlG})
	if m.notepad.focused {
		t.Fatal("ctrl+g should release focus")
	}
	id := m.notepadSessionID()
	data, err := os.ReadFile(notepadPath(id))
	if err != nil {
		t.Fatalf("notes should be persisted: %v", err)
	}
	if strings.TrimSpace(string(data)) != "hello\nworld" {
		t.Fatalf("persisted content wrong: %q", string(data))
	}

	// Simulate a reload (fresh model, same HOME + session id) → notes survive.
	m.notepad.loaded = false
	m.loadNotepad()
	if strings.Join(m.notepad.lines, "|") != "hello|world" {
		t.Fatalf("notes should reload from disk, got %q", strings.Join(m.notepad.lines, "|"))
	}
}

func TestNotepadTabSwitchFlushesDirtyNotes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.setRightTab(rightTabNotepad)
	m.notepad.focused = true
	m.notepadType("ship notes before switching tabs")
	if !m.notepad.dirty {
		t.Fatal("typing should mark the notepad dirty")
	}

	m.setRightTab(rightTabChanges)
	if m.notepad.focused {
		t.Fatal("switching away should release notepad focus")
	}
	if m.notepad.dirty {
		t.Fatal("switching away should flush dirty notes")
	}
	data, err := os.ReadFile(notepadPath(m.notepadSessionID()))
	if err != nil {
		t.Fatalf("tab switch should persist notes: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "ship notes before switching tabs" {
		t.Fatalf("persisted note = %q", got)
	}
}

func TestNotepadQuitFlushesDirtyNotes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.setRightTab(rightTabNotepad)
	m.notepad.focused = true
	m.notepadType("save before quit")
	if !m.notepad.dirty {
		t.Fatal("typing should mark the notepad dirty")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should quit")
	}
	if m.notepad.dirty {
		t.Fatal("quit should flush dirty notes")
	}
	data, err := os.ReadFile(notepadPath(m.notepadSessionID()))
	if err != nil {
		t.Fatalf("quit should persist notes: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "save before quit" {
		t.Fatalf("persisted note = %q", got)
	}
}

func TestNotepadBackspaceJoinsLines(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.setRightTab(rightTabNotepad)
	m.notepad.focused = true
	m.notepadType("ab\ncd")
	// cursor at end of "cd" (row 1, col 2); backspace twice removes c,d
	m.notepadKey("backspace", tea.KeyMsg{Type: tea.KeyBackspace})
	m.notepadKey("backspace", tea.KeyMsg{Type: tea.KeyBackspace})
	// now at start of row 1 (empty) → one more backspace joins with row 0
	m.notepadKey("backspace", tea.KeyMsg{Type: tea.KeyBackspace})
	if got := strings.Join(m.notepad.lines, "|"); got != "ab" {
		t.Fatalf("backspace join wrong: %q", got)
	}
}

func TestNotepadFocusGatesKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.setRightTab(rightTabNotepad)
	m.notepad.focused = false
	// Not focused → notepadKey must not consume.
	if _, handled := m.notepadKey("a", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}); handled {
		t.Fatal("notepad should not consume keys when unfocused")
	}
	// Empty + unfocused renders the hint.
	band := strings.Join(m.notepadLines(10), "\n")
	if !strings.Contains(band, "click") {
		t.Fatalf("unfocused empty notepad should show the hint:\n%s", band)
	}
}

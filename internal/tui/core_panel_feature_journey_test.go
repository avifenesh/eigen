package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIChangesPanelFeatureJourney(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.push(editBlock("src/main.go", "old line", "new line"))
	m.setRightTab(rightTabChanges)
	plain := ansi.Strip(strings.Join(m.changesLines(12), "\n"))
	for _, want := range []string{"chg", "main.go", "old line", "new line"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("changes journey missing %q:\n%s", want, plain)
		}
	}
	idx := m.changesRowAt(1)
	if idx < 0 {
		t.Fatalf("changes row should map to a file index")
	}
	m.jumpToChange(idx)
	if m.sel < 0 || m.sel >= len(m.blocks) || m.blocks[m.sel].toolName == "" {
		t.Fatalf("jumping to change should select the edit tool block, sel=%d", m.sel)
	}
}

func TestTUIGitPanelFeatureJourney(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.setRightTab(rightTabGit)
	dir := m.sessionDir()
	m.gitCache = gitSummary{Dir: dir, Repo: true, Branch: "main", Ahead: 1, Behind: 2, Staged: 1, Unstaged: 3, Untracked: 4}
	m.gitCacheDir = dir
	plain := ansi.Strip(strings.Join(m.gitLines(12), "\n"))
	for _, want := range []string{"git", "branch  main", "sync", "files", "staged 1"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("git journey missing %q:\n%s", want, plain)
		}
	}
}

func TestTUITerminalPanelFeatureJourney(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	cmd := m.setRightTab(rightTabTerminal)
	if cmd == nil || m.rightTab != rightTabTerminal || !m.term.focused {
		t.Fatalf("terminal tab should focus and return start command, tab=%v focused=%v cmd=%v", m.rightTab, m.term.focused, cmd)
	}
	plain := ansi.Strip(strings.Join(m.termLines(10), "\n"))
	for _, want := range []string{"trm"} {
		if !strings.Contains(strings.ToLower(plain), want) {
			t.Fatalf("terminal journey missing %q:\n%s", want, plain)
		}
	}
	m.setRightTab(rightTabChanges)
	if m.term.focused {
		t.Fatal("leaving terminal tab should release terminal focus")
	}
}

func TestTUINotepadPanelFeatureJourney(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.setRightTab(rightTabNotepad)
	m.notepad.focused = true
	m.notepadType("remember this")
	plain := ansi.Strip(strings.Join(m.notepadLines(10), "\n"))
	if !strings.Contains(plain, "remember this") {
		t.Fatalf("notepad journey should show typed note:\n%s", plain)
	}
	m.setRightTab(rightTabChanges)
	if m.notepad.dirty {
		t.Fatal("leaving notepad should persist dirty note")
	}
}

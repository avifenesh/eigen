package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func assertTUIGoldenContains(t *testing.T, goldenPath, rendered string) {
	t.Helper()
	b, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	plain := ansi.Strip(rendered)
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(plain, line) {
			t.Fatalf("rendered TUI missing golden token %q from %s:\n%s", line, goldenPath, plain)
		}
	}
}

func TestTUIEmptyComposerGoldenSnapshotTokens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	plain := ansi.Strip(m.View())
	for _, want := range []string{"eigen", "type", "perm=auto", "input=steer"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("empty composer missing golden token %q:\n%s", want, plain)
		}
	}
}

func TestTUIComposerMentionMenuGoldenSnapshotTokens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	m.fileIdx = []string{"src/main.go", "README.md"}
	m.fileIdxAt = time.Now()
	typeRunes(m, "inspect @ma")
	plain := ansi.Strip(m.View())
	for _, want := range []string{"inspect @ma", "src/main.go"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("mention composer missing golden token %q:\n%s", want, plain)
		}
	}
}

func TestTUIComposerTranscriptGoldenSnapshotTokens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	m.text("user", "inspect src/main.go")
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "Summary: main is important"}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventDone, Text: "Summary: main is important"}})
	m.Update(turnDoneMsg{})
	assertTUIGoldenContains(t, "testdata/golden/composer_transcript.txt", m.View())
}

func TestTUIEveryRightPanelTabGoldenSnapshotTokens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.changesOn = true
	for _, tab := range m.rightTabs() {
		t.Run(tab.label(), func(t *testing.T) {
			m.setRightTab(tab)
			if tab == rightTabGit {
				dir := m.sessionDir()
				m.gitCache = gitSummary{Dir: dir, Repo: true, Branch: "main"}
				m.gitCacheDir = dir
				m.gitCacheAt = time.Now()
			}
			plain := ansi.Strip(m.View())
			for _, want := range []string{"eigen", m.tabLabel(tab, true)} {
				if !strings.Contains(plain, want) {
					t.Fatalf("right-panel tab %s missing golden token %q:\n%s", tab.label(), want, plain)
				}
			}
		})
	}
}

func TestTUIRightPanelGitGoldenSnapshotTokens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.setRightTab(rightTabGit)
	dir := m.sessionDir()
	m.gitCache = gitSummary{Dir: dir, Repo: true, Branch: "main"}
	m.gitCacheDir = dir
	m.gitCacheAt = time.Now()
	assertTUIGoldenContains(t, "testdata/golden/right_panel_git.txt", m.View())
}

func TestTUIRightPanelPremiumSurfaceGoldenSnapshotTokens(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})

	m.setRightTab(rightTabTasks)
	assertTUIGoldenContains(t, "testdata/golden/tasks_panel_premium.txt", m.View())
	m.setRightTab(rightTabShells)
	assertTUIGoldenContains(t, "testdata/golden/shells_panel_premium.txt", m.View())

	m.setRightTab(rightTabNotepad)
	typeRunes(m, "phase notes")
	assertTUIGoldenContains(t, "testdata/golden/notepad_panel_premium.txt", m.View())
}

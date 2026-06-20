package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRenderSoakDoesNotSpawnWorkOrLeakGoroutines(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.text("user", "edit")
	m.push(editBlock("src/main.go", "old", "new"))
	m.setRightTab(rightTabGit)
	gitPanelCommandCount.Store(0)
	if m.rightTab != rightTabGit {
		t.Fatal("precondition: git tab selected")
	}

	beforeG := settledGoroutines(t)
	gitCommands := gitPanelCommandCount.Load()
	fileWalks := fileIndexWalkCount.Load()
	for i := 0; i < 250; i++ {
		_ = m.View()
		_ = m.transcriptBand()
		_ = m.gitLines(12)
		_ = m.compMenuView()
	}
	if got := gitPanelCommandCount.Load(); got != gitCommands {
		t.Fatalf("render soak spawned git commands: before=%d after=%d", gitCommands, got)
	}
	if got := fileIndexWalkCount.Load(); got != fileWalks {
		t.Fatalf("render soak walked file index: before=%d after=%d", fileWalks, got)
	}
	assertGoroutineBound(t, beforeG, 2, "render soak")
}

func TestTUILiveLoopResourceMeasurement(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 32})
	m.text("user", "please inspect the project")
	m.setRightTab(rightTabGit)
	gitPanelCommandCount.Store(0)
	beforeG := settledGoroutines(t)
	beforeGit := gitPanelCommandCount.Load()
	beforeWalks := fileIndexWalkCount.Load()
	start := time.Now()
	for i := 0; i < 300; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(fmt.Sprintf("x%d", i%10))})
		if i%5 == 0 {
			m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		}
		_ = m.View()
		_ = m.changesLines(20)
	}
	elapsed := time.Since(start)
	if !raceEnabled && elapsed > 2*time.Second {
		t.Fatalf("live-loop resource measurement too slow: %s", elapsed)
	}
	if got := gitPanelCommandCount.Load(); got != beforeGit {
		t.Fatalf("live loop spawned git commands from render/update: before=%d after=%d", beforeGit, got)
	}
	if got := fileIndexWalkCount.Load(); got != beforeWalks {
		t.Fatalf("live loop walked file index unexpectedly: before=%d after=%d", beforeWalks, got)
	}
	assertGoroutineBound(t, beforeG, 2, "live loop")
}

func TestMentionCompletionSoakReusesIndex(t *testing.T) {
	m := testModel(t)
	m.fileIdx = []string{"src/main.go", "src/metrics.go", "README.md"}
	m.fileIdxAt = time.Now()
	walks := fileIndexWalkCount.Load()
	for _, tok := range []string{"@m", "@ma", "@mai", "@main", "@metrics"} {
		m.ti.SetValue("inspect " + tok)
		m.refreshCompletion()
		if !m.comp.active() && !strings.Contains(tok, "metrics") {
			t.Fatalf("expected completion for %q", tok)
		}
	}
	if got := fileIndexWalkCount.Load(); got != walks {
		t.Fatalf("fresh mention cache should not walk during typing: before=%d after=%d", walks, got)
	}
}

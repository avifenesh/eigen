package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestTUIComposerTranscriptJourney(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	m.fileIdx = []string{"src/main.go", "README.md"}
	m.fileIdxAt = time.Now()

	typeRunes(m, "inspect @ma")
	if !m.comp.active() || m.comp.kind != compMention {
		t.Fatalf("@ma should open mention menu (active=%v kind=%v)", m.comp.active(), m.comp.kind)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.ti.Value(); got != "inspect src/main.go " {
		t.Fatalf("mention completion inserted %q", got)
	}
	typeRunes(m, "and summarize")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.state != stRunning {
		t.Fatalf("enter should submit the composer, state=%v", m.state)
	}
	foundUser := false
	for _, b := range m.blocks {
		if b.role == "user" && strings.Contains(b.body, "src/main.go") {
			foundUser = true
		}
	}
	if !foundUser {
		t.Fatalf("submitted transcript should contain the completed mention; blocks=%v", m.blocks)
	}
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventReasoningDelta, Text: "checking context"}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "Summary: "}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "main is important"}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventDone, Text: "Summary: main is important"}})
	m.Update(turnDoneMsg{})
	plain := ansi.Strip(m.View())
	for _, want := range []string{"inspect src/main.go", "Summary: main is important", "eigen"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("composer/transcript journey missing %q:\n%s", want, plain)
		}
	}
	if m.state != stInput || m.ti.Value() != "" {
		t.Fatalf("turn completion should return to empty composer, state=%v value=%q", m.state, m.ti.Value())
	}
}

func TestTUISlashCommandJourneyFromComposer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 28})
	typeRunes(m, "/he")
	if !m.comp.active() || m.comp.kind != compSlash {
		t.Fatal("/he should open slash completion")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "commands") && !strings.Contains(plain, "/help") {
		t.Fatalf("slash /help journey should render help text:\n%s", plain)
	}
	if m.comp.active() || m.state != stInput {
		t.Fatalf("slash command should finish in input mode, comp=%v state=%v", m.comp.active(), m.state)
	}
}

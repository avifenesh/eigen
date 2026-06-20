package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// TestPremiumInteractionViewSoak drives the TUI model through the premium chat
// chrome without executing asynchronous Bubble Tea commands. It is deliberately
// a deterministic state/render soak (not a goroutine-leak test): PTY and live
// resource tests cover process/runtime behavior, while this pins the composed
// UX surfaces that users interact with every turn.
func TestPremiumInteractionViewSoak(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 132, Height: 34})
	m.fileIdx = []string{"README.md", "internal/tui/tui.go", "docs/gui-parity-evidence.md"}
	m.fileIdxAt = time.Now()

	for _, r := range "inspect @gui" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		assertViewFits(t, m, "typing")
	}
	if !m.comp.active() || m.comp.kind != compMention {
		t.Fatalf("typing @gui should open mention completion, got active=%v kind=%d", m.comp.active(), m.comp.kind)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.comp.active() || !strings.Contains(m.ti.Value(), "docs/gui-parity-evidence.md") {
		t.Fatalf("tab should accept mention completion, input=%q active=%v", m.ti.Value(), m.comp.active())
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.state != stRunning {
		t.Fatalf("enter should submit and enter running state, got %v", m.state)
	}
	assertViewContains(t, m, "submitted turn", "inspect", "docs/gui-parity-evidence.md")

	m.Update(eventReasoning("checking panels"))
	m.Update(eventText("I will inspect the GUI state."))
	m.Update(eventDone("done"))
	m.Update(turnDoneMsg{})
	if m.state != stInput {
		t.Fatalf("turnDone should restore input, got %v", m.state)
	}
	assertViewContains(t, m, "completed turn", "I will inspect the GUI state")

	// Cycle through the right-panel surfaces that are always present in the
	// premium chat layout. Terminal activation is intentionally skipped here: it
	// starts a real PTY command and is covered by PTY smoke tests.
	for _, tab := range []rightPanelTab{rightTabGit, rightTabTasks, rightTabNotepad, rightTabChanges} {
		_ = m.setRightTab(tab)
		assertViewFits(t, m, fmt.Sprintf("tab %s", tab.label()))
		assertViewContains(t, m, fmt.Sprintf("tab %s", tab.label()), "[", "]")
		if m.rightTab != tab {
			t.Fatalf("setRightTab(%s) selected %s", tab.label(), m.rightTab.label())
		}
	}

	m.setRightTab(rightTabNotepad)
	for _, r := range "phase notes" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		assertViewFits(t, m, "notepad typing")
	}
	assertViewContains(t, m, "notepad", "phase notes")
	m.notepad.focused = false

	m.setRightTab(rightTabTasks)
	assertViewContains(t, m, "tasks tab", "nothing running")
	m.setRightTab(rightTabChanges)
	assertViewContains(t, m, "changes tab", "no edits yet")

	for i := 0; i < 80; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		if i%7 == 0 {
			m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		}
		assertViewFits(t, m, "composer soak")
	}
}

func eventReasoning(s string) agentEvent {
	return agentEvent{e: agent.Event{Kind: agent.EventReasoningDelta, Text: s}}
}

func eventText(s string) agentEvent {
	return agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: s}}
}

func eventDone(s string) agentEvent {
	return agentEvent{e: agent.Event{Kind: agent.EventDone, Text: s}}
}

func assertViewFits(t *testing.T, m *model, context string) {
	t.Helper()
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) > m.height {
		t.Fatalf("%s: view rendered %d rows > height %d\n%s", context, len(lines), m.height, view)
	}
	for i, ln := range lines {
		if w := ansi.StringWidth(ansi.Strip(ln)); w > m.width {
			t.Fatalf("%s: line %d width %d > %d: %q", context, i, w, m.width, ln)
		}
	}
}

func assertViewContains(t *testing.T, m *model, context string, wants ...string) {
	t.Helper()
	view := ansi.Strip(m.View())
	for _, want := range wants {
		if !strings.Contains(view, want) {
			t.Fatalf("%s: view missing %q\n%s", context, want, view)
		}
	}
}

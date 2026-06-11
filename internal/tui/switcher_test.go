package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/chat"
)

// switchBackend wraps the local test backend with a fake daemon session list,
// so the switcher paths run without a real daemon.
type switchBackend struct {
	chat.Backend
	id      string
	entries []chat.SessionEntry
}

func (s *switchBackend) Sessions() []chat.SessionEntry { return s.entries }
func (s *switchBackend) SessionID() string             { return s.id }

func switcherModel(t *testing.T) *model {
	m := testModel(t)
	m.backend = &switchBackend{
		Backend: m.backend,
		id:      "s2",
		entries: []chat.SessionEntry{
			{ID: "s1", Title: "first", Dir: "/tmp/a", Status: "idle"},
			{ID: "s2", Title: "current", Dir: "/tmp/b", Status: "working"},
			{ID: "s3", Title: "third", Dir: "/tmp/c", Status: "approval"},
		},
	}
	return m
}

func TestSwitcherOpensPreselectsCurrent(t *testing.T) {
	m := switcherModel(t)
	m.openSwitcher()
	if !m.switching {
		t.Fatal("switcher should be open")
	}
	if m.switchIdx != 1 {
		t.Fatalf("should preselect the current session (idx 1), got %d", m.switchIdx)
	}
	v := m.View()
	for _, want := range []string{"switch session", "first", "current", "third"} {
		if !strings.Contains(v, want) {
			t.Fatalf("switcher view missing %q:\n%s", want, v)
		}
	}
}

func TestSwitcherEnterHops(t *testing.T) {
	m := switcherModel(t)
	m.openSwitcher()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}) // down to s3
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.switchTo != "s3" {
		t.Fatalf("switchTo = %q, want s3", m.switchTo)
	}
	if cmd == nil {
		t.Fatal("enter on another session must quit (to hop)")
	}
}

func TestSwitcherEnterOnCurrentIsNoop(t *testing.T) {
	m := switcherModel(t)
	m.openSwitcher() // preselected on current (s2)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.switchTo != "" || m.switching {
		t.Fatalf("enter on the current session should just close: switchTo=%q switching=%v",
			m.switchTo, m.switching)
	}
}

func TestSwitcherHomeKey(t *testing.T) {
	m := switcherModel(t)
	m.openSwitcher()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if !m.openApp {
		t.Fatal("h should set openApp (go home to the app)")
	}
}

func TestSwitcherEscCancels(t *testing.T) {
	m := switcherModel(t)
	m.openSwitcher()
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.switching || m.switchTo != "" || m.openApp {
		t.Fatal("esc must cancel without an exit intent")
	}
}

func TestSwitcherLocalBackendNote(t *testing.T) {
	m := testModel(t) // plain local backend: no SessionLister
	m.openSwitcher()
	if m.switching {
		t.Fatal("local chats have no sibling sessions; switcher must not open")
	}
	v := m.View()
	if !strings.Contains(v, "daemon-hosted") {
		t.Fatalf("expected an explanatory note, got:\n%s", v)
	}
}

func TestSessionsSlashCommand(t *testing.T) {
	m := switcherModel(t)
	m.command("/sessions")
	if !m.switching {
		t.Fatal("/sessions should open the switcher")
	}
}

func TestStatusGlyphs(t *testing.T) {
	for status, want := range map[string]string{
		"working": "●", "idle": "○", "approval": "◆", "error": "✗",
	} {
		if g := statusGlyph(status); !strings.Contains(g, want) {
			t.Fatalf("statusGlyph(%q) = %q, want %q", status, g, want)
		}
	}
}

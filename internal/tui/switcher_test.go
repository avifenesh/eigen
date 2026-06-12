package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/llm"
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

// visionRouter routes ONLY when an image is attached (mimicking the real
// router's image-forces-vision gate even while disabled).
type visionRouter struct {
	on     bool
	called bool
	prov   llmProvStub
}

type llmProvStub struct{ id string }

func (p llmProvStub) Name() string    { return p.id }
func (p llmProvStub) ModelID() string { return p.id }
func (p llmProvStub) Complete(context.Context, llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "ok"}, nil
}

func (v *visionRouter) Enabled() bool       { return v.on }
func (v *visionRouter) SetEnabled(b bool)   { v.on = b }
func (v *visionRouter) Providers() []string { return nil }
func (v *visionRouter) Route(_ context.Context, _, _, _ string, hasImage bool) (llm.Provider, string, string) {
	v.called = true
	if !v.on && !hasImage {
		return nil, "", ""
	}
	return v.prov, "claude-fable-5", "routed → claude-fable-5 (vision/medium)"
}

func TestImageForcesVisionRouteWhenRouterOff(t *testing.T) {
	m := testModel(t)
	vr := &visionRouter{on: false, prov: llmProvStub{id: "claude-fable-5"}}
	m.router = vr
	m.modelID = "openai.gpt-5.5" // no vision in catalog

	// Write a real (tiny) png so the image reference resolves.
	dir := t.TempDir()
	png := dir + "/shot.png"
	os.WriteFile(png, []byte("\x89PNG\r\n\x1a\nfakedata"), 0o644)

	m.submit("describe " + png)
	if !vr.called {
		t.Fatal("router must be consulted when an image needs vision")
	}
	if m.modelID != "claude-fable-5" {
		t.Fatalf("model = %q, want vision model", m.modelID)
	}
}

func TestPlainPromptRespectsRouterOff(t *testing.T) {
	m := testModel(t)
	vr := &visionRouter{on: false, prov: llmProvStub{id: "claude-fable-5"}}
	m.router = vr
	m.modelID = "openai.gpt-5.5"
	m.submit("just text, no image")
	if m.modelID != "openai.gpt-5.5" {
		t.Fatal("plain prompt must not route while disabled")
	}
}

func TestRenameCommand(t *testing.T) {
	m := testModel(t)
	m.command("/rename my project work")
	if got := m.backend.Title(); got != "my project work" {
		t.Fatalf("title = %q", got)
	}
	// Bare /rename opens the interactive prompt (the single rename surface).
	m.command("/rename")
	if !m.ov.active || m.ov.kind != promptText {
		t.Fatal("bare /rename should open the rename prompt")
	}
	// Accepting an empty value clears the title (reverts to the derived preview).
	m.ov.value = ""
	m.overlayKey("enter")
	if got := m.backend.Title(); got != "" {
		t.Fatalf("clear: title = %q", got)
	}
}

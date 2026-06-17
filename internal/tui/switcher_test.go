package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/theme"
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

// Detach makes switchBackend a chat.Detacher, i.e. a daemon-backed session
// (the turn keeps running daemon-side after the view leaves). Tests of
// background/detach paths rely on this.
func (s *switchBackend) Detach() {}

func switcherModel(t *testing.T) *model {
	m := testModel(t)
	m.backend = &switchBackend{
		Backend: m.backend,
		id:      "s2",
		entries: []chat.SessionEntry{
			{ID: "s1", Title: "first", Dir: "/tmp/a", Status: "idle", Turns: 4},
			{ID: "s2", Title: "current", Dir: "/tmp/b", Status: "working", Turns: 2},
			{ID: "s3", Title: "third", Dir: "/tmp/c", Status: "approval", Turns: 8},
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
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // down to s3
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
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	if !m.openApp {
		t.Fatal("ctrl+h should set openApp (go home to the app)")
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
		"working": theme.StatusWorking, "idle": theme.StatusIdle,
		"approval": theme.StatusApproval, "error": theme.StatusError,
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
	// A model the catalog POSITIVELY marks blind (probed: xAI returns 400
	// "Image inputs are not supported" for composer). Routing fails closed,
	// so only a known-blind id forces the vision route — NOT unknown ids.
	m.modelID = "grok-composer-2.5-fast"

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

func TestPlainPromptRoutesWhenRouterOn(t *testing.T) {
	m := testModel(t)
	vr := &visionRouter{on: true, prov: llmProvStub{id: "claude-fable-5"}}
	m.router = vr
	m.modelID = "openai.gpt-5.5"
	m.submit("just text, no image")
	if !vr.called {
		t.Fatal("router should be consulted for plain top-level prompts when /route is on")
	}
	if m.modelID != "claude-fable-5" {
		t.Fatalf("model = %q, want routed model", m.modelID)
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

func TestUnknownModelDoesNotForceVisionRoute(t *testing.T) {
	// Fail CLOSED for routing: an UNCATALOGED id must stay on the user's
	// model even with an image attached — only a positive "blind" verdict
	// routes away.
	m := testModel(t)
	vr := &visionRouter{on: false, prov: llmProvStub{id: "claude-fable-5"}}
	m.router = vr
	m.modelID = "some-unknown-model"
	dir := t.TempDir()
	png := dir + "/shot.png"
	os.WriteFile(png, []byte("\x89PNG\r\n\x1a\nfakedata"), 0o644)
	m.submit("describe " + png)
	if m.modelID != "some-unknown-model" {
		t.Fatalf("unknown model must not be routed away, got %q", m.modelID)
	}
}

func TestKnownVisionModelAttachesImages(t *testing.T) {
	// gpt-5.5 is PROBED vision-capable (mantle accepted input_image) — an
	// image reference must attach, not route away and not drop.
	m := testModel(t)
	vr := &visionRouter{on: false, prov: llmProvStub{id: "claude-fable-5"}}
	m.router = vr
	m.modelID = "openai.gpt-5.5"
	dir := t.TempDir()
	png := dir + "/shot.png"
	os.WriteFile(png, []byte("\x89PNG\r\n\x1a\nfakedata"), 0o644)
	m.submit("describe " + png)
	if m.modelID != "openai.gpt-5.5" {
		t.Fatalf("vision-capable model must not be routed away, got %q", m.modelID)
	}
	if vr.called {
		t.Fatal("router must not be consulted when the model can see")
	}
}

func TestSwitcherTypeToSearch(t *testing.T) {
	m := switcherModel(t)
	m.openSwitcher()
	all := len(m.switchEntries)
	if all < 2 {
		t.Skip("need multiple sessions")
	}
	// Type a query that matches one session's title/id/dir; the filtered
	// list should narrow and enter should hop to a matching entry.
	target := m.switchEntries[all-1]
	for _, r := range target.ID {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	got := m.switchFiltered()
	if len(got) == 0 || len(got) > all {
		t.Fatalf("search should narrow: %d of %d", len(got), all)
	}
	// Backspace clears back to the full list.
	for range target.ID {
		m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if m.switchQuery != "" {
		t.Fatalf("backspace should clear the query, got %q", m.switchQuery)
	}
	if len(m.switchFiltered()) != all {
		t.Fatal("cleared query should restore the full list")
	}
}

func TestSwitcherHidesEmptySessions(t *testing.T) {
	m := testModel(t)
	m.backend = &switchBackend{
		Backend: m.backend,
		id:      "s2",
		entries: []chat.SessionEntry{
			{ID: "s1", Title: "real work", Dir: "/tmp/a", Status: "idle", Turns: 10},
			{ID: "s2", Title: "current", Dir: "/tmp/b", Status: "idle", Turns: 0}, // current, kept though empty
			{ID: "s9", Title: "", Dir: "/tmp/c", Status: "idle", Turns: 0},        // junk: drop
			{ID: "s12", Title: "", Dir: "/tmp/d", Status: "idle", Turns: 0},       // junk: drop
		},
	}
	m.openSwitcher()
	got := map[string]bool{}
	for _, e := range m.switchEntries {
		got[e.ID] = true
	}
	if !got["s1"] || !got["s2"] {
		t.Fatalf("real + current sessions must be listed, got %v", got)
	}
	if got["s9"] || got["s12"] {
		t.Fatalf("empty sessions must be filtered out, got %v", got)
	}
}

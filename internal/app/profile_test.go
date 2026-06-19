package app

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/observe"
)

func TestProfilePageRegisteredAndRenders(t *testing.T) {
	p, ok := PageByName("profile")
	if !ok || p != PageProfile || !isKnownPage(PageProfile) {
		t.Fatalf("profile page not registered: page=%v ok=%v", p, ok)
	}
	if p, ok := PageByName("me"); !ok || p != PageProfile {
		t.Fatalf("me alias = %v/%v, want profile", p, ok)
	}
	m := NewAt(testData(), PageProfile)
	m.width, m.height = 100, 30
	out := m.View()
	for _, want := range []string{"eigen", "profile", "usage and personalization prompt", "edit prompt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile shell missing %q:\n%s", want, out)
		}
	}
}

func TestProfileShowsUsageStats(t *testing.T) {
	d := testData()
	d.Observe = observe.Summary{
		Records: 12,
		Models: map[string]observe.ModelSummary{
			"gpt-5.5": {Turns: 2, InTokens: 100, OutTokens: 25, CacheReadTokens: 70, CacheWriteTokens: 10},
			"fast":    {Turns: 1, InTokens: 10, OutTokens: 5},
		},
		Errors: map[string]int{"denied": 1},
	}
	m := NewAt(d, PageProfile)
	out := m.profile.view(m, 100, 30)
	for _, want := range []string{"usage statistics", "3 sessions", "1 projects", "3 turns", "12 events", "tokens 110/30", "cache 70/10", "1 err", "top models", "gpt-5.5"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile usage missing %q:\n%s", want, out)
		}
	}
}

func TestProfileEmptyUsageAndNilMemoryNoCrash(t *testing.T) {
	d := testData()
	d.GlobalMem = nil
	m := NewAt(d, PageProfile)
	out := m.profile.view(m, 80, 24)
	for _, want := range []string{"no model usage recorded yet", "global memory unavailable"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile empty state missing %q:\n%s", want, out)
		}
	}
}

func TestProfilePromptSavesSingleGlobalUserProfile(t *testing.T) {
	m := memModel(t, "")
	m.active = PageProfile

	m.Update(key("e"))
	if !m.profile.editing {
		t.Fatal("e should open the profile prompt editor")
	}
	for _, r := range "I prefer concise summaries" {
		m.Update(key(string(r)))
	}
	m.Update(key("enter"))
	if m.profile.editing {
		t.Fatalf("enter should save and close editor, err=%q", m.profile.err)
	}
	if !strings.Contains(m.profile.status, "saved") {
		t.Fatalf("save status missing: %q", m.profile.status)
	}
	if got := m.data.GlobalMem.UserProfile(); !strings.Contains(got, "I prefer concise summaries") {
		t.Fatalf("profile prompt not persisted:\n%s", got)
	}
	if got := strings.Join(m.data.GlobalMem.AdHocNotes(0), "\n"); got != "" {
		t.Fatalf("profile prompt should be one USER.md, not ad-hoc notes:\n%s", got)
	}
}

func TestProfilePromptEscCancelsAndCapturesJumpKeys(t *testing.T) {
	m := memModel(t, "")
	m.active = PageProfile
	m.width, m.height = 100, 30

	m.Update(key("e"))
	m.pendingG = true
	m.Update(key("q"))
	if m.quitting || m.active != PageProfile {
		t.Fatalf("q while editing should type, not quit/jump; active=%v quitting=%v", m.active, m.quitting)
	}
	if m.pendingG {
		t.Fatal("input-owned q should clear stale g prefix")
	}
	if !strings.Contains(m.profile.input, "q") {
		t.Fatalf("q should be typed into input, got %q", m.profile.input)
	}
	m.Update(key("esc"))
	if m.profile.editing || m.profile.input != "" {
		t.Fatalf("esc should cancel and clear input, editing=%v input=%q", m.profile.editing, m.profile.input)
	}
	if got := m.data.GlobalMem.UserProfile(); got != "" {
		t.Fatalf("cancel should not persist profile prompt:\n%s", got)
	}
}

func TestProfileClickOpensPromptEditorAnywhere(t *testing.T) {
	m := memModel(t, "")
	m.active = PageProfile
	cmd, handled := m.profile.clickAt(m, 99)
	if cmd != nil || !handled || !m.profile.editing {
		t.Fatalf("profile click should open editor without row math, handled=%v cmd=%v editing=%v", handled, cmd, m.profile.editing)
	}
}

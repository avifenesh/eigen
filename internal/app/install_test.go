package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPromptCapturesInput(t *testing.T) {
	var p installPrompt
	p.open("plugin", "plugin name")
	if !p.active {
		t.Fatal("prompt should be active after open")
	}
	// Type "abc" via runes.
	p.key("a", []rune("a"))
	p.key("b", []rune("b"))
	p.key("c", []rune("c"))
	if p.input != "abc" {
		t.Fatalf("input = %q, want abc", p.input)
	}
	// Backspace.
	p.key("backspace", nil)
	if p.input != "ab" {
		t.Fatalf("after backspace = %q, want ab", p.input)
	}
	// Enter submits the trimmed source (the CALLER closes after running install).
	src, ok := p.key("enter", nil)
	if !ok || src != "ab" {
		t.Fatalf("submit = %q,%v want ab,true", src, ok)
	}
	p.close() // caller does this after the install runs
	if p.active {
		t.Fatal("prompt should close after the caller finishes")
	}
}

func TestInstallPromptEscCancels(t *testing.T) {
	var p installPrompt
	p.open("skill", "src")
	p.key("x", []rune("x"))
	if _, ok := p.key("esc", nil); ok {
		t.Fatal("esc must not submit")
	}
	if p.active || p.input != "" {
		t.Fatal("esc should cancel + clear")
	}
}

func TestInstallPromptEmptyEnterCancels(t *testing.T) {
	var p installPrompt
	p.open("plugin", "name")
	if src, ok := p.key("enter", nil); ok || src != "" {
		t.Fatalf("empty enter should not submit, got %q,%v", src, ok)
	}
	if p.active {
		t.Fatal("empty enter should close the prompt")
	}
}

func TestParseSkillInstallInputFlags(t *testing.T) {
	got, err := parseSkillInstallInput("owner/repo/path --force --overwrite --name custom --no-scan")
	if err != nil {
		t.Fatal(err)
	}
	if got.source != "owner/repo/path" || got.name != "custom" || !got.force || !got.overwrite || !got.noScan {
		t.Fatalf("parseSkillInstallInput = %+v", got)
	}
}

func TestSkillsInstallRunsInBackgroundWithBusyMarker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	src := filepath.Join(t.TempDir(), "mine")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: mine\ndescription: test\n---\nUse normal tools.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewAt(testData(), PageSkills)
	m.width, m.height = 100, 30
	m.Update(key("i"))
	for _, r := range src {
		m.Update(key(string(r)))
	}
	_, cmd := m.Update(key("enter"))
	if cmd == nil || !m.skills.prompt.busy {
		t.Fatal("skill install should start a background command and set busy")
	}
	if v := m.skills.view(m, 80, 20); !strings.Contains(v, "installing skill") {
		t.Fatalf("busy marker missing while install runs:\n%s", v)
	}
	m.Update(cmd())
	if m.skills.prompt.busy || m.skills.prompt.active {
		t.Fatal("install completion should clear busy prompt")
	}
	if !strings.Contains(m.skills.prompt.status, "installed skill") {
		t.Fatalf("install completion should report success, got %q", m.skills.prompt.status)
	}
}

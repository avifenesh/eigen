package app

import "testing"

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

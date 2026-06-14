package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownTableRenders(t *testing.T) {
	md := "| Model | Ctx | Vision |\n|---|---|---|\n| opus-4-8 | 200k | yes |\n| gpt-5.5 | 400k | yes |"
	out := renderProse(md, 80)
	plain := ansi.Strip(out)
	// Header + both rows present.
	for _, want := range []string{"Model", "Ctx", "Vision", "opus-4-8", "gpt-5.5", "400k"} {
		if !strings.Contains(plain, want) {
			t.Errorf("table missing %q:\n%s", want, plain)
		}
	}
	// Rendered as a real bordered table (box-drawing), not raw pipes.
	if !strings.Contains(plain, "╭") || !strings.Contains(plain, "┼") || !strings.Contains(plain, "╰") {
		t.Errorf("table should have box-drawing borders:\n%s", plain)
	}
	if strings.Contains(plain, "|---|") {
		t.Errorf("raw separator row should be consumed:\n%s", plain)
	}
}

func TestIsTableSep(t *testing.T) {
	for _, s := range []string{"|---|---|", "---|---", "| :--: | --: |", "|-|-|-|"} {
		if !isTableSep(s) {
			t.Errorf("should be a table separator: %q", s)
		}
	}
	for _, s := range []string{"", "hello", "| a | b |", "- item"} {
		if isTableSep(s) {
			t.Errorf("should NOT be a table separator: %q", s)
		}
	}
}

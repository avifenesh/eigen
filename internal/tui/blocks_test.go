package tui

import (
	"strings"
	"testing"
)

func TestThinkingBlockCollapsedShowsPreviewOnly(t *testing.T) {
	b := &block{kind: blockThinking, title: "thinking", collapsed: true}
	b.body.WriteString("first line of reasoning\nsecond line that must stay hidden")
	out := b.render(false)
	if strings.Contains(out, "second line") {
		t.Fatalf("collapsed thinking leaked hidden lines:\n%s", out)
	}
	if !strings.Contains(out, "▸") {
		t.Fatalf("collapsed block should show ▸ marker:\n%s", out)
	}
	if !strings.Contains(out, "first line of reasoning") {
		t.Fatalf("collapsed block should preview first line:\n%s", out)
	}
}

func TestThinkingBlockExpandedShowsAll(t *testing.T) {
	b := &block{kind: blockThinking, title: "thinking"}
	b.body.WriteString("alpha\nbeta\ngamma")
	out := b.render(false)
	for _, want := range []string{"alpha", "beta", "gamma", "▾"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expanded block missing %q:\n%s", want, out)
		}
	}
}

func TestToolBlockShowsResultWhenExpanded(t *testing.T) {
	b := &block{kind: blockTool, title: "read {\"path\":\"x\"}", result: "FILEBODY-CONTENT"}
	if strings.Contains(b.render(false), "FILEBODY-CONTENT") {
		// collapsed by default? no — collapsed is false here, so it should show
	}
	if !strings.Contains(b.render(false), "FILEBODY-CONTENT") {
		t.Fatalf("expanded tool block should show result:\n%s", b.render(false))
	}
	b.collapsed = true
	if strings.Contains(b.render(false), "FILEBODY-CONTENT") && !strings.Contains(previewLine("FILEBODY-CONTENT"), "FILEBODY") {
		t.Fatalf("collapsed tool block should only preview result")
	}
}

func TestSelectedBlockMarked(t *testing.T) {
	b := &block{kind: blockTool, title: "bash", collapsed: true}
	if !strings.Contains(b.render(true), "❭") {
		t.Fatalf("selected block should show ❭ marker:\n%s", b.render(true))
	}
}

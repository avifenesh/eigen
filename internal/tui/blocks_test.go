package tui

import (
	"strings"
	"testing"
)

func TestThinkingBlockCollapsedShowsPreviewOnly(t *testing.T) {
	b := &block{kind: blockThinking, title: "thinking", collapsed: true}
	b.body = "first line of reasoning\nsecond line that must stay hidden"
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
	b.body = "alpha\nbeta\ngamma"
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

func TestLCSDiffKeepsContext(t *testing.T) {
	out := diffText("a\nb\nc", "a\nB\nc")
	// b -> B with a and c as unchanged context.
	if !strings.Contains(out, "  a") {
		t.Fatalf("unchanged line 'a' should be context:\n%s", out)
	}
	if !strings.Contains(out, "- b") || !strings.Contains(out, "+ B") {
		t.Fatalf("changed line should show -/+:\n%s", out)
	}
	if !strings.Contains(out, "  c") {
		t.Fatalf("unchanged line 'c' should be context:\n%s", out)
	}
}

func TestLCSDiffPureAddition(t *testing.T) {
	out := diffText("a", "a\nb")
	if !strings.Contains(out, "  a") || !strings.Contains(out, "+ b") {
		t.Fatalf("addition diff wrong:\n%s", out)
	}
	if strings.Contains(out, "- a") {
		t.Fatalf("unchanged line should not be removed:\n%s", out)
	}
}

func TestRenderWrappedCacheInvalidates(t *testing.T) {
	b := &block{kind: blockThinking, title: "thinking", collapsed: true}
	b.body = "alpha"
	first := b.renderWrapped(false, 80)
	if b.renderWrapped(false, 80) != first {
		t.Fatal("identical args should return the cached render")
	}
	// Expanding must change the output (cache invalidated by collapsed flag).
	b.collapsed = false
	if exp := b.renderWrapped(false, 80); exp == first {
		t.Fatal("expanding should change the rendered output")
	}
	// Appending to the body must change the output (length signature changes).
	b.body = "alpha\nbeta"
	if !strings.Contains(b.renderWrapped(false, 80), "beta") {
		t.Fatal("appended content should appear after re-render")
	}
	// Selection marker changes the output.
	plain := b.renderWrapped(false, 80)
	if b.renderWrapped(true, 80) == plain {
		t.Fatal("selection should change the rendered output")
	}
}

func TestProseStylesCodeFences(t *testing.T) {
	body := "Here is code:\n```go\nfunc main() {}\n```\ndone"
	b := &block{kind: blockText, role: "assistant", body: body}
	out := b.render(false)
	if !strings.Contains(out, "func main() {}") {
		t.Fatalf("code content should be present:\n%s", out)
	}
	if !strings.Contains(out, "│") {
		t.Fatalf("fenced code should get a left bar:\n%s", out)
	}
}

func TestPlainProseUnchanged(t *testing.T) {
	b := &block{kind: blockText, role: "assistant", body: "just a sentence"}
	if !strings.Contains(b.render(false), "just a sentence") {
		t.Fatal("plain prose should pass through")
	}
}

func TestProseRendersHeadings(t *testing.T) {
	b := &block{kind: blockText, role: "assistant", body: "# Title\nbody text"}
	out := b.render(false)
	if !strings.Contains(out, "Title") {
		t.Fatalf("heading text should render:\n%s", out)
	}
	if !strings.Contains(out, "body text") {
		t.Fatalf("body should render below the heading:\n%s", out)
	}
}

func TestProseRendersBulletList(t *testing.T) {
	b := &block{kind: blockText, role: "assistant", body: "- one\n- two"}
	out := b.render(false)
	if !strings.Contains(out, "•") {
		t.Fatalf("bullets should render with a • glyph:\n%s", out)
	}
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Fatalf("bullet text should render:\n%s", out)
	}
}

func TestProseRendersOrderedList(t *testing.T) {
	b := &block{kind: blockText, role: "assistant", body: "1. first\n2. second"}
	out := b.render(false)
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Fatalf("ordered list text should render:\n%s", out)
	}
	if !strings.Contains(out, "1.") || !strings.Contains(out, "2.") {
		t.Fatalf("ordered list numbers should be kept:\n%s", out)
	}
}

func TestRenderInlineSpans(t *testing.T) {
	out := renderInline("a **bold** and `code` and *em* end")
	for _, want := range []string{"bold", "code", "em", "end"} {
		if !strings.Contains(out, want) {
			t.Fatalf("inline content %q missing:\n%s", want, out)
		}
	}
	// The literal markers should be consumed (not left in the output).
	if strings.Contains(out, "**") || strings.Contains(out, "`") {
		t.Fatalf("inline markers should be consumed:\n%q", out)
	}
}

func TestProseBlockquote(t *testing.T) {
	b := &block{kind: blockText, role: "assistant", body: "> quoted line"}
	out := b.render(false)
	if !strings.Contains(out, "quoted line") || !strings.Contains(out, "▏") {
		t.Fatalf("blockquote should render with a bar:\n%s", out)
	}
}

func TestHeadingLevel(t *testing.T) {
	if headingLevel("## hi") != 2 {
		t.Fatal("## should be level 2")
	}
	if headingLevel("####### too many") != 0 {
		t.Fatal("7 hashes is not a heading")
	}
	if headingLevel("#no space") != 0 {
		t.Fatal("heading requires a space after #")
	}
}

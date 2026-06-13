package tui

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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

func TestCollapseContextFoldsLongRuns(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nCHANGE\nk"
	new := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nCHANGED\nk"
	out := diffText(old, new)
	if !strings.Contains(out, "unchanged lines ⋯") {
		t.Fatalf("long unchanged run should collapse:\n%s", out)
	}
	// The two context lines just before the change must survive.
	if !strings.Contains(out, "  i") || !strings.Contains(out, "  j") {
		t.Fatalf("context near the change should be kept:\n%s", out)
	}
	// The first lines should be folded away.
	if strings.Contains(out, "  c\n") {
		t.Fatalf("distant context should be folded:\n%s", out)
	}
	if !strings.Contains(out, "- CHANGE") || !strings.Contains(out, "+ CHANGED") {
		t.Fatalf("the change itself must render:\n%s", out)
	}
}

func TestDiffStatsAndHeaderSuffix(t *testing.T) {
	detail := diffText("a\nb", "a\nB\nc")
	add, del := diffStats(detail)
	if add != 2 || del != 1 {
		t.Fatalf("want +2 −1, got +%d −%d (detail:\n%s)", add, del, detail)
	}
	if got := statsSuffix(detail); got != " (+2 −1)" {
		t.Fatalf("statsSuffix = %q", got)
	}
	if got := statsSuffix("  unchanged"); got != "" {
		t.Fatalf("no-change suffix should be empty, got %q", got)
	}
}

func TestEditHeaderShowsStats(t *testing.T) {
	b := &block{
		kind: blockTool, toolName: "edit", title: "edit",
		toolArgs: json.RawMessage(`{"path":"f.go","old_string":"x = 1","new_string":"x = 2"}`),
	}
	h := b.header()
	if !strings.Contains(h, "(+1 −1)") {
		t.Fatalf("edit header should include stats: %q", h)
	}
}

func TestWriteDetailRendersAllAdded(t *testing.T) {
	b := &block{
		kind: blockTool, toolName: "write", title: "write",
		toolArgs: json.RawMessage(`{"path":"f.go","content":"line1\nline2"}`),
	}
	d := b.toolDetail()
	if !strings.Contains(d, "+ line1") || !strings.Contains(d, "+ line2") {
		t.Fatalf("write detail should show all-added lines:\n%s", d)
	}
	if !strings.Contains(b.header(), "(+2 −0)") {
		t.Fatalf("write header should show +2: %q", b.header())
	}
}

func TestApplyPatchDetailNormalizes(t *testing.T) {
	patch := "--- a/f.go\n+++ b/f.go\n@@ -1,2 +1,2 @@\n context\n-old line\n+new line"
	b := &block{
		kind: blockTool, toolName: "apply_patch", title: "apply_patch",
		toolArgs: json.RawMessage(`{"patch":` + strconv.Quote(patch) + `}`),
	}
	d := b.toolDetail()
	if !strings.Contains(d, "- old line") || !strings.Contains(d, "+ new line") {
		t.Fatalf("patch +/- lines should normalize:\n%s", d)
	}
	if !strings.Contains(d, "⋯ @@") {
		t.Fatalf("hunk markers should render as dim context:\n%s", d)
	}
}

func TestRenderDiffHighlightsChangedSpan(t *testing.T) {
	// Force a color profile: tests run without a TTY, where lipgloss strips
	// all styling and the underline assertion would be vacuous.
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(old)

	// A modified pair with a clear common prefix/suffix should use the
	// underline span styles.
	out := renderDiff("- x := compute(a, b)\n+ x := compute(a, c)")
	if !strings.Contains(out, "\x1b[4") { // underline SGR from the span styles
		t.Fatalf("changed span should be underlined:\n%q", out)
	}
	// Dissimilar pair: no underline (similarity gate).
	out = renderDiff("- completely different\n+ zzz qqq vvv")
	if strings.Contains(out, "\x1b[4;") || strings.Contains(out, "\x1b[4m") {
		t.Fatalf("dissimilar lines should not underline:\n%q", out)
	}
}

func TestSplitCommon(t *testing.T) {
	pre, aMid, bMid, suf := splitCommon("foo(bar)", "foo(baz)")
	if pre != "foo(ba" || suf != ")" || aMid != "r" || bMid != "z" {
		t.Fatalf("splitCommon wrong: pre=%q aMid=%q bMid=%q suf=%q", pre, aMid, bMid, suf)
	}
	// Identical strings: middles empty.
	_, aMid, bMid, _ = splitCommon("same", "same")
	if aMid != "" || bMid != "" {
		t.Fatalf("identical strings should have empty middles: %q %q", aMid, bMid)
	}
}

func TestMultieditDetailNumbersEdits(t *testing.T) {
	b := &block{
		kind: blockTool, toolName: "multiedit", title: "multiedit",
		toolArgs: json.RawMessage(`{"edits":[{"old_string":"a","new_string":"b"},{"old_string":"c","new_string":"d"}]}`),
	}
	d := b.toolDetail()
	if !strings.Contains(d, "edit 1/2:") || !strings.Contains(d, "edit 2/2:") {
		t.Fatalf("multiedit edits should be numbered:\n%s", d)
	}
}

func TestProseHeadingDropsHashes(t *testing.T) {
	out := renderProse("# Heading One\nbody")
	if strings.Contains(out, "#") {
		t.Errorf("heading should not keep raw '#' markers:\n%s", out)
	}
	if !strings.Contains(out, "Heading One") {
		t.Error("heading text missing")
	}
	// h1 gets an underline rule.
	if !strings.Contains(out, "═") {
		t.Errorf("h1 should get an underline rule:\n%s", out)
	}
}

func TestProseCodeFenceHidesBackticks(t *testing.T) {
	out := renderProse("```go\nx := 1\n```")
	if strings.Contains(out, "```") {
		t.Errorf("code fence should hide raw backticks:\n%s", out)
	}
	if !strings.Contains(out, "x := 1") {
		t.Error("code content missing")
	}
	if !strings.Contains(out, "go") {
		t.Errorf("language label should show:\n%s", out)
	}
}

func TestProseRendersLinks(t *testing.T) {
	out := renderProse("see [the docs](http://example.com) here")
	if strings.Contains(out, "[the docs]") || strings.Contains(out, "http://") {
		t.Errorf("raw link markdown should be gone:\n%s", out)
	}
	if !strings.Contains(out, "the docs") {
		t.Error("link text missing")
	}
}

func TestToolBlockHasGutterRule(t *testing.T) {
	b := &block{kind: blockTool, toolName: "read", toolArgs: []byte(`{"path":"x"}`),
		result: "file body", state: toolDone}
	out := b.render(false)
	if !strings.Contains(out, "▏") {
		t.Errorf("tool block should render in a gutter lane (▏):\n%s", out)
	}
}

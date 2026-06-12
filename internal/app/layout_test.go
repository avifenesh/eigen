package app

// Tests for the Tier 10 app-shell layout + framing geometry.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// layoutModel builds a minimal app Model with loaded-enough data for layout.
func layoutModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := New(testData())
	m.width, m.height = w, h
	return m
}

func TestLayoutRectsStack(t *testing.T) {
	for _, w := range []int{60, 80, 120, 160, 220} {
		m := layoutModel(t, w, 30)
		l := m.computeLayout()
		// Title at top, status at bottom.
		if l.title.y != 0 || l.title.h != 1 {
			t.Fatalf("w=%d title=%+v", w, l.title)
		}
		if l.status.y != m.height-1 {
			t.Fatalf("w=%d status should be last row, got %+v", w, l.status)
		}
		// Rail then content fill the body horizontally (with a 1-col gutter at
		// non-narrow breakpoints).
		if l.rail.x != 0 {
			t.Fatalf("w=%d rail should start at x=0, got %+v", w, l.rail)
		}
		gut := 1
		if l.bp == bpNarrow {
			gut = 0
		}
		if l.content.x != l.rail.x+l.rail.w+gut {
			t.Fatalf("w=%d content should follow rail+gutter: rail=%+v content=%+v", w, l.rail, l.content)
		}
		// No negative dims.
		for _, r := range []rect{l.rail, l.railInner, l.content, l.inner, l.title, l.status} {
			if r.w < 0 || r.h < 0 {
				t.Fatalf("w=%d negative rect %+v", w, r)
			}
		}
		// Inner content rect is smaller than the outer (border eats cells) at
		// non-narrow breakpoints.
		if l.bp != bpNarrow && l.inner.w >= l.content.w {
			t.Fatalf("w=%d inner (%d) should be < outer (%d) due to border", w, l.inner.w, l.content.w)
		}
	}
}

func TestLayoutBreakpoints(t *testing.T) {
	if bp := breakpointFor(60); bp != bpNarrow {
		t.Fatalf("60 cols should be narrow, got %v", bp)
	}
	if bp := breakpointFor(100); bp != bpNormal {
		t.Fatalf("100 cols should be normal, got %v", bp)
	}
	if bp := breakpointFor(160); bp != bpWide {
		t.Fatalf("160 cols should be wide, got %v", bp)
	}
}

func TestLayoutInspectorOnlyWhenWide(t *testing.T) {
	if l := (layoutModel(t, 100, 30)).computeLayout(); !l.inspector.empty() {
		t.Fatal("normal breakpoint should have no inspector")
	}
	l := (layoutModel(t, 160, 30)).computeLayout()
	if l.inspector.empty() {
		t.Fatal("wide breakpoint should have an inspector")
	}
	if l.inspector.x+l.inspector.w != 160 {
		t.Fatalf("inspector should be flush right, got %+v", l.inspector)
	}
	// Content must not overlap the inspector.
	if l.content.x+l.content.w > l.inspector.x {
		t.Fatalf("content overlaps inspector: content=%+v inspector=%+v", l.content, l.inspector)
	}
}

func TestViewLineWidthsWithinTerminal(t *testing.T) {
	// Rendered lines must never exceed the terminal width, or the terminal
	// wraps and breaks hit-testing.
	for _, w := range []int{80, 120, 160} {
		m := layoutModel(t, w, 30)
		v := m.View()
		for i, ln := range strings.Split(v, "\n") {
			if lw := lipgloss.Width(ln); lw > w {
				t.Fatalf("w=%d line %d width %d exceeds terminal:\n%q", w, i, lw, ln)
			}
		}
	}
}

func TestViewHasFramedChrome(t *testing.T) {
	m := layoutModel(t, 120, 30)
	v := m.View()
	// A rounded border draws these corner/edge runes — proof the framing rendered.
	if !strings.ContainsAny(v, "╭╮╰╯│─") {
		t.Fatalf("expected bordered chrome in the view:\n%s", v)
	}
	// Title bar shows the product mark + active page.
	if !strings.Contains(v, "eigen") {
		t.Fatal("title bar should show the product mark")
	}
}

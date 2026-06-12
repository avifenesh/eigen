package tui

// Tests for the Tier 9 chrome foundation: layout rects, region hit-testing, the
// action registry, the confirm/text overlay, and clickable status segments.

import (
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/chat"
	tea "github.com/charmbracelet/bubbletea"
)

func TestLayoutRectsStackVertically(t *testing.T) {
	m := testModel(t) // 80x24
	l := m.computeLayout()
	// transcript sits below the (empty) plan, input below transcript, status last.
	if l.transcript.y != l.plan.y+l.plan.h {
		t.Fatalf("transcript should follow plan: plan=%+v transcript=%+v", l.plan, l.transcript)
	}
	if l.input.y < l.transcript.y+l.transcript.h {
		t.Fatalf("input should be below the transcript: %+v vs %+v", l.input, l.transcript)
	}
	if l.status.y < l.input.y+l.input.h {
		t.Fatalf("status should be below the input: %+v vs %+v", l.status, l.input)
	}
	// Status rect is the last thing on screen; its bottom is within height.
	if l.status.y+l.status.h > m.height {
		t.Fatalf("status bottom %d exceeds height %d", l.status.y+l.status.h, m.height)
	}
}

func TestLayoutMatchesViewportSizing(t *testing.T) {
	m := testModel(t)
	// The transcript rect height must equal the viewport relayout sized.
	l := m.computeLayout()
	if l.transcript.h != m.vp.Height {
		t.Fatalf("transcript rect h=%d != vp.Height=%d", l.transcript.h, m.vp.Height)
	}
	// The status rect height equals statusBarHeight.
	if l.status.h != m.statusBarHeight() {
		t.Fatalf("status rect h=%d != statusBarHeight=%d", l.status.h, m.statusBarHeight())
	}
}

func TestHitTestRegions(t *testing.T) {
	m := testModel(t)
	l := m.computeLayout()
	// A point in the status rect resolves to regStatus.
	if h := m.hitTest(0, l.status.y); h.region != regStatus {
		t.Fatalf("status row should hit regStatus, got %v", h.region)
	}
	// A point in the transcript resolves to regTranscript.
	if h := m.hitTest(1, l.transcript.y); h.region != regTranscript {
		t.Fatalf("transcript row should hit regTranscript, got %v", h.region)
	}
	// A point in the input box resolves to regInput.
	if h := m.hitTest(1, l.input.y+1); h.region != regInput {
		t.Fatalf("input row should hit regInput, got %v", h.region)
	}
	// Off-screen resolves to regNone.
	if h := m.hitTest(0, m.height+5); h.region != regNone {
		t.Fatalf("offscreen should hit regNone, got %v", h.region)
	}
}

func TestStatusActionMapsToSegment(t *testing.T) {
	m := testModel(t)
	m.width = 200 // single line so every segment is on row 0
	boxes := m.statusBarLayout()
	// Find the perm segment and click its midpoint.
	var permBox *statusSegBox
	for i := range boxes {
		if strings.HasPrefix(boxes[i].seg.text, "perm=") {
			permBox = &boxes[i]
		}
	}
	if permBox == nil {
		t.Fatal("expected a perm segment")
	}
	l := m.computeLayout()
	x := (permBox.startCol + permBox.endCol) / 2
	if act := m.statusActionAt(x, l.status.y+permBox.row); act != actPermPicker {
		t.Fatalf("clicking perm segment should map to actPermPicker, got %v", act)
	}
	// The non-clickable "eigen" brand maps to actNone.
	if act := m.statusActionAt(2, l.status.y); act != actNone {
		t.Fatalf("brand segment should map to actNone, got %v", act)
	}
}

func TestDispatchGatesDisabledActions(t *testing.T) {
	m := testModel(t)
	// compact is idle-only: disabled while running.
	m.state = stRunning
	before := len(m.blocks)
	cmd := m.dispatch(actCompactPrompt)
	if cmd != nil {
		t.Fatal("disabled action should not run")
	}
	if len(m.blocks) <= before {
		t.Fatal("a disabled action should note why it's unavailable")
	}
	if m.ov.active {
		t.Fatal("disabled compact must not open the confirm overlay")
	}
}

func TestDispatchCompactOpensConfirm(t *testing.T) {
	m := testModel(t)
	m.state = stInput
	m.dispatch(actCompactPrompt)
	if !m.ov.active || m.ov.kind != promptConfirm {
		t.Fatal("compact should open a confirm overlay when idle")
	}
	// 'n' cancels without compacting.
	if cmd, handled := m.overlayKey("n"); !handled || cmd != nil {
		t.Fatal("n should cancel the confirm")
	}
	if m.ov.active {
		t.Fatal("confirm should close on n")
	}
}

func TestPermClickOpensConfirmNotBlindToggle(t *testing.T) {
	m := testModel(t)
	m.backend.(*chat.Local).Agent().Perm = agent.PermGated
	m.dispatch(actPermPicker)
	if !m.ov.active || m.ov.kind != promptConfirm {
		t.Fatal("perm click should open a confirm (security-sensitive), not toggle")
	}
	// Perm unchanged until confirmed.
	if m.backend.Perm() != agent.PermGated {
		t.Fatal("perm must not change before the user confirms")
	}
	// 'y' applies the toggle.
	m.overlayKey("y")
	if m.backend.Perm() != agent.PermAuto {
		t.Fatalf("y should apply the toggle, got %q", m.backend.Perm())
	}
}

func TestRenameOverlayRoundTrip(t *testing.T) {
	m := testModel(t)
	m.dispatch(actRename)
	if !m.ov.active || m.ov.kind != promptText {
		t.Fatal("rename should open a text overlay")
	}
	for _, r := range "my session" {
		m.overlayKey(string(r))
	}
	// space key arrives as "space" via bubbletea; simulate it explicitly too.
	m.overlayKey("enter")
	if m.ov.active {
		t.Fatal("enter should close the rename overlay")
	}
	if got := m.backend.Title(); got != "my session" {
		t.Fatalf("rename should set the title, got %q", got)
	}
}

func TestOverlayTextSpaceAndBackspace(t *testing.T) {
	m := testModel(t)
	m.openText("name:", "", func(m *model, v string) tea.Cmd { return nil })
	m.overlayKey("a")
	m.overlayKey("space")
	m.overlayKey("b")
	if m.ov.value != "a b" {
		t.Fatalf("space key should insert a space, got %q", m.ov.value)
	}
	m.overlayKey("backspace")
	if m.ov.value != "a " {
		t.Fatalf("backspace should delete the last rune, got %q", m.ov.value)
	}
}

func TestOverlayTextAcceptsPastedRun(t *testing.T) {
	m := testModel(t)
	m.openText("name:", "", func(m *model, v string) tea.Cmd { return nil })
	// Terminals deliver fast typing / bracketed paste as one multi-rune event
	// (key string contains spaces). It must be appended verbatim, not dropped.
	m.overlayKey("my live session")
	if m.ov.value != "my live session" {
		t.Fatalf("a pasted run should be accepted whole, got %q", m.ov.value)
	}
	// A literal space (with no space embedded) also works.
	m.overlayKey(" ")
	m.overlayKey("x")
	if m.ov.value != "my live session x" {
		t.Fatalf("trailing space + rune, got %q", m.ov.value)
	}
}

func TestClickStatusSegmentDispatches(t *testing.T) {
	m := testModel(t)
	m.width = 200
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 24})
	// Find the effort... no effort on fakeProv. Use the perm segment which is
	// always present, and verify a left-click press on it opens the confirm.
	boxes := m.statusBarLayout()
	var permBox *statusSegBox
	for i := range boxes {
		if strings.HasPrefix(boxes[i].seg.text, "perm=") {
			permBox = &boxes[i]
		}
	}
	if permBox == nil {
		t.Fatal("expected a perm segment")
	}
	l := m.computeLayout()
	x := (permBox.startCol + permBox.endCol) / 2
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: l.status.y + permBox.row})
	if !m.ov.active {
		t.Fatal("clicking the perm status segment should open its action (confirm)")
	}
}

func TestRectContains(t *testing.T) {
	r := rect{x: 2, y: 3, w: 4, h: 2} // x:2..5, y:3..4
	if !r.contains(2, 3) || !r.contains(5, 4) {
		t.Fatal("corners should be inside")
	}
	if r.contains(6, 3) || r.contains(2, 5) || r.contains(1, 3) {
		t.Fatal("outside points should be outside")
	}
	if (rect{}).contains(0, 0) {
		t.Fatal("empty rect contains nothing")
	}
}

// --- Wave 2: header bar -----------------------------------------------------

func TestHeaderRectAtTop(t *testing.T) {
	m := testModel(t)
	l := m.computeLayout()
	if l.header.y != 0 || l.header.h != 1 {
		t.Fatalf("header should be the first row, got %+v", l.header)
	}
	if l.plan.y != l.header.h {
		t.Fatalf("plan should follow the header, got plan=%+v", l.plan)
	}
	if l.transcript.y != m.topHeight() {
		t.Fatalf("transcript should start at topHeight=%d, got %d", m.topHeight(), l.transcript.y)
	}
}

func TestHeaderViewShowsTitleAndButtons(t *testing.T) {
	m := testModel(t)
	m.backend.SetTitle("my session")
	v := m.headerView()
	for _, want := range []string{"my session", "[home]", "[sessions]", "[+new]", "[config]"} {
		if !strings.Contains(v, want) {
			t.Fatalf("header view missing %q:\n%s", want, v)
		}
	}
}

func TestHeaderActionAtButtons(t *testing.T) {
	m := testModel(t)
	m.width = 100
	_, btnStart := m.headerButtonsText(100)
	if act := m.headerActionAt(btnStart+1, 0); act != actHome {
		t.Fatalf("first button should be home, got %v", act)
	}
	if act := m.headerActionAt(1, 0); act != actRename {
		t.Fatalf("title region should map to rename, got %v", act)
	}
	if act := m.headerActionAt(1, 1); act != actNone {
		t.Fatalf("non-zero localY should be actNone, got %v", act)
	}
}

func TestHeaderClickDispatches(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	_, btnStart := m.headerButtonsText(100)
	col := btnStart
	var configCol int
	for _, b := range m.headerButtons() {
		lbl := "[" + b.label + "]"
		if b.action == actConfigPanel {
			configCol = col + 1
		}
		col += len(lbl) + 1
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: configCol, Y: 0})
	if !m.conf.active {
		t.Fatal("clicking [config] in the header should open the config panel")
	}
}

func TestHeaderTitleClickOpensRename(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: 0})
	if !m.ov.active || m.ov.kind != promptText {
		t.Fatal("clicking the header title should open the rename prompt")
	}
}

func TestAnsiTrunc(t *testing.T) {
	if got := ansiTrunc("hello world", 5); got != "hell…" {
		t.Fatalf("truncate to 5 = %q", got)
	}
	if got := ansiTrunc("hi", 10); got != "hi" {
		t.Fatalf("no truncation needed = %q", got)
	}
	if got := ansiTrunc("anything", 0); got != "" {
		t.Fatalf("zero width = %q", got)
	}
}

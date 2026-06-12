package tui

// Tests for the Tier 9 chrome foundation: layout rects, region hit-testing, the
// action registry, the confirm/text overlay, and clickable status segments.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/chat"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

// --- Wave 3: left session rail ---------------------------------------------

func TestRailHiddenForLocalBackend(t *testing.T) {
	m := testModel(t) // plain local backend: no SessionLister
	if m.railVisible() {
		t.Fatal("local chats have no siblings — the rail must stay hidden")
	}
	if m.railWidth() != 0 {
		t.Fatal("hidden rail has zero width")
	}
	// The transcript band falls back to the plain viewport.
	if m.transcriptBand() != m.vp.View() {
		t.Fatal("with no rail the band is just the viewport")
	}
}

func TestRailVisibleForDaemonBackend(t *testing.T) {
	m := switcherModel(t) // has a SessionLister with 3 entries
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.refreshRail()
	if !m.railVisible() {
		t.Fatal("daemon-hosted backend on a wide terminal should show the rail")
	}
	if m.railWidth() != railWidthCols {
		t.Fatalf("rail width = %d, want %d", m.railWidth(), railWidthCols)
	}
	// The viewport shrank by the rail width.
	if m.vp.Width != 100-railWidthCols {
		t.Fatalf("viewport width should shrink by the rail, got %d", m.vp.Width)
	}
	// The rail renders the session titles.
	band := m.transcriptBand()
	for _, want := range []string{"sessions", "first", "current", "third"} {
		if !strings.Contains(band, want) {
			t.Fatalf("rail band missing %q:\n%s", want, band)
		}
	}
}

func TestRailHidesOnNarrowTerminal(t *testing.T) {
	m := switcherModel(t)
	m.Update(tea.WindowSizeMsg{Width: 70, Height: 24}) // < railMinTerminalWidth
	m.refreshRail()
	if m.railVisible() {
		t.Fatal("rail must hide on a narrow terminal (transcript needs the width)")
	}
	if m.vp.Width != 70 {
		t.Fatalf("narrow viewport should use the full width, got %d", m.vp.Width)
	}
}

func TestRailLayoutShiftsTranscript(t *testing.T) {
	m := switcherModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.refreshRail()
	l := m.computeLayout()
	if l.leftRail.empty() {
		t.Fatal("rail rect should be present")
	}
	if l.transcript.x != railWidthCols {
		t.Fatalf("transcript origin should shift right by the rail, got x=%d", l.transcript.x)
	}
	// A click on the transcript (right of the rail) hits regTranscript.
	if h := m.hitTest(railWidthCols+1, l.transcript.y); h.region != regTranscript {
		t.Fatalf("right of the rail should be transcript, got %v", h.region)
	}
	// A click in the rail column hits regLeftRail.
	if h := m.hitTest(1, l.transcript.y); h.region != regLeftRail {
		t.Fatalf("left column should be the rail, got %v", h.region)
	}
}

func TestPanelTitleLineHasClose(t *testing.T) {
	line := panelTitleLine("changes", 20, true)
	if !strings.Contains(line, "changes") || !strings.Contains(line, "[x]") {
		t.Fatalf("panel title should include title + close affordance: %q", line)
	}
	if got := ansi.StringWidth(ansi.Strip(line)); got != 20 {
		t.Fatalf("panel title width = %d, want 20 (%q)", got, line)
	}
	if !panelCloseAt(18, 0, 20) || !panelCloseAt(19, 0, 20) {
		t.Fatal("right-aligned [x] should be clickable")
	}
	if panelCloseAt(1, 0, 20) || panelCloseAt(18, 1, 20) {
		t.Fatal("outside close rect should not be clickable")
	}
}

func TestPanelCloseClickTogglesRail(t *testing.T) {
	m := switcherModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.refreshRail()
	l := m.computeLayout()
	if l.leftRail.empty() {
		t.Fatal("rail should be visible")
	}
	// The rail close [x] is right-aligned in the panel title row before the
	// separator column, so click the x cell.
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.leftRail.x + l.leftRail.w - 2, Y: l.leftRail.y})
	if m.railOn {
		t.Fatal("clicking rail [x] should toggle the rail off")
	}
}

func TestPanelCloseClickTogglesChanges(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.text("user", "edit")
	m.push(editBlock("f.go", "a", "b"))
	m.relayout()
	l := m.computeLayout()
	if l.rightPanel.empty() {
		t.Fatal("changes panel should be visible")
	}
	// Right panel has a leading "│ " gutter; [x] is inside the remaining width.
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: l.rightPanel.x + l.rightPanel.w - 1, Y: l.rightPanel.y})
	if m.changesOn {
		t.Fatal("clicking changes [x] should toggle the panel off")
	}
}

func TestPanelToggleKeys(t *testing.T) {
	m := switcherModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if m.railOn {
		t.Fatal("ctrl+b should toggle rail off")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if m.changesOn {
		t.Fatal("ctrl+g should toggle changes off")
	}
}

func TestRailRowClickHops(t *testing.T) {
	m := switcherModel(t) // current is s2; entries s1,s2,s3
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.refreshRail()
	l := m.computeLayout()
	// Row 0 is the "sessions" header; entry 0 (s1) is at rail row 1.
	_, cmd := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: l.leftRail.y + 1})
	if m.switchTo != "s1" {
		t.Fatalf("clicking the first rail row should hop to s1, got %q", m.switchTo)
	}
	if cmd == nil {
		t.Fatal("a hop should quit (to switch)")
	}
}

func TestRailClickCurrentSessionNoop(t *testing.T) {
	m := switcherModel(t) // current is s2 → rail row 2
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.refreshRail()
	l := m.computeLayout()
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: l.leftRail.y + 2})
	if m.switchTo != "" {
		t.Fatal("clicking the current session must be a no-op")
	}
}

func TestRailToggleCommand(t *testing.T) {
	m := switcherModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	if !m.railOn {
		t.Fatal("rail is on by default")
	}
	m.command("/rail")
	if m.railOn {
		t.Fatal("/rail should toggle the rail off")
	}
	m.command("/rail")
	if !m.railOn {
		t.Fatal("/rail should toggle it back on")
	}
}

func TestRailScreenToContentRebased(t *testing.T) {
	m := switcherModel(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.refreshRail()
	m.text("assistant", "hello transcript")
	// A click inside the rail column must NOT map to transcript content.
	if _, ok := m.screenToContent(1, m.topHeight()); ok {
		t.Fatal("a point in the rail column should not be transcript content")
	}
	// A click right of the rail maps to content (rebased by the rail width).
	if _, ok := m.screenToContent(railWidthCols+2, m.topHeight()); !ok {
		t.Fatal("a point right of the rail should be transcript content")
	}
}

// --- Wave 4: right changes panel -------------------------------------------

// editBlock builds a tool block representing an edit to path with the given
// old→new strings (so toolDetail produces a diff with stats).
func editBlock(path, old, neu string) *block {
	args, _ := json.Marshal(map[string]string{"path": path, "old_string": old, "new_string": neu})
	return &block{kind: blockTool, toolName: "edit", toolArgs: args, state: toolDone}
}

func TestChangesHiddenWithNoEdits(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.text("user", "hi")
	m.text("assistant", "no edits here")
	if m.changesVisible() {
		t.Fatal("changes panel must hide when the last run made no edits")
	}
	if m.rightPanelWidth() != 0 {
		t.Fatal("hidden panel has zero width")
	}
}

func TestChangesShowsLastRunFiles(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.text("user", "edit some files")
	m.push(editBlock("src/main.go", "foo", "bar"))
	m.push(editBlock("README.md", "old line", "new line\nextra"))
	if !m.changesVisible() {
		t.Fatal("changes panel should show after an edit-producing run")
	}
	if m.rightPanelWidth() != rightPanelWidthCols {
		t.Fatalf("panel width = %d", m.rightPanelWidth())
	}
	changes := m.lastRunChanges()
	if len(changes) != 2 {
		t.Fatalf("want 2 changed files, got %d: %+v", len(changes), changes)
	}
	if changes[0].path != "src/main.go" || changes[1].path != "README.md" {
		t.Fatalf("files in first-touched order expected, got %+v", changes)
	}
	band := m.transcriptBand()
	for _, want := range []string{"changes", "[x]", "main.go", "README.md"} {
		if !strings.Contains(band, want) {
			t.Fatalf("changes band missing %q:\n%s", want, band)
		}
	}
}

func TestChangesAggregatesSameFile(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.text("user", "edit twice")
	m.push(editBlock("a.go", "x", "y"))
	m.push(editBlock("a.go", "p", "q"))
	changes := m.lastRunChanges()
	if len(changes) != 1 {
		t.Fatalf("same file should aggregate to one row, got %d", len(changes))
	}
	if changes[0].adds < 2 || changes[0].dels < 2 {
		t.Fatalf("stats should sum across both edits, got +%d -%d", changes[0].adds, changes[0].dels)
	}
}

func TestChangesOnlyLastRun(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	// Run 1 edits old.go.
	m.text("user", "first task")
	m.push(editBlock("old.go", "a", "b"))
	// Run 2 edits new.go — the panel should show only run 2.
	m.text("user", "second task")
	m.push(editBlock("new.go", "c", "d"))
	changes := m.lastRunChanges()
	if len(changes) != 1 || changes[0].path != "new.go" {
		t.Fatalf("only the last run's edits should show, got %+v", changes)
	}
}

func TestChangesFallsBackToEarlierRunWhenLastHasNoEdits(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.text("user", "edit task")
	m.push(editBlock("edited.go", "a", "b"))
	// A later run with no edits (just a question) must not blank the panel —
	// it shows the most recent run that DID produce edits.
	m.text("user", "just a question")
	m.text("assistant", "an answer, no edits")
	changes := m.lastRunChanges()
	if len(changes) != 1 || changes[0].path != "edited.go" {
		t.Fatalf("should fall back to the last edit-producing run, got %+v", changes)
	}
}

func TestChangesLayoutNarrowsTranscript(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.text("user", "edit")
	m.push(editBlock("f.go", "a", "b"))
	m.relayout()
	l := m.computeLayout()
	if l.rightPanel.empty() {
		t.Fatal("right panel rect should be present")
	}
	if l.rightPanel.x != m.width-rightPanelWidthCols {
		t.Fatalf("right panel should be flush right, got x=%d", l.rightPanel.x)
	}
	// Transcript ends where the panel begins.
	if l.transcript.x+l.transcript.w != l.rightPanel.x {
		t.Fatalf("transcript should abut the panel: tr=%+v panel=%+v", l.transcript, l.rightPanel)
	}
	if h := m.hitTest(m.width-1, l.rightPanel.y); h.region != regRightPanel {
		t.Fatalf("far-right column should be the changes panel, got %v", h.region)
	}
}

func TestChangesRowClickJumps(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.text("user", "edit")
	m.push(editBlock("src/main.go", "foo", "bar"))
	editIdx := len(m.blocks) - 1
	m.relayout()
	l := m.computeLayout()
	// Row 0 of the panel is the header; the first file is at panel row 1.
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: m.width - 2, Y: l.rightPanel.y + 1})
	if m.sel != editIdx {
		t.Fatalf("clicking a changes file should select its tool block, got sel=%d want %d", m.sel, editIdx)
	}
	if m.blocks[editIdx].collapsed {
		t.Fatal("jumped-to block should be expanded")
	}
}

func TestChangesHidesWhenTooNarrow(t *testing.T) {
	m := testModel(t)
	// Width that fits the transcript minimum but not the panel too.
	m.Update(tea.WindowSizeMsg{Width: minTranscriptCols + rightPanelWidthCols - 5, Height: 24})
	m.text("user", "edit")
	m.push(editBlock("f.go", "a", "b"))
	if m.changesVisible() {
		t.Fatal("changes panel must hide when it would squeeze the transcript below its minimum")
	}
}

func TestChangesToggleCommand(t *testing.T) {
	m := testModel(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.text("user", "edit")
	m.push(editBlock("f.go", "a", "b"))
	if !m.changesOn {
		t.Fatal("changes panel is on by default")
	}
	m.command("/changes")
	if m.changesOn {
		t.Fatal("/changes should toggle it off")
	}
	if m.changesVisible() {
		t.Fatal("toggled-off panel must not be visible")
	}
}

func TestFilesInPatch(t *testing.T) {
	patch := "diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1,2 @@\n-old\n+new\n+more\ndiff --git a/y.go b/y.go\n--- a/y.go\n+++ b/y.go\n@@ -1 +1 @@\n-a\n+b\n"
	files := filesInPatch(patch, 7)
	if len(files) != 2 {
		t.Fatalf("patch touches 2 files, got %d: %+v", len(files), files)
	}
	if files[0].path != "x.go" || files[0].adds != 2 || files[0].dels != 1 {
		t.Fatalf("x.go stats wrong: %+v", files[0])
	}
	if files[1].path != "y.go" {
		t.Fatalf("y.go expected, got %+v", files[1])
	}
}

// --- Wave 5: command palette ------------------------------------------------

func TestConfigPanelBackAffordance(t *testing.T) {
	m := testModel(t)
	m.openConfigPanel()
	v := m.View()
	if !strings.Contains(v, "‹ back") {
		t.Fatalf("config panel should show a visible back affordance:\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.conf.active {
		t.Fatal("backspace should close the config panel")
	}
	m.openConfigPanel()
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: 0})
	if m.conf.active {
		t.Fatal("clicking ‹ back should close the config panel")
	}
}

func TestPaletteOpensWithCtrlK(t *testing.T) {
	m := testModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if !m.pal.active {
		t.Fatal("ctrl+k should open the palette")
	}
	if len(m.pal.matches) != len(m.paletteCatalog()) {
		t.Fatalf("empty query should show all entries, got %d", len(m.pal.matches))
	}
	if !strings.Contains(m.View(), "command") {
		t.Fatal("palette view should render")
	}
}

func TestPaletteFuzzyFilters(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	for _, r := range "rename" {
		m.paletteKey(string(r))
	}
	if len(m.pal.matches) == 0 {
		t.Fatal("'rename' should match at least one entry")
	}
	if !strings.Contains(strings.ToLower(m.pal.matches[0].label), "rename") {
		t.Fatalf("top match for 'rename' should be the rename entry, got %q", m.pal.matches[0].label)
	}
}

func TestPaletteSubsequenceMatch(t *testing.T) {
	if fuzzyScore("config panel", "cfg") < 0 {
		t.Fatal("cfg should subsequence-match 'config panel'")
	}
	sub := fuzzyScore("config panel", "config")
	seq := fuzzyScore("config panel", "cfg")
	if !(sub < seq) {
		t.Fatalf("substring (%d) should rank better than subsequence (%d)", sub, seq)
	}
	if fuzzyScore("config", "xyz") >= 0 {
		t.Fatal("non-matching query should score -1")
	}
}

func TestPaletteEnterRunsAction(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	for _, r := range "rename" {
		m.paletteKey(string(r))
	}
	m.paletteKey("enter")
	if m.pal.active {
		t.Fatal("enter should close the palette")
	}
	if !m.ov.active || m.ov.kind != promptText {
		t.Fatal("running 'rename session' should open the rename prompt")
	}
}

func TestPaletteEnterPrefillsArgSlash(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	for _, r := range "find in" {
		m.paletteKey(string(r))
	}
	if len(m.pal.matches) == 0 {
		t.Fatal("expected a match for 'find in'")
	}
	m.paletteKey("enter")
	if m.ti.Value() != "/find " {
		t.Fatalf("an arg-taking slash entry should prefill the input, got %q", m.ti.Value())
	}
}

func TestPaletteEscCancels(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	m.paletteKey("esc")
	if m.pal.active {
		t.Fatal("esc should close the palette")
	}
}

func TestPaletteNavigation(t *testing.T) {
	m := testModel(t)
	m.openPalette()
	m.paletteKey("down")
	if m.pal.idx != 1 {
		t.Fatalf("down should move to idx 1, got %d", m.pal.idx)
	}
	m.paletteKey("up")
	if m.pal.idx != 0 {
		t.Fatalf("up should move back to 0, got %d", m.pal.idx)
	}
}

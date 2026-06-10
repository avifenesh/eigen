package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/session"
	"github.com/avifenesh/eigen/internal/skill"
	"github.com/avifenesh/eigen/internal/tool"
	"github.com/avifenesh/eigen/internal/transcript"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakeProv is a canned provider so the model can be driven without a network or
// real backend. It returns a final answer with no tool calls.
type fakeProv struct{ text string }

func (fakeProv) Name() string { return "fake" }
func (p fakeProv) Complete(context.Context, llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: p.text}, nil
}

// testModel builds a model wired to the fake provider, in the same shape Run
// assembles it, but without starting Bubble Tea.
func testModel(t *testing.T) *model {
	t.Helper()
	reg, err := tool.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	a := &agent.Agent{Provider: fakeProv{text: "done"}, Tools: reg, Perm: agent.PermAuto}
	sp := spinner.New()
	ti := textarea.New()
	ti.ShowLineNumbers = false
	ti.MaxHeight = inputMaxRows
	ti.SetHeight(1)
	ti.KeyMap.InsertNewline.SetEnabled(false)
	ti.Focus()
	m := &model{
		a:           a,
		sp:          sp,
		ti:          ti,
		session:     a.NewSession(),
		ctx:         context.Background(),
		state:       stInput,
		srcDir:      t.TempDir(),
		sessionPath: t.TempDir() + "/s.eigen.jsonl",
		// store intentionally nil: resume paths must guard against it.
	}
	// Give it a viewport so View() and sync() are exercised.
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// keys feeds a sequence of key strings through Update, failing on any panic
// (Update recovers, so we assert via state instead — see TestUpdateRecovers).
func typeRunes(m *model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestTypingIncludingSpaceThenSubmit(t *testing.T) {
	m := testModel(t)
	typeRunes(m, "fix the bug")
	if got := m.ti.Value(); got != "fix the bug" {
		t.Fatalf("input value = %q, want %q (space handling broken?)", got, "fix the bug")
	}
	// Enter submits and transitions to running.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.state != stRunning {
		t.Fatalf("after submit state = %v, want stRunning", m.state)
	}
}

func TestCtrlJInsertsNewline(t *testing.T) {
	m := testModel(t)
	typeRunes(m, "line one")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	typeRunes(m, "line two")
	if got := m.ti.Value(); got != "line one\nline two" {
		t.Fatalf("ctrl+j should insert a newline, got %q", got)
	}
	// The input grew to two text rows (+2 border rows).
	if m.ti.Height() < 2 {
		t.Fatalf("multi-line input should grow to >=2 text rows, got %d", m.ti.Height())
	}
	// Enter still submits the whole multi-line value.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.state != stRunning {
		t.Fatal("enter should submit the multi-line prompt")
	}
}

func TestLongLineWrapsGrowsInput(t *testing.T) {
	m := testModel(t)
	// Input text width ~ 80-2. A long single logical line (no newlines) must
	// soft-wrap and grow the box, even though LineCount() stays 1.
	long := strings.Repeat("word ", 60) // ~300 cols → several wrapped rows
	typeRunes(m, long)
	if m.ti.LineCount() != 1 {
		t.Fatalf("a single logical line should stay LineCount 1, got %d", m.ti.LineCount())
	}
	if m.ti.Height() < 2 {
		t.Fatalf("a long soft-wrapped line should grow the input box, got height %d", m.ti.Height())
	}
}

func TestEmptySubmitIsNoop(t *testing.T) {
	m := testModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.state != stInput {
		t.Fatal("empty enter should not start a turn")
	}
}

func TestNavigationKeysNoPanic(t *testing.T) {
	m := testModel(t)
	// Add a couple collapsible blocks.
	m.push(&block{kind: blockThinking, title: "thinking", collapsed: true, body: sb("a\nb")})
	m.push(&block{kind: blockTool, title: "bash", collapsed: true})
	for _, k := range []tea.KeyType{tea.KeyUp, tea.KeyUp, tea.KeyDown, tea.KeyTab, tea.KeyPgUp, tea.KeyPgDown} {
		m.Update(tea.KeyMsg{Type: k})
	}
}

func TestTurnDoneAutosaves(t *testing.T) {
	m := testModel(t)
	m.session = m.a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "hi"}})
	m.Update(turnDoneMsg{})
	if m.state != stInput {
		t.Fatal("turnDone should return to input state")
	}
	// Autosave wrote the session file.
	if _, err := os.Stat(m.sessionPath); err != nil {
		t.Fatalf("turnDone did not autosave session: %v", err)
	}
}

func TestTurnDoneWithErrorShowsErrorBlock(t *testing.T) {
	m := testModel(t)
	before := len(m.blocks)
	m.Update(turnDoneMsg{err: context.Canceled})
	if len(m.blocks) != before+1 {
		t.Fatal("error turnDone should push an error block")
	}
	if !m.blocks[len(m.blocks)-1].isErr {
		t.Fatal("pushed block should be marked isErr")
	}
}

func TestApprovalFlow(t *testing.T) {
	m := testModel(t)
	reply := make(chan bool, 1)
	m.Update(approvalMsg{name: "write", args: json.RawMessage(`{"path":"x"}`), reply: reply})
	if m.pending == nil {
		t.Fatal("approvalMsg should set pending")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	select {
	case ok := <-reply:
		if !ok {
			t.Fatal("y should approve")
		}
	default:
		t.Fatal("approval reply not sent")
	}
	if m.pending != nil {
		t.Fatal("pending should clear after reply")
	}
}

func TestAgentEventRendering(t *testing.T) {
	m := testModel(t)
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "hello "}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "world"}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "bash", ToolArgs: json.RawMessage(`{}`)}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolResult, ToolName: "bash", Result: "ok"}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventDone, Text: "world"}})
	// The two text deltas must coalesce into a single assistant block.
	var asst *block
	for _, b := range m.blocks {
		if b.kind == blockText && b.role == "assistant" {
			asst = b
		}
	}
	if asst == nil || asst.body != "hello world" {
		t.Fatalf("text deltas did not coalesce: %+v", asst)
	}
}

func TestPickerNavigationWithNilStore(t *testing.T) {
	m := testModel(t)
	m.picking = true
	m.picks = []*session.Meta{
		{ID: "eig_a", Title: "alpha", Source: "claude"},
		{ID: "eig_b", Title: "beta", Source: "eigen"},
	}
	m.pickIdx = 0
	// Navigate down past the end, back up, then select.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.pickIdx != 1 {
		t.Fatalf("pickIdx = %d, want 1", m.pickIdx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // clamp at last
	if m.pickIdx != 1 {
		t.Fatalf("pickIdx should clamp at 1, got %d", m.pickIdx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.pickIdx != 0 {
		t.Fatalf("pickIdx = %d, want 0", m.pickIdx)
	}
	// Enter with a nil store must not panic (guarded in loadSessionByID).
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.picking {
		t.Fatal("enter should close the picker")
	}
	// View should render without the picker now.
	_ = m.View()
}

func TestPickerEscCancels(t *testing.T) {
	m := testModel(t)
	m.picking = true
	m.picks = []*session.Meta{{ID: "eig_a", Title: "alpha"}}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.picking {
		t.Fatal("esc should cancel the picker")
	}
}

func TestCommands(t *testing.T) {
	m := testModel(t)
	for _, cmd := range []string{"/help", "/clear", "/resume", "/bogus"} {
		m.command(cmd)
	}
	// /resume with nil store should have noted "no session store".
	found := false
	for _, b := range m.blocks {
		if strings.Contains(b.body, "no session store") {
			found = true
		}
	}
	if !found {
		t.Fatal("/resume with nil store should report no session store")
	}
}

func TestUpdateRecoversFromPanic(t *testing.T) {
	m := testModel(t)
	// Force a panic inside Update by making the picker index invalid: enter on
	// an empty picks slice indexes out of range, which the recover must catch.
	m.picking = true
	m.picks = nil
	m.pickIdx = 0
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next == nil {
		t.Fatal("Update should return a model even after a recovered panic")
	}
	if m.picking {
		t.Fatal("recover should reset picking")
	}
	if m.state != stInput {
		t.Fatal("recover should reset to input state")
	}
	last := m.blocks[len(m.blocks)-1]
	if !last.isErr || !strings.Contains(last.body, "internal error") {
		t.Fatalf("recover should push an internal-error block, got %+v", last)
	}
}

// --- steer + queue ---------------------------------------------------------

func TestQueueWhileRunning(t *testing.T) {
	m := testModel(t)
	m.state = stRunning
	m.status = "thinking"
	typeRunes(m, "do the next thing")
	if m.ti.Value() != "do the next thing" {
		t.Fatalf("input should accept typing while running, got %q", m.ti.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.queued) != 1 || m.queued[0] != "do the next thing" {
		t.Fatalf("enter should queue while running: %v", m.queued)
	}
	if m.ti.Value() != "" {
		t.Fatal("input should reset after queueing")
	}
	// turnDone drains the queue and starts the queued message as a new turn.
	m.Update(turnDoneMsg{})
	if len(m.queued) != 0 {
		t.Fatalf("queue should drain on turnDone, left %v", m.queued)
	}
	if m.state != stRunning {
		t.Fatal("draining a queued message should start a new turn")
	}
	found := false
	for _, b := range m.blocks {
		if b.kind == blockText && b.role == "user" && b.body == "do the next thing" {
			found = true
		}
	}
	if !found {
		t.Fatal("queued message should be submitted as a user turn")
	}
}

func TestEscInterruptsRunningTurn(t *testing.T) {
	m := testModel(t)
	interrupted := false
	m.state = stRunning
	m.cancel = func() { interrupted = true }
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !interrupted {
		t.Fatal("esc should cancel the running turn")
	}
}

func TestBottomHeightReflectsState(t *testing.T) {
	m := testModel(t)
	// 1 input line + 2 border rows + 1 status bar (now at the bottom).
	if m.bottomHeight() != 4 {
		t.Fatalf("input bottomHeight=%d want 4", m.bottomHeight())
	}
	m.state = stRunning
	// + 1 spinner/status line while running.
	if m.bottomHeight() != 5 {
		t.Fatalf("running bottomHeight=%d want 5", m.bottomHeight())
	}
	m.state = stInput
	typeRunes(m, "/")
	if m.bottomHeight() <= 4 {
		t.Fatal("open slash menu should add rows to bottomHeight")
	}
}

// --- mouse click ------------------------------------------------------------

func TestMouseClickTogglesBlock(t *testing.T) {
	m := testModel(t)
	m.note("a note before it")
	tb := m.push(&block{kind: blockThinking, title: "thinking", collapsed: true, body: sb("line a\nline b")})
	idx := len(m.blocks) - 1
	// Screen row = topHeight (panels above the viewport) + viewport-relative row.
	row := m.topHeight() + m.blockStart[idx] - m.vp.YOffset
	// A click is a press + release with no motion in between.
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: row})
	m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Y: row})
	if tb.collapsed {
		t.Fatal("click on a collapsed block should expand it")
	}
	row = m.topHeight() + m.blockStart[idx] - m.vp.YOffset
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: row})
	m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Y: row})
	if !tb.collapsed {
		t.Fatal("second click should collapse it again")
	}
}

func TestMouseClickAccountsForPlanPanel(t *testing.T) {
	m := testModel(t)
	// A plan panel pushes the viewport down by topHeight() rows.
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "todo",
		ToolArgs: json.RawMessage(`{"todos":[{"content":"a","status":"pending"}]}`)}})
	if m.topHeight() == 0 {
		t.Fatal("a plan panel should make topHeight > 0")
	}
	tb := m.push(&block{kind: blockTool, toolName: "bash", title: "bash", collapsed: true,
		toolArgs: json.RawMessage(`{"command":"ls"}`)})
	idx := len(m.blocks) - 1

	// Clicking at the viewport-relative row WITHOUT the offset must NOT toggle
	// (this is the old bug: the click landed too high).
	wrong := m.blockStart[idx] - m.vp.YOffset
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: wrong})
	m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Y: wrong})
	if !tb.collapsed {
		t.Fatal("click above the block (no topHeight offset) should not toggle it")
	}
	// Clicking at the correct screen row (offset by topHeight) toggles it.
	right := m.topHeight() + m.blockStart[idx] - m.vp.YOffset
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: right})
	m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, Y: right})
	if tb.collapsed {
		t.Fatal("click on the actual block row should expand it")
	}
}

func TestMouseWheelDoesNotPanic(t *testing.T) {
	m := testModel(t)
	for i := 0; i < 5; i++ {
		m.push(&block{kind: blockText, role: "assistant", body: sb("line")})
	}
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
}

func TestMouseDragAutoCopies(t *testing.T) {
	m := testModel(t)
	fc := &fakeClip{avail: true}
	m.clip = fc
	m.text("assistant", "copy this text")
	idx := len(m.blocks) - 1
	row := m.topHeight() + m.blockStart[idx] - m.vp.YOffset
	// Press at column 0, drag to column 9 (no release in between), then release.
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: row})
	m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: 9, Y: row})
	m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, X: 9, Y: row})
	if fc.copied == "" {
		t.Fatal("dragging a selection should auto-copy to the clipboard")
	}
	if !strings.HasPrefix("copy this text", fc.copied) && !strings.Contains("copy this text", fc.copied) {
		t.Fatalf("copied text should come from the dragged line, got %q", fc.copied)
	}
}

func TestMouseDragMultiLineCopies(t *testing.T) {
	m := testModel(t)
	fc := &fakeClip{avail: true}
	m.clip = fc
	m.text("assistant", "first line")
	m.text("assistant", "second line")
	// Drag from the first assistant block's row to the second.
	r1 := m.topHeight() + m.blockStart[len(m.blocks)-2] - m.vp.YOffset
	r2 := m.topHeight() + m.blockStart[len(m.blocks)-1] - m.vp.YOffset
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: r1})
	m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, X: 11, Y: r2})
	m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, X: 11, Y: r2})
	if !strings.Contains(fc.copied, "first") || !strings.Contains(fc.copied, "second") {
		t.Fatalf("multi-line drag should copy both lines, got %q", fc.copied)
	}
}

// --- slash autocomplete -----------------------------------------------------

func TestSlashMenuOpensAndFilters(t *testing.T) {
	m := testModel(t)
	typeRunes(m, "/")
	if !m.comp.active() || m.comp.kind != compSlash {
		t.Fatal("typing / should open the slash menu")
	}
	if len(m.comp.items) != len(slashCommands) {
		t.Fatalf("/ should list all %d commands, got %d", len(slashCommands), len(m.comp.items))
	}
	typeRunes(m, "re")
	if len(m.comp.items) == 0 {
		t.Fatal("/re should match at least one command")
	}
	for _, it := range m.comp.items {
		if !strings.HasPrefix(it.label, "/re") {
			t.Fatalf("filter leaked non-matching command %q", it.label)
		}
	}
}

func TestSlashMenuClosesOnSpaceOrText(t *testing.T) {
	m := testModel(t)
	typeRunes(m, "/save ")
	if m.comp.active() {
		t.Fatal("a space after the command should close the menu (now typing args)")
	}
}

func TestSlashMenuTabCompletes(t *testing.T) {
	m := testModel(t)
	typeRunes(m, "/re")
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.comp.active() {
		t.Fatal("tab should close the menu")
	}
	if !strings.HasPrefix(m.ti.Value(), "/re") || !strings.HasSuffix(m.ti.Value(), " ") {
		t.Fatalf("tab should fill the command + trailing space, got %q", m.ti.Value())
	}
}

func TestSlashMenuEnterRunsCommand(t *testing.T) {
	m := testModel(t)
	typeRunes(m, "/h")
	if len(m.comp.items) != 1 || m.comp.items[0].label != "/help" {
		t.Fatalf("expected only /help, got %v", m.comp.items)
	}
	before := len(m.blocks)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.comp.active() {
		t.Fatal("enter should close the menu")
	}
	if len(m.blocks) <= before {
		t.Fatal("/help via menu should push a note")
	}
}

func TestSlashMenuNavigationAndEsc(t *testing.T) {
	m := testModel(t)
	typeRunes(m, "/")
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.comp.idx != 1 {
		t.Fatalf("down should move selection to 1, got %d", m.comp.idx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.comp.idx != 0 {
		t.Fatalf("up should move selection back to 0, got %d", m.comp.idx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.comp.active() {
		t.Fatal("esc should close the slash menu")
	}
}

// --- rich tool rendering + live status -------------------------------------

func TestToolSummary(t *testing.T) {
	cases := []struct{ name, args, want string }{
		{"read", `{"path":"src/main.go"}`, "read src/main.go"},
		{"list", `{"path":"src"}`, "list src"},
		{"write", `{"path":"a.go"}`, "write a.go"},
		{"edit", `{"path":"a.go"}`, "edit a.go"},
		{"bash", `{"command":"ls -la"}`, "bash ls -la"},
		{"grep", `{"pattern":"foo","path":"src"}`, "grep foo in src"},
		{"grep", `{"pattern":"foo"}`, "grep foo"},
		{"glob", `{"pattern":"**/*.go"}`, "glob **/*.go"},
		{"fetch", `{"url":"https://example.com"}`, "fetch https://example.com"},
	}
	for _, c := range cases {
		if got := toolSummary(c.name, json.RawMessage(c.args)); got != c.want {
			t.Errorf("toolSummary(%s, %s) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

func TestToolBlockLiveStatus(t *testing.T) {
	m := testModel(t)
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "read", ToolArgs: json.RawMessage(`{"path":"x"}`)}})
	var tb *block
	for _, b := range m.blocks {
		if b.kind == blockTool {
			tb = b
		}
	}
	if tb == nil || tb.state != toolRunning {
		t.Fatal("tool start should create a running tool block")
	}
	if g := tb.statusGlyph(); !strings.Contains(g, "•") {
		t.Fatalf("running glyph should contain '•', got %q", g)
	}
	if !strings.Contains(tb.header(), "read x") {
		t.Fatalf("tool header should use the rich summary, got %q", tb.header())
	}
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolResult, ToolName: "read", Result: "contents"}})
	if tb.state != toolDone {
		t.Fatal("successful tool result should mark the block done")
	}

	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "bash", ToolArgs: json.RawMessage(`{"command":"boom"}`)}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolResult, ToolName: "bash", Result: "err", IsError: true}})
	var bb *block
	for _, b := range m.blocks {
		if b.kind == blockTool && b.toolName == "bash" {
			bb = b
		}
	}
	if bb == nil || bb.state != toolFailed {
		t.Fatal("errored tool result should mark the block failed")
	}
}

// --- @file mentions ---------------------------------------------------------

func TestMentionMenuCompletes(t *testing.T) {
	m := testModel(t)
	m.fileIdx = []string{"src/main.go", "src/util.go", "README.md"}
	m.fileIdxAt = time.Now()
	typeRunes(m, "look at @ma")
	if !m.comp.active() || m.comp.kind != compMention {
		t.Fatalf("@ma should open the mention menu (active=%v kind=%d)", m.comp.active(), m.comp.kind)
	}
	if m.comp.items[0].label != "src/main.go" {
		t.Fatalf("expected src/main.go ranked first, got %q", m.comp.items[0].label)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.comp.active() {
		t.Fatal("tab should close the mention menu")
	}
	if want := "look at src/main.go "; m.ti.Value() != want {
		t.Fatalf("mention insert wrong: got %q want %q", m.ti.Value(), want)
	}
}

func TestMentionNeedsWordBoundary(t *testing.T) {
	m := testModel(t)
	m.fileIdx = []string{"main.go"}
	m.fileIdxAt = time.Now()
	// "@" glued to a preceding word (e.g. an email-ish token) must not trigger.
	typeRunes(m, "foo@ma")
	if m.comp.active() {
		t.Fatal("@ mid-word should not open the mention menu")
	}
}

func TestMentionEnterInserts(t *testing.T) {
	m := testModel(t)
	m.fileIdx = []string{"README.md"}
	m.fileIdxAt = time.Now()
	typeRunes(m, "@re")
	if m.comp.kind != compMention {
		t.Fatal("@re should open the mention menu")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.comp.active() {
		t.Fatal("enter should close the mention menu")
	}
	if want := "README.md "; m.ti.Value() != want {
		t.Fatalf("enter on a mention should insert the path, got %q", m.ti.Value())
	}
}

func TestIndexFilesSkipsVCSAndBuildDirs(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(".git/config")
	mustWrite("node_modules/pkg/index.js")
	mustWrite("src/main.go")
	mustWrite("README.md")

	files := indexFiles(dir)
	joined := strings.Join(files, ",")
	if strings.Contains(joined, ".git") || strings.Contains(joined, "node_modules") {
		t.Fatalf("VCS/build dirs should be skipped, got %v", files)
	}
	if !strings.Contains(joined, "main.go") || !strings.Contains(joined, "README.md") {
		t.Fatalf("expected source files indexed, got %v", files)
	}
}

// --- /perm and /model -------------------------------------------------------

func TestPermCommand(t *testing.T) {
	m := testModel(t)
	m.command("/perm gated")
	if m.a.Perm != agent.PermGated {
		t.Fatal("/perm gated should switch posture to gated")
	}
	m.command("/perm auto")
	if m.a.Perm != agent.PermAuto {
		t.Fatal("/perm auto should switch posture to auto")
	}
	before := len(m.blocks)
	m.command("/perm")
	if len(m.blocks) <= before {
		t.Fatal("/perm with no arg should report the current posture")
	}
	m.command("/perm bogus")
	if last := m.blocks[len(m.blocks)-1]; !last.isErr {
		t.Fatal("/perm bogus should report an error")
	}
}

// loadMetaForTest reads the session meta sidecar written alongside sessionPath.
func loadMetaForTest(t *testing.T, sessionPath string) (transcript.SessionMeta, bool) {
	t.Helper()
	return transcript.LoadMeta(sessionPath)
}

func TestCtrlATogglesPerm(t *testing.T) {
	m := testModel(t)
	m.a.Perm = agent.PermGated
	// ctrl+a flips gated → auto.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if m.a.Perm != agent.PermAuto {
		t.Fatalf("ctrl+a should flip gated→auto, got %q", m.a.Perm)
	}
	// And back auto → gated.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if m.a.Perm != agent.PermGated {
		t.Fatalf("ctrl+a should flip auto→gated, got %q", m.a.Perm)
	}
	// It persists the posture to the session meta sidecar.
	if meta, ok := loadMetaForTest(t, m.sessionPath); !ok || meta.Perm != "gated" {
		t.Fatalf("ctrl+a should persist perm to meta, got %+v (ok=%v)", meta, ok)
	}
}

// TestMultiplexerSafeAltBindings verifies the alt+… alternatives work, since
// zellij/tmux capture ctrl+p/n/o before they reach the app.
func TestMultiplexerSafeAltBindings(t *testing.T) {
	m := testModel(t)
	m.a.Perm = agent.PermGated
	// alt+a toggles perm.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true})
	if m.a.Perm != agent.PermAuto {
		t.Fatalf("alt+a should toggle perm, got %q", m.a.Perm)
	}
	// alt+r cycles effort.
	ep := &effortProv{effort: "low"}
	m.a.Provider = ep
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}, Alt: true})
	if ep.Effort() == "low" {
		t.Fatal("alt+r should cycle effort")
	}
	// alt+m cycles model.
	m.newProvider = func(provider, mdl string) (llm.Provider, error) { return fakeProv{}, nil }
	m.modelID = llm.Models()[0].ID
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
	if m.modelID == llm.Models()[0].ID {
		t.Fatal("alt+m should cycle to a different model")
	}
}

func TestAltArrowsSelectBlocks(t *testing.T) {
	m := testModel(t)
	// Two collapsible blocks so selection has somewhere to move.
	m.push(&block{kind: blockThinking, title: "t1", collapsed: true, body: sb("a")})
	m.push(&block{kind: blockThinking, title: "t2", collapsed: true, body: sb("b")})
	m.sel = -1
	m.Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true}) // alt+up selects (enters from tail)
	if m.sel < 0 {
		t.Fatal("alt+up should move block selection")
	}
}

func TestCtrlECyclesEffort(t *testing.T) {
	m := testModel(t)
	ep := &effortProv{effort: "low"}
	m.a.Provider = ep
	// ctrl+e advances low → medium.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if ep.Effort() != "medium" {
		t.Fatalf("ctrl+e should advance low→medium, got %q", ep.Effort())
	}
	// Persists to the meta sidecar.
	if meta, ok := loadMetaForTest(t, m.sessionPath); !ok || meta.Effort != "medium" {
		t.Fatalf("ctrl+e should persist effort to meta, got %+v (ok=%v)", meta, ok)
	}
}

func TestCtrlECyclesEffortWraps(t *testing.T) {
	m := testModel(t)
	ep := &effortProv{effort: llm.EffortLevels[len(llm.EffortLevels)-1]} // highest
	m.a.Provider = ep
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if ep.Effort() != llm.EffortLevels[0] {
		t.Fatalf("ctrl+e at the top level should wrap to %q, got %q", llm.EffortLevels[0], ep.Effort())
	}
}

func TestCtrlEUnsupportedModelNotes(t *testing.T) {
	m := testModel(t)
	// fakeProv has no EffortSetter.
	before := len(m.blocks)
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if len(m.blocks) <= before {
		t.Fatal("ctrl+e on a model without effort should note that")
	}
}

func TestCtrlOCyclesModel(t *testing.T) {
	m := testModel(t)
	models := llm.Models()
	// Start on the first catalog model; ctrl+o should advance to the second.
	m.modelID = models[0].ID
	m.provName = models[0].Provider
	var gotProv, gotModel string
	m.newProvider = func(provider, model string) (llm.Provider, error) {
		gotProv, gotModel = provider, model
		return fakeProv{}, nil
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if gotModel != models[1].ID {
		t.Fatalf("ctrl+o should advance to the next model %q, got %q", models[1].ID, gotModel)
	}
	if m.modelID != models[1].ID {
		t.Fatalf("modelID should update to %q, got %q", models[1].ID, m.modelID)
	}
	// Provider is reconciled from the catalog.
	if gotProv != llm.ResolveProvider(models[1].Provider, models[1].ID) {
		t.Fatalf("provider should be reconciled, got %q", gotProv)
	}
}

func TestCtrlOCyclesModelWraps(t *testing.T) {
	m := testModel(t)
	models := llm.Models()
	last := models[len(models)-1]
	m.modelID = last.ID
	m.provName = last.Provider
	var gotModel string
	m.newProvider = func(provider, model string) (llm.Provider, error) {
		gotModel = model
		return fakeProv{}, nil
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if gotModel != models[0].ID {
		t.Fatalf("ctrl+o at the last model should wrap to %q, got %q", models[0].ID, gotModel)
	}
}

// --- skills -----------------------------------------------------------------

func TestSkillsCommandListsAndPreviews(t *testing.T) {
	dir := t.TempDir()
	sd := filepath.Join(dir, "refactor")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: refactor\ndescription: \"Safely restructure code.\"\n---\n\n# Refactor\nStep one: read.\nStep two: edit."
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m := testModel(t)
	m.skills = skill.Discover(dir)

	// Bare /skills lists the skill name + description.
	before := len(m.blocks)
	m.command("/skills")
	if len(m.blocks) <= before {
		t.Fatal("/skills should push a listing block")
	}
	last := m.blocks[len(m.blocks)-1]
	if !strings.Contains(last.body, "refactor") || !strings.Contains(last.body, "Safely restructure") {
		t.Fatalf("/skills listing missing name/description:\n%s", last.body)
	}

	// /skills <name> previews the full body in an expanded block.
	m.command("/skills refactor")
	prev := m.blocks[len(m.blocks)-1]
	if prev.kind != blockThinking || prev.collapsed {
		t.Fatalf("/skills <name> should open an expanded preview block: %+v", prev)
	}
	if !strings.Contains(prev.body, "Step one: read.") {
		t.Fatalf("preview should contain the skill body:\n%s", prev.body)
	}

	// Unknown skill reports an error.
	m.command("/skills nope")
	if errb := m.blocks[len(m.blocks)-1]; !errb.isErr {
		t.Fatal("/skills <unknown> should report an error")
	}
}

func TestSkillsCommandNoneDiscovered(t *testing.T) {
	m := testModel(t)
	m.skills = skill.Discover(t.TempDir()) // empty dir → no skills
	before := len(m.blocks)
	m.command("/skills")
	if len(m.blocks) <= before {
		t.Fatal("/skills with no skills should note that")
	}
	if !strings.Contains(m.blocks[len(m.blocks)-1].body, "no skills") {
		t.Fatalf("expected a 'no skills' note, got %q", m.blocks[len(m.blocks)-1].body)
	}
}

func TestModelCommand(t *testing.T) {
	m := testModel(t)
	// Bare /model now opens the interactive model picker (no note block).
	m.command("/model")
	if !m.modelPicking {
		t.Fatal("/model should open the model picker")
	}
	if len(m.modelPicks) != len(llm.Models()) {
		t.Fatalf("/model picker should list all catalog models, got %d", len(m.modelPicks))
	}
	// esc closes the picker without switching.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.modelPicking {
		t.Fatal("esc should close the model picker")
	}
}

// --- edit diff rendering ----------------------------------------------------

func TestEditBlockRendersDiff(t *testing.T) {
	b := &block{
		kind:     blockTool,
		toolName: "edit",
		toolArgs: json.RawMessage(`{"path":"a.go","old_string":"foo","new_string":"bar"}`),
		result:   "edited a.go (1 replacement(s))",
	}
	out := b.render(false) // expanded
	if !strings.Contains(out, "- foo") {
		t.Fatalf("edit diff should show removed line:\n%s", out)
	}
	if !strings.Contains(out, "+ bar") {
		t.Fatalf("edit diff should show added line:\n%s", out)
	}
}

func TestEditBlockCollapsedPreviewIsPlain(t *testing.T) {
	b := &block{
		kind:      blockTool,
		toolName:  "edit",
		collapsed: true,
		toolArgs:  json.RawMessage(`{"path":"a.go","old_string":"foo\nbaz","new_string":"bar"}`),
	}
	// previewLine must not panic or leak multiple lines on the diff text.
	out := b.render(false)
	if strings.Count(out, "\n") > 0 {
		t.Fatalf("collapsed edit block should be a single line:\n%q", out)
	}
}

// --- live plan panel --------------------------------------------------------

func TestTodoToolDrivesPlanPanel(t *testing.T) {
	m := testModel(t)
	args := json.RawMessage(`{"todos":[
		{"content":"design","status":"completed"},
		{"content":"build","status":"in_progress"},
		{"content":"test","status":"pending"}]}`)
	before := len(m.blocks)
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "todo", ToolArgs: args}})
	if len(m.todos) != 3 {
		t.Fatalf("plan should have 3 items, got %d", len(m.todos))
	}
	if len(m.blocks) != before {
		t.Fatal("todo tool should NOT create a transcript block (it drives the panel)")
	}
	if m.topHeight() != 1+3 {
		t.Fatalf("topHeight should be plan header+3 tasks, got %d", m.topHeight())
	}
	view := m.planView()
	for _, want := range []string{"plan (1/3)", "design", "build", "test"} {
		if !strings.Contains(view, want) {
			t.Fatalf("plan view missing %q:\n%s", want, view)
		}
	}
	// A todo result event must not create a block either.
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolResult, ToolName: "todo", Result: "ok"}})
	if len(m.blocks) != before {
		t.Fatal("todo result should not create a block")
	}
}

func TestEmptyPlanHasNoPanel(t *testing.T) {
	m := testModel(t)
	if m.planView() != "" {
		t.Fatal("no todos should mean no plan panel")
	}
	// The status bar now lives at the bottom; with no todos the top is empty.
	if m.topHeight() != 0 {
		t.Fatalf("topHeight with no todos should be 0 (status bar moved to bottom), got %d", m.topHeight())
	}
}

// --- compact + rate-limit ----------------------------------------------------

func TestCompactCommandRunsAndReports(t *testing.T) {
	m := testModel(t)
	cmd := m.command("/compact")
	if cmd == nil {
		t.Fatal("/compact should return a command")
	}
	if m.state != stRunning || m.status != "compacting…" {
		t.Fatalf("/compact should show a compacting state, got state=%v status=%q", m.state, m.status)
	}
	before := len(m.blocks)
	m.Update(compactDoneMsg{before: 10, after: 3, beforeTok: 50000, afterTok: 8000})
	if m.state != stInput {
		t.Fatal("compactDone should return to input state")
	}
	if len(m.blocks) <= before {
		t.Fatal("compactDone should push a result note")
	}
	if !strings.Contains(m.blocks[len(m.blocks)-1].body, "compacted") {
		t.Fatalf("compact note should report the result, got %q", m.blocks[len(m.blocks)-1].body)
	}
}

func TestCompactNoopNote(t *testing.T) {
	m := testModel(t)
	m.Update(compactDoneMsg{before: 2, after: 2})
	if !strings.Contains(m.blocks[len(m.blocks)-1].body, "nothing to compact") {
		t.Fatalf("no-op compact should note nothing to compact, got %q", m.blocks[len(m.blocks)-1].body)
	}
}

func TestRateLimitHint(t *testing.T) {
	m := testModel(t)
	m.Update(turnDoneMsg{err: fmt.Errorf("converse: HTTP 429: too many tokens")})
	hinted := false
	for _, b := range m.blocks {
		if strings.Contains(b.body, "/compact") && strings.Contains(b.body, "rate-limited") {
			hinted = true
		}
	}
	if !hinted {
		t.Fatal("a 429/too-many-tokens error should add a /compact hint")
	}
}

func TestIsRateLimit(t *testing.T) {
	cases := map[string]bool{
		"HTTP 429: too many tokens":     true,
		"converse: throttlingException": true,
		"Too Many Requests":             true,
		"context deadline exceeded":     false,
		"HTTP 400: bad request":         false,
	}
	for msg, want := range cases {
		if got := isRateLimit(fmt.Errorf("%s", msg)); got != want {
			t.Errorf("isRateLimit(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestStatusBarShowsContext(t *testing.T) {
	m := testModel(t)
	bar := m.statusBarView()
	if !strings.Contains(bar, "eigen") || !strings.Contains(bar, "perm=") {
		t.Fatalf("status bar missing model/perm: %q", bar)
	}
}

// TestBlocksHaveBlankSeparators verifies the transcript inserts a blank line
// between consecutive blocks so they don't run together visually. The blank
// line is tracked in plainLines (used by click-mapping + drag-selection) so the
// row for each block's first line stays aligned with the rendered viewport.
func TestBlocksHaveBlankSeparators(t *testing.T) {
	m := testModel(t)
	m.text("user", "first")
	m.text("assistant", "second")
	m.text("user", "third")
	// Each block (after the first) is preceded by exactly one blank line.
	for i := 1; i < len(m.blocks); i++ {
		sep := m.blockStart[i] - 1
		if sep < 0 || sep >= len(m.plainLines) {
			t.Fatalf("block %d has no room for a separator (start=%d)", i, m.blockStart[i])
		}
		if strings.TrimSpace(m.plainLines[sep]) != "" {
			t.Fatalf("expected blank separator before block %d, got %q", i, m.plainLines[sep])
		}
	}
	// The first block starts at line 0 (no leading separator).
	if m.blockStart[0] != 0 {
		t.Fatalf("first block should start at line 0, got %d", m.blockStart[0])
	}
}

// --- read-aloud -------------------------------------------------------------

type fakeSpeaker struct {
	spoken  []string
	avail   bool
	stopped int
}

func (f *fakeSpeaker) Speak(t string)  { f.spoken = append(f.spoken, t) }
func (f *fakeSpeaker) Available() bool { return f.avail }
func (f *fakeSpeaker) Stop()           { f.stopped++ }

func TestReadAloudSpeaksFinalAnswer(t *testing.T) {
	m := testModel(t)
	fs := &fakeSpeaker{avail: true}
	m.speaker = fs
	m.command("/read")
	if !m.readAloud {
		t.Fatal("/read should enable read-aloud when TTS is available")
	}
	m.text("assistant", "the final answer")
	m.Update(turnDoneMsg{})
	if len(fs.spoken) != 1 || fs.spoken[0] != "the final answer" {
		t.Fatalf("should speak the final answer, got %v", fs.spoken)
	}
}

func TestReadAloudRequiresTTS(t *testing.T) {
	m := testModel(t)
	m.speaker = &fakeSpeaker{avail: false}
	m.command("/read")
	if m.readAloud {
		t.Fatal("/read without an available TTS command should not enable")
	}
}

func TestReadAloudOffStaysSilent(t *testing.T) {
	m := testModel(t)
	fs := &fakeSpeaker{avail: true}
	m.speaker = fs
	m.text("assistant", "x")
	m.Update(turnDoneMsg{}) // readAloud is false
	if len(fs.spoken) != 0 {
		t.Fatalf("should not speak when read-aloud is off, got %v", fs.spoken)
	}
}

func TestReadAloudNotSpokenOnError(t *testing.T) {
	m := testModel(t)
	fs := &fakeSpeaker{avail: true}
	m.speaker = fs
	m.readAloud = true
	m.text("assistant", "partial")
	m.Update(turnDoneMsg{err: context.Canceled})
	if len(fs.spoken) != 0 {
		t.Fatalf("should not speak on a failed turn, got %v", fs.spoken)
	}
}

// --- live model switch ------------------------------------------------------

func TestModelSwitchReplacesProvider(t *testing.T) {
	m := testModel(t)
	m.provName = "mantle"
	m.newProvider = func(provider, model string) (llm.Provider, error) {
		return fakeProv{text: provider + "/" + model}, nil
	}
	m.command("/model new-model-id")
	if m.modelID != "new-model-id" {
		t.Fatalf("modelID should update, got %q", m.modelID)
	}
	if m.a.Provider.Name() != "fake" {
		t.Fatal("provider should be swapped")
	}
	if m.a.Compactor == nil {
		t.Fatal("compactor should be recreated for the new provider")
	}
}

func TestModelSwitchProviderAndId(t *testing.T) {
	m := testModel(t)
	var gotProv, gotModel string
	m.newProvider = func(provider, model string) (llm.Provider, error) {
		gotProv, gotModel = provider, model
		return fakeProv{}, nil
	}
	m.command("/model converse claude-x")
	if gotProv != "converse" || gotModel != "claude-x" {
		t.Fatalf("should parse provider+id, got %q %q", gotProv, gotModel)
	}
	if m.provName != "converse" {
		t.Fatal("provName should update")
	}
}

func TestModelSwitchInfersProviderFromCatalog(t *testing.T) {
	m := testModel(t)
	m.provName = "mantle" // currently on mantle
	var gotProv, gotModel string
	m.newProvider = func(provider, model string) (llm.Provider, error) {
		gotProv, gotModel = provider, model
		return fakeProv{}, nil
	}
	// Picking a converse model by id alone must infer the converse provider
	// from the catalog — not keep the current mantle provider.
	m.command("/model us.anthropic.claude-sonnet-4-6")
	if gotProv != "converse" {
		t.Fatalf("provider should be inferred from the catalog, got %q", gotProv)
	}
	if gotModel != "us.anthropic.claude-sonnet-4-6" {
		t.Fatalf("model id wrong, got %q", gotModel)
	}
	if m.provName != "converse" {
		t.Fatal("provName should update to the inferred provider")
	}
}

func TestModelSwitchErrorKeepsProvider(t *testing.T) {
	m := testModel(t)
	orig := m.a.Provider
	m.newProvider = func(provider, model string) (llm.Provider, error) {
		return nil, context.DeadlineExceeded
	}
	m.command("/model bad")
	if m.a.Provider != orig {
		t.Fatal("a failed switch must keep the existing provider")
	}
	last := m.blocks[len(m.blocks)-1]
	if !last.isErr {
		t.Fatal("a failed switch should report an error")
	}
}

func TestModelSwitchUnavailableWithoutConstructor(t *testing.T) {
	m := testModel(t)
	m.newProvider = nil
	m.command("/model x")
	last := m.blocks[len(m.blocks)-1]
	if !last.isErr {
		t.Fatal("no constructor should report switching unavailable")
	}
}

// effortProv is a provider that supports the EffortSetter interface.
type effortProv struct{ effort string }

func (effortProv) Name() string { return "effort-prov" }
func (effortProv) Complete(context.Context, llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "ok"}, nil
}
func (p *effortProv) SetEffort(level string) bool {
	if !llm.ValidEffort(level) {
		return false
	}
	p.effort = level
	return true
}
func (p *effortProv) Effort() string { return p.effort }

func TestEffortCommand(t *testing.T) {
	m := testModel(t)
	ep := &effortProv{effort: "high"}
	m.a.Provider = ep

	// Bare /effort reports the current level.
	before := len(m.blocks)
	m.command("/effort")
	if len(m.blocks) <= before {
		t.Fatal("/effort should report the current effort")
	}
	// Set a valid level.
	m.command("/effort medium")
	if ep.Effort() != "medium" {
		t.Fatalf("/effort medium should change effort, got %q", ep.Effort())
	}
	// Invalid level errors.
	m.command("/effort bogus")
	if last := m.blocks[len(m.blocks)-1]; !last.isErr {
		t.Fatal("/effort bogus should report an error")
	}
}

func TestEffortUnsupportedModel(t *testing.T) {
	m := testModel(t)
	// fakeProv does not implement EffortSetter.
	before := len(m.blocks)
	m.command("/effort high")
	if len(m.blocks) <= before {
		t.Fatal("/effort on an unsupported model should note that")
	}
}

// searchProv supports the Searcher interface (grok-style live search).
type searchProv struct{ mode string }

func (searchProv) Name() string { return "search-prov" }
func (searchProv) Complete(context.Context, llm.Request) (*llm.Response, error) {
	return &llm.Response{Text: "ok"}, nil
}
func (p *searchProv) SetSearch(mode string) bool {
	switch mode {
	case "off", "auto", "on":
		p.mode = mode
		return true
	default:
		return false
	}
}
func (p *searchProv) SearchMode() string { return p.mode }

func TestSearchCommand(t *testing.T) {
	m := testModel(t)
	sp := &searchProv{mode: "off"}
	m.a.Provider = sp

	before := len(m.blocks)
	m.command("/search")
	if len(m.blocks) <= before {
		t.Fatal("/search should report the current mode")
	}
	m.command("/search on")
	if sp.SearchMode() != "on" {
		t.Fatalf("/search on should enable, got %q", sp.SearchMode())
	}
	m.command("/search bogus")
	if last := m.blocks[len(m.blocks)-1]; !last.isErr {
		t.Fatal("/search bogus should report an error")
	}
}

func TestSearchUnsupportedModel(t *testing.T) {
	m := testModel(t)
	before := len(m.blocks)
	m.command("/search on") // fakeProv has no Searcher
	if len(m.blocks) <= before {
		t.Fatal("/search on an unsupported model should note that")
	}
}

// --- /find ------------------------------------------------------------------

func TestFindSelectsMatch(t *testing.T) {
	m := testModel(t)
	m.text("user", "first message")
	m.text("assistant", "the special token is xyzzy")
	m.text("user", "later message")
	m.command("/find xyzzy")
	// sel should point at the assistant block containing xyzzy (index 1).
	if m.sel != 1 {
		t.Fatalf("/find should select the matching block, got sel=%d", m.sel)
	}
}

func TestFindNoMatch(t *testing.T) {
	m := testModel(t)
	m.text("assistant", "hello")
	before := m.sel
	m.command("/find notpresent")
	if m.sel != before {
		t.Fatal("/find with no match should not move the selection")
	}
}

func TestFindUsageWhenEmpty(t *testing.T) {
	m := testModel(t)
	before := len(m.blocks)
	m.command("/find")
	if len(m.blocks) <= before {
		t.Fatal("/find with no arg should print usage")
	}
}

// --- /copy ------------------------------------------------------------------

type fakeClip struct {
	copied   string
	pasted   string // text Paste() returns
	avail    bool
	canPaste bool
}

func (f *fakeClip) Copy(text string) error { f.copied = text; return nil }
func (f *fakeClip) Available() bool        { return f.avail }
func (f *fakeClip) CanPaste() bool         { return f.canPaste }
func (f *fakeClip) Paste() (string, error) { return f.pasted, nil }

func TestCopySelectedBlock(t *testing.T) {
	m := testModel(t)
	fc := &fakeClip{avail: true}
	m.clip = fc
	m.text("user", "q")
	m.text("assistant", "the answer to copy")
	m.sel = 1 // the assistant block
	m.command("/copy")
	if fc.copied != "the answer to copy" {
		t.Fatalf("should copy the selected block, got %q", fc.copied)
	}
}

func TestCopyFallsBackToLastAnswer(t *testing.T) {
	m := testModel(t)
	fc := &fakeClip{avail: true}
	m.clip = fc
	m.text("assistant", "final answer")
	m.sel = -1 // nothing selected
	m.command("/copy")
	if fc.copied != "final answer" {
		t.Fatalf("should copy the last assistant message, got %q", fc.copied)
	}
}

func TestCopyRequiresClipboard(t *testing.T) {
	m := testModel(t)
	m.clip = &fakeClip{avail: false}
	m.text("assistant", "x")
	m.command("/copy")
	last := m.blocks[len(m.blocks)-1]
	if !last.isErr {
		t.Fatal("/copy without a clipboard command should report an error")
	}
}

// --- idle dreaming ----------------------------------------------------------

func TestIdleTickIgnoresStaleGeneration(t *testing.T) {
	m := testModel(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	mem, _ := memory.Open("/proj")
	m.mem = mem
	m.dreamOnIdle = true
	m.idleGen = 3
	// A tick from an older generation must be ignored (no dream cmd).
	_, cmd := m.Update(idleTickMsg{gen: 2})
	if cmd != nil {
		t.Fatal("stale idle tick should be ignored")
	}
}

func TestIdleTickRequiresEnabled(t *testing.T) {
	m := testModel(t)
	mem, _ := memory.Open(t.TempDir())
	m.mem = mem
	m.dreamOnIdle = false
	_, cmd := m.Update(idleTickMsg{gen: m.idleGen})
	if cmd != nil {
		t.Fatal("idle dreaming disabled should never fire")
	}
}

func TestSubmitBumpsIdleGen(t *testing.T) {
	m := testModel(t)
	before := m.idleGen
	m.submit("do something")
	if m.idleGen == before {
		t.Fatal("submitting a turn should invalidate pending idle timers")
	}
}

func TestDreamDoneAppendsToMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := testModel(t)
	mem, _ := memory.Open("/proj")
	m.mem = mem
	m.Update(dreamDoneMsg{notes: []string{"build with go build", "tests via go test"}})
	got := mem.Read()
	if !strings.Contains(got, "go build") || !strings.Contains(got, "go test") {
		t.Fatalf("dream notes should be appended to memory:\n%s", got)
	}
}

func TestScheduleIdleDreamNilWhenDisabled(t *testing.T) {
	m := testModel(t)
	m.dreamOnIdle = false
	if m.scheduleIdleDream() != nil {
		t.Fatal("disabled idle dreaming should schedule nothing")
	}
}

// --- approval scope memory --------------------------------------------------

func TestApprovalAlwaysAllow(t *testing.T) {
	m := testModel(t)
	reply := make(chan bool, 1)
	m.Update(approvalMsg{name: "bash", args: json.RawMessage(`{"command":"ls"}`), reply: reply})
	if m.pending == nil {
		t.Fatal("first bash call should prompt")
	}
	// Press 'a' = always allow.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if got := <-reply; !got {
		t.Fatal("'a' should approve")
	}
	if !m.approvedTools["bash"] {
		t.Fatal("'a' should remember the tool for the session")
	}
	// A second bash call must auto-approve without prompting.
	reply2 := make(chan bool, 1)
	m.Update(approvalMsg{name: "bash", args: json.RawMessage(`{"command":"pwd"}`), reply: reply2})
	if m.pending != nil {
		t.Fatal("an always-allowed tool should not prompt again")
	}
	select {
	case got := <-reply2:
		if !got {
			t.Fatal("auto-approval should reply true")
		}
	default:
		t.Fatal("auto-approval should have replied immediately")
	}
}

func TestApprovalAlwaysAllowIsPerTool(t *testing.T) {
	m := testModel(t)
	m.approvedTools = map[string]bool{"bash": true}
	reply := make(chan bool, 1)
	// A different tool must still prompt.
	m.Update(approvalMsg{name: "write", args: json.RawMessage(`{}`), reply: reply})
	if m.pending == nil {
		t.Fatal("a tool that was not always-allowed should still prompt")
	}
}

// --- /export ----------------------------------------------------------------

func TestExportWritesMarkdown(t *testing.T) {
	m := testModel(t)
	m.session = m.a.Resume([]llm.Message{
		{Role: llm.RoleUser, Text: "add a feature"},
		{Role: llm.RoleAssistant, Text: "here is the plan"},
	})
	path := filepath.Join(t.TempDir(), "out.md")
	m.command("/export " + path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "## You") || !strings.Contains(s, "add a feature") || !strings.Contains(s, "## eigen") {
		t.Fatalf("markdown export missing content:\n%s", s)
	}
}

func TestSessionMarkdownRendersToolCalls(t *testing.T) {
	md := sessionMarkdown([]llm.Message{
		{Role: llm.RoleAssistant, Text: "running", ToolCalls: []llm.ToolCall{{Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)}}},
		{Role: llm.RoleTool, Text: "file1\nfile2"},
	})
	if !strings.Contains(md, "tool `bash`") || !strings.Contains(md, "```") {
		t.Fatalf("markdown should render tool calls + results:\n%s", md)
	}
}

// --- concurrency safety -----------------------------------------------------

type slowProv struct{ delay time.Duration }

func (slowProv) Name() string { return "slow" }
func (p slowProv) Complete(ctx context.Context, _ llm.Request) (*llm.Response, error) {
	select {
	case <-time.After(p.delay):
	case <-ctx.Done():
	}
	return &llm.Response{Text: "done"}, nil
}

// TestNoRaceRenderingDuringTurn renders the View repeatedly while the agent
// goroutine appends to the session — the status bar must not read the live
// session slice. Run with -race to catch regressions.
func TestNoRaceRenderingDuringTurn(t *testing.T) {
	m := testModel(t)
	reg, _ := tool.NewRegistry()
	m.a = &agent.Agent{Provider: slowProv{delay: 30 * time.Millisecond}, Tools: reg, Perm: agent.PermAuto}
	m.session = m.a.NewSession()

	done := make(chan struct{})
	go func() {
		_, _ = m.session.Send(context.Background(), "do work")
		close(done)
	}()
	for {
		select {
		case <-done:
			return
		default:
			_ = m.View() // exercises statusBarView -> ctxIndicator
			_ = m.statusBarView()
		}
	}
}

// --- non-streaming / EventDone rendering ------------------------------------

func TestEventDoneRendersAnswerWhenNotStreamed(t *testing.T) {
	m := testModel(t)
	// A provider that doesn't stream text: only EventDone carries the answer.
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventDone, Text: "the final answer"}})
	var asst *block
	for _, b := range m.blocks {
		if b.kind == blockText && b.role == "assistant" {
			asst = b
		}
	}
	if asst == nil || asst.body != "the final answer" {
		t.Fatalf("EventDone must render the final answer when not streamed; blocks=%d", len(m.blocks))
	}
}

func TestEventDoneDoesNotDuplicateStreamedText(t *testing.T) {
	m := testModel(t)
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "streamed "}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "answer"}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventDone, Text: "streamed answer"}})
	n := 0
	for _, b := range m.blocks {
		if b.kind == blockText && b.role == "assistant" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("EventDone should not duplicate already-streamed text, got %d assistant blocks", n)
	}
}

// --- input history ----------------------------------------------------------

func TestInputHistoryRecall(t *testing.T) {
	m := testModel(t)
	typeRunes(m, "first command")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	typeRunes(m, "second command")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// Type a partial live draft, then browse up.
	typeRunes(m, "draft")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.ti.Value() != "second command" {
		t.Fatalf("first up should recall newest history, got %q", m.ti.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.ti.Value() != "first command" {
		t.Fatalf("second up should recall older, got %q", m.ti.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.ti.Value() != "second command" {
		t.Fatalf("down should move toward newer, got %q", m.ti.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.ti.Value() != "draft" {
		t.Fatalf("down past the end should restore the live draft, got %q", m.ti.Value())
	}
}

func TestInputHistoryDedupesConsecutive(t *testing.T) {
	m := testModel(t)
	for i := 0; i < 3; i++ {
		typeRunes(m, "same")
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if len(m.history) != 1 {
		t.Fatalf("consecutive duplicates should collapse, got %d", len(m.history))
	}
}

func TestCtrlPSelectsBlocks(t *testing.T) {
	m := testModel(t)
	m.push(&block{kind: blockTool, title: "bash", collapsed: true})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.sel < 0 {
		t.Fatal("ctrl+p should select a collapsible block")
	}
}

func TestCtrlYCopies(t *testing.T) {
	m := testModel(t)
	fc := &fakeClip{avail: true}
	m.clip = fc
	m.text("assistant", "answer to copy")
	m.sel = 0
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if fc.copied != "answer to copy" {
		t.Fatalf("ctrl+y should copy the selected block, got %q", fc.copied)
	}
}

// --- streamed reasoning (thinking) ------------------------------------------

func TestReasoningStreamsExpandedThenCollapses(t *testing.T) {
	m := testModel(t)
	// Reasoning deltas arrive: a thinking block appears, expanded and live.
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventReasoningDelta, Text: "let me "}})
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventReasoningDelta, Text: "think"}})
	var tb *block
	for _, b := range m.blocks {
		if b.kind == blockThinking {
			tb = b
		}
	}
	if tb == nil {
		t.Fatal("reasoning deltas should create a thinking block")
	}
	if tb.collapsed {
		t.Fatal("thinking block should be expanded while reasoning streams")
	}
	if tb.body != "let me think" {
		t.Fatalf("reasoning deltas should accumulate, got %q", tb.body)
	}
	// Real text output arrives → the thinking block collapses.
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventTextDelta, Text: "the answer"}})
	if !tb.collapsed {
		t.Fatal("thinking block should collapse once the answer starts streaming")
	}
}

func TestReasoningCollapsesOnToolStart(t *testing.T) {
	m := testModel(t)
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventReasoningDelta, Text: "plan"}})
	var tb *block
	for _, b := range m.blocks {
		if b.kind == blockThinking {
			tb = b
		}
	}
	if tb == nil || tb.collapsed {
		t.Fatal("thinking should start expanded")
	}
	m.Update(agentEvent{e: agent.Event{Kind: agent.EventToolStart, ToolName: "read", ToolArgs: json.RawMessage(`{"path":"x"}`)}})
	if !tb.collapsed {
		t.Fatal("thinking should collapse when a tool starts")
	}
}

// --- overload failover ---------------------------------------------------

func TestIsOverloaded(t *testing.T) {
	cases := map[string]bool{
		"converse: failed after 5 attempts: HTTP 503: Bedrock is unable to process your request": true,
		"HTTP 503 Service Unavailable": true,
		"model overloaded":             true,
		"HTTP 429: too many tokens":    false,
		"context canceled":             false,
	}
	for msg, want := range cases {
		if got := isOverloaded(fmt.Errorf("%s", msg)); got != want {
			t.Errorf("isOverloaded(%q) = %v, want %v", msg, got, want)
		}
	}
}

func TestOverloadFailoverRedirectsAndRetries(t *testing.T) {
	m := testModel(t)
	m.provName, m.modelID = "converse", "global.anthropic.claude-fable-5"
	m.newProvider = func(provider, mdl string) (llm.Provider, error) {
		return fakeProv{text: provider + "/" + mdl}, nil
	}
	// Seed history as a failed turn leaves it.
	m.session = m.a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "task"}})

	_, cmd := m.Update(turnDoneMsg{err: fmt.Errorf("converse: failed after 5 attempts: HTTP 503: unable to process")})
	if m.modelID != failoverModelID {
		t.Fatalf("failover should switch to %s, got %s", failoverModelID, m.modelID)
	}
	if m.failoverFrom == nil || m.failoverFrom.model != "global.anthropic.claude-fable-5" {
		t.Fatalf("failover should remember the origin, got %+v", m.failoverFrom)
	}
	if m.failoverLeft != failoverTurns {
		t.Fatalf("failover window should be %d, got %d", failoverTurns, m.failoverLeft)
	}
	if cmd == nil {
		t.Fatal("failover should auto-retry the failed turn (resend cmd)")
	}
	if m.state != stRunning {
		t.Fatal("retry should put the UI back into running state")
	}
}

func TestOverloadFailoverCountsDownAndSwitchesBack(t *testing.T) {
	m := testModel(t)
	m.provName, m.modelID = "converse", failoverModelID
	m.failoverFrom = &failoverOrigin{provider: "converse", model: "global.anthropic.claude-fable-5"}
	m.failoverLeft = 2
	m.newProvider = func(provider, mdl string) (llm.Provider, error) {
		return fakeProv{text: provider + "/" + mdl}, nil
	}
	// One successful turn: counts down, stays on fallback.
	m.Update(turnDoneMsg{})
	if m.failoverLeft != 1 || m.failoverFrom == nil {
		t.Fatalf("after 1 ok turn: left=%d from=%v", m.failoverLeft, m.failoverFrom)
	}
	// Second successful turn: window over, switches back to the origin.
	m.Update(turnDoneMsg{})
	if m.failoverFrom != nil {
		t.Fatal("failover window should be over")
	}
	if m.modelID != "global.anthropic.claude-fable-5" {
		t.Fatalf("should switch back to the origin model, got %s", m.modelID)
	}
}

func TestOverloadOnFallbackModelNoLoop(t *testing.T) {
	m := testModel(t)
	m.provName, m.modelID = "converse", failoverModelID // already on the fallback
	m.newProvider = func(provider, mdl string) (llm.Provider, error) {
		return fakeProv{}, nil
	}
	_, cmd := m.Update(turnDoneMsg{err: fmt.Errorf("HTTP 503: unable to process")})
	if m.failoverFrom != nil {
		t.Fatal("must not fail over when already on the fallback model")
	}
	if cmd != nil {
		// cmd may be textarea.Blink etc; the important part is state is input,
		// not a resend loop.
		if m.state == stRunning {
			t.Fatal("must not auto-retry in a loop on the fallback model")
		}
	}
}

func TestManualModelSwitchClearsFailover(t *testing.T) {
	m := testModel(t)
	m.provName, m.modelID = "converse", failoverModelID
	m.failoverFrom = &failoverOrigin{provider: "converse", model: "global.anthropic.claude-fable-5"}
	m.failoverLeft = 3
	m.newProvider = func(provider, mdl string) (llm.Provider, error) {
		return fakeProv{}, nil
	}
	m.command("/model glm-4.6")
	if m.failoverFrom != nil || m.failoverLeft != 0 {
		t.Fatal("a manual /model switch should clear the failover window")
	}
}

func TestSaveMetaDuringFailoverKeepsOriginalModel(t *testing.T) {
	m := testModel(t)
	m.provName, m.modelID = "converse", failoverModelID
	m.failoverFrom = &failoverOrigin{provider: "converse", model: "global.anthropic.claude-fable-5"}
	m.saveMeta()
	meta, ok := loadMetaForTest(t, m.sessionPath)
	if !ok {
		t.Fatal("meta not saved")
	}
	if meta.Model != "global.anthropic.claude-fable-5" {
		t.Fatalf("meta during failover should keep the original model, got %s", meta.Model)
	}
}

// --- status bar wrap, input wrap math, click-to-cursor, right-click paste ---

func TestStatusBarWrapsWhenNarrow(t *testing.T) {
	m := testModel(t)
	m.width = 24 // force overflow with the usual parts
	lines := m.statusBarLines()
	if len(lines) != 2 {
		t.Fatalf("narrow status bar should wrap to 2 lines, got %d: %v", len(lines), lines)
	}
	for i, ln := range lines {
		if w := ansi.StringWidth(ln); w > m.width {
			t.Fatalf("status line %d width %d exceeds %d", i, w, m.width)
		}
	}
}

func TestStatusBarSingleLineWhenWide(t *testing.T) {
	m := testModel(t)
	m.width = 200
	if h := m.statusBarHeight(); h != 1 {
		t.Fatalf("wide status bar should be 1 line, got %d", h)
	}
}

func TestWrappedRowCountWordWrap(t *testing.T) {
	// 10-wide: "aaaa bbbb cccc" → "aaaa bbbb"(9) then "cccc" → 2 rows.
	if got := wrappedRowCount("aaaa bbbb cccc", 10); got != 2 {
		t.Fatalf("word-wrap row count = %d, want 2", got)
	}
	// A single short line is 1 row.
	if got := wrappedRowCount("hello", 10); got != 1 {
		t.Fatalf("short line row count = %d, want 1", got)
	}
	// A word longer than the width hard-splits.
	if got := wrappedRowCount("aaaaaaaaaaaaa", 5); got < 3 {
		t.Fatalf("long word should hard-wrap to >=3 rows, got %d", got)
	}
}

func TestRightClickPastes(t *testing.T) {
	m := testModel(t)
	m.clip = &fakeClip{avail: true, canPaste: true, pasted: "pasted text"}
	m.Update(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if got := m.ti.Value(); !strings.Contains(got, "pasted text") {
		t.Fatalf("right-click should paste into input, got %q", got)
	}
}

func TestRightClickPasteNoBackend(t *testing.T) {
	m := testModel(t)
	m.clip = &fakeClip{avail: true, canPaste: false}
	before := len(m.blocks)
	m.Update(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if len(m.blocks) <= before {
		t.Fatal("paste with no backend should note the missing command")
	}
}

func TestClickInInputPositionsCursor(t *testing.T) {
	m := testModel(t)
	m.ti.Prompt = "│ "
	typeRunes(m, "hello world")
	// The input top row in this headless layout:
	top := m.inputTopRow()
	// Click on the first text row (top+1), column 3 (after prompt).
	vrow, col, ok := m.clickInInput(5, top+1)
	if !ok {
		t.Fatalf("click on input text row should be detected (top=%d)", top)
	}
	m.positionCursorAt(vrow, col)
	// Cursor should now be within the line (not at the very end necessarily).
	if m.ti.Line() != 0 {
		t.Fatalf("cursor should stay on logical line 0, got %d", m.ti.Line())
	}
}

// TestSlashCommandRunsWhileRunning verifies a settings slash command typed
// mid-turn is executed immediately, not queued as a prompt to the model.
func TestSlashCommandRunsWhileRunning(t *testing.T) {
	m := testModel(t)
	ep := &effortProv{effort: "low"}
	m.a.Provider = ep
	m.state = stRunning
	typeRunes(m, "/effort high")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if ep.Effort() != "high" {
		t.Fatalf("/effort high mid-turn should set effort, got %q", ep.Effort())
	}
	if len(m.queued) != 0 {
		t.Fatalf("a slash command must not be queued as a prompt, queued=%v", m.queued)
	}
}

// TestUnsafeSlashCommandRefusedWhileRunning verifies session-mutating commands
// are refused mid-turn rather than racing the agent goroutine.
func TestUnsafeSlashCommandRefusedWhileRunning(t *testing.T) {
	m := testModel(t)
	m.push(&block{kind: blockText, role: "assistant", body: sb("hi")})
	before := len(m.session.Messages())
	m.state = stRunning
	typeRunes(m, "/clear")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.queued) != 0 {
		t.Fatal("/clear should not be queued as a prompt")
	}
	// Session not cleared mid-turn.
	if got := len(m.session.Messages()); got != before {
		t.Fatalf("/clear must be refused mid-turn (session changed: %d→%d)", before, got)
	}
}

func TestSafeWhileRunning(t *testing.T) {
	safe := []string{"/effort", "/perm", "/model", "/search", "/find", "/read", "/copy", "/tools", "/skills", "/help"}
	unsafe := []string{"/clear", "/compact", "/resume", "/rebuild", "/save", "/export", "/quit", "/exit"}
	for _, c := range safe {
		if !safeWhileRunning(c) {
			t.Errorf("%s should be safe while running", c)
		}
	}
	for _, c := range unsafe {
		if safeWhileRunning(c) {
			t.Errorf("%s should NOT be safe while running", c)
		}
	}
}

func TestRefreshCtxProactiveNudgeFiresOnce(t *testing.T) {
	m := testModel(t)
	m.a.MaxContextTokens = 10000
	// Seed a session whose estimate is ~85% of the budget.
	big := strings.Repeat("word ", 7000) // ~8750 tokens (>80% of 10k)
	m.session = m.a.Resume([]llm.Message{{Role: llm.RoleUser, Text: big}})

	m.refreshCtx()
	if !m.ctxNudged {
		t.Fatal("nudge flag should be set once over the threshold")
	}
	notes := 0
	for _, b := range m.blocks {
		if b.kind == blockNote && strings.Contains(b.body, "context ~") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("want exactly 1 context nudge, got %d", notes)
	}

	// A second refresh while still over the threshold must NOT re-nudge.
	m.refreshCtx()
	notes = 0
	for _, b := range m.blocks {
		if b.kind == blockNote && strings.Contains(b.body, "context ~") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("nudge should fire only once per fill cycle, got %d", notes)
	}

	// Falling back under the threshold re-arms it.
	m.session = m.a.Resume([]llm.Message{{Role: llm.RoleUser, Text: "tiny"}})
	m.refreshCtx()
	if m.ctxNudged {
		t.Fatal("nudge flag should re-arm when usage drops below the threshold")
	}
}

func TestGoalCommand(t *testing.T) {
	m := testModel(t)
	m.command("/goal ship the v2 API")
	if got := m.a.CurrentGoal(); got != "ship the v2 API" {
		t.Fatalf("goal not set, got %q", got)
	}
	found := false
	for _, b := range m.blocks {
		if b.kind == blockNote && strings.Contains(b.body, "goal → ship the v2 API") {
			found = true
		}
	}
	if !found {
		t.Fatal("setting a goal should note it")
	}

	// Bare /goal shows it.
	m.command("/goal")
	found = false
	for _, b := range m.blocks {
		if b.kind == blockNote && strings.Contains(b.body, "goal: ship the v2 API") {
			found = true
		}
	}
	if !found {
		t.Fatal("bare /goal should show the current goal")
	}

	// Clear.
	m.command("/goal clear")
	if m.a.CurrentGoal() != "" {
		t.Fatal("/goal clear should unset the goal")
	}
}

func TestPingOnTurnDoneOnlyAfterLongTurns(t *testing.T) {
	m := testModel(t)
	// Short turn: no ping. We can't capture the bell easily, but we can verify
	// the notifier command path via a script that records its invocation.
	dir := t.TempDir()
	marker := filepath.Join(dir, "pinged")
	script := filepath.Join(dir, "notify.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.notifyCmd = script

	// Short turn: started just now → no notifier.
	m.turnStarted = time.Now()
	m.pingOnTurnDone(nil)
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("short turn must not ping")
	}

	// Long turn: started long ago → notifier fires.
	m.turnStarted = time.Now().Add(-2 * pingMinTurn)
	m.pingOnTurnDone(nil)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("long turn should ping the notifier")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Zero start time (no turn): no-op.
	m.turnStarted = time.Time{}
	m.pingOnTurnDone(nil) // must not panic
}

func TestTurnStatsTrackThroughput(t *testing.T) {
	m := testModel(t)
	m.turnStarted = time.Now().Add(-10 * time.Second)
	m.turnOutChars = 4000 // ~1000 tokens over 10s → ~100 tok/s
	m.finishTurnStats()
	if m.lastOutToks != 1000 {
		t.Fatalf("lastOutToks = %d, want 1000", m.lastOutToks)
	}
	if m.lastTokRate < 80 || m.lastTokRate > 120 {
		t.Fatalf("lastTokRate = %.1f, want ~100", m.lastTokRate)
	}
	// Status bar should include the rate.
	found := false
	for _, seg := range m.statusBarParts() {
		if strings.Contains(seg.text, "tok/s") {
			found = true
		}
	}
	if !found {
		t.Fatal("status bar should show tok/s after a turn")
	}
	// No output: no stats update.
	m2 := testModel(t)
	m2.turnStarted = time.Now()
	m2.turnOutChars = 0
	m2.finishTurnStats()
	if m2.lastTokRate != 0 {
		t.Fatal("no output should record no rate")
	}
}

func TestLiveTokRateGating(t *testing.T) {
	m := testModel(t)
	// Idle: empty.
	if m.liveTokRate() != "" {
		t.Fatal("idle should have no live rate")
	}
	// Too little streamed: empty.
	m.turnStarted = time.Now().Add(-5 * time.Second)
	m.turnOutChars = 50
	if m.liveTokRate() != "" {
		t.Fatal("tiny output should have no live rate")
	}
	// Enough: shows.
	m.turnOutChars = 4000
	if got := m.liveTokRate(); !strings.Contains(got, "tok/s") {
		t.Fatalf("live rate should render, got %q", got)
	}
}

func TestGoalNagPingsWhileIdle(t *testing.T) {
	m := testModel(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "nagged")
	script := filepath.Join(dir, "notify.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.notifyCmd = script
	m.a.SetGoal("finish the migration")

	// A nag for the current generation while idle: pings, notes, re-arms.
	cmd := m.handleGoalNag(goalNagMsg{gen: m.idleGen})
	if cmd == nil {
		t.Fatal("nag should re-arm while the goal is open")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("goal nag should ping the notifier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	found := false
	for _, b := range m.blocks {
		if b.kind == blockNote && strings.Contains(b.body, "goal still open") {
			found = true
		}
	}
	if !found {
		t.Fatal("goal nag should add a note")
	}

	// Stale generation: no-op.
	if cmd := m.handleGoalNag(goalNagMsg{gen: m.idleGen - 1}); cmd != nil {
		t.Fatal("stale nag must not re-arm")
	}

	// Goal cleared: no-op, no re-arm.
	m.a.SetGoal("")
	if cmd := m.handleGoalNag(goalNagMsg{gen: m.idleGen}); cmd != nil {
		t.Fatal("cleared goal must stop the nag")
	}

	// Running turn: no-op (re-armed on turn done instead).
	m.a.SetGoal("still going")
	m.state = stRunning
	if cmd := m.handleGoalNag(goalNagMsg{gen: m.idleGen}); cmd != nil {
		t.Fatal("running turn must not nag")
	}
}

func TestScheduleGoalNagOnlyWithGoal(t *testing.T) {
	m := testModel(t)
	if m.scheduleGoalNag() != nil {
		t.Fatal("no goal → no nag timer")
	}
	m.a.SetGoal("x")
	if m.scheduleGoalNag() == nil {
		t.Fatal("goal set → nag timer expected")
	}
}

func TestLoopCommandSetShowClear(t *testing.T) {
	m := testModel(t)
	m.command("/loop 5m read GOALS.md and do the next unchecked item")
	if m.loopPrompt != "read GOALS.md and do the next unchecked item" {
		t.Fatalf("loop prompt wrong: %q", m.loopPrompt)
	}
	if m.loopEvery != 5*time.Minute {
		t.Fatalf("loop interval wrong: %v", m.loopEvery)
	}
	// Status bar shows it.
	found := false
	for _, seg := range m.statusBarParts() {
		if strings.Contains(seg.text, "loop=5m") {
			found = true
		}
	}
	if !found {
		t.Fatal("status bar should show the loop")
	}
	// No interval → default.
	m.command("/loop just do the thing")
	if m.loopEvery != defaultLoopInterval || m.loopPrompt != "just do the thing" {
		t.Fatalf("default interval wrong: %v %q", m.loopEvery, m.loopPrompt)
	}
	// Too-short interval rejected.
	m.command("/loop 5s too fast")
	if m.loopPrompt == "too fast" {
		t.Fatal("too-short interval must be rejected")
	}
	// Clear.
	m.command("/loop clear")
	if m.loopPrompt != "" || m.loopEvery != 0 {
		t.Fatal("/loop clear should reset")
	}
}

func TestLoopFiresOnlyWhenIdle(t *testing.T) {
	m := testModel(t)
	m.loopPrompt, m.loopEvery = "do it", time.Minute

	// Idle + current gen → fires (submit puts the model into running state).
	cmd := m.handleLoop(loopMsg{gen: m.idleGen})
	if cmd == nil {
		t.Fatal("idle loop should fire")
	}
	if m.state != stRunning {
		t.Fatal("loop fire should start a turn")
	}
	if m.loopRuns != 1 {
		t.Fatalf("loopRuns = %d, want 1", m.loopRuns)
	}

	// Running → defers (re-arms, does not submit again).
	runsBefore := m.loopRuns
	cmd = m.handleLoop(loopMsg{gen: m.idleGen})
	if cmd == nil {
		t.Fatal("running loop should re-arm for later")
	}
	if m.loopRuns != runsBefore {
		t.Fatal("running loop must not fire a turn")
	}

	// Stale gen → no-op.
	if cmd := m.handleLoop(loopMsg{gen: m.idleGen - 1}); cmd != nil {
		t.Fatal("stale loop msg must be ignored")
	}

	// Cleared → no-op.
	m.loopPrompt = ""
	if cmd := m.handleLoop(loopMsg{gen: m.idleGen}); cmd != nil {
		t.Fatal("cleared loop must not fire")
	}
}

func TestParseLoopArgs(t *testing.T) {
	every, prompt, err := parseLoopArgs("1h30m check the queue")
	if err != nil || every != 90*time.Minute || prompt != "check the queue" {
		t.Fatalf("got %v %q %v", every, prompt, err)
	}
	if _, _, err := parseLoopArgs("10m"); err == nil {
		t.Fatal("interval without prompt should error")
	}
	if _, _, err := parseLoopArgs(""); err == nil {
		t.Fatal("empty should error")
	}
}

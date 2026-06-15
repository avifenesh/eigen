package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// todoItem mirrors one entry of the agent's todo tool call.
type todoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

// maxTodoRows caps how many plan rows the panel shows.
const maxTodoRows = 8

// updateTodos parses a todo tool call's arguments into the live plan panel.
func (m *model) updateTodos(args json.RawMessage) {
	var in struct {
		Todos []todoItem `json:"todos"`
	}
	if json.Unmarshal(args, &in) != nil {
		return
	}
	m.todos = in.Todos
	m.relayout()
}

// topHeight is the number of rows the top panels occupy: the header bar (1,
// always present) plus the plan panel (0 when empty). The status bar lives at
// the bottom, so it is not counted here. screenToContent/toggleAtRow rebase by
// topHeight, so the transcript click mapping follows the header automatically.
func (m *model) topHeight() int {
	if m.sidebarVisible() {
		return 0 // the sidebar owns the header AND the plan rows
	}
	h := m.headerHeight()
	if len(m.todos) > 0 {
		rows := len(m.todos)
		if rows > maxTodoRows {
			rows = maxTodoRows
		}
		h += 1 + rows // plan header + tasks
	}
	return h
}

// todoGlyphStyled returns a colored marker for a task status.
func todoGlyphStyled(status string) string {
	switch status {
	case "completed":
		return styleStatus.Render("✓")
	case "in_progress":
		return styleAsk.Render("▸")
	case "cancelled":
		return styleReason.Render("✗")
	default:
		return dim("○")
	}
}

// statusBarView renders the persistent status line: model · perm · context
// usage · read-aloud. Shown at the bottom, below the input. It wraps onto a
// second line when the parts don't fit the terminal width (so nothing runs off
// the edge); statusBarHeight() reports how many rows it uses.
func (m *model) statusBarView() string {
	return strings.Join(m.statusBarLines(), "\n")
}

// statusBarHeight is the number of rows the status bar occupies (1 or 2). In
// sidebar mode the segments render as sidebar rows instead — the bottom bar
// is gone and the input area stays clean.
func (m *model) statusBarHeight() int {
	if m.sidebarVisible() {
		return 0
	}
	return len(m.statusBarLines())
}

// statusSeg is one status-bar segment: its plain text (for width math), the
// style used to render it, and the action a click on it dispatches (actNone =
// not clickable, e.g. the "eigen" brand or the tok/s readout).
type statusSeg struct {
	text   string
	style  lipgloss.Style
	action actionID
}

// statusBarParts assembles the colored status segments.
func (m *model) statusBarParts() []statusSeg {
	segs := []statusSeg{{text: "eigen", style: styleAccent.Bold(true)}}
	if m.backend != nil {
		// ModelID covers remote backends too (no live provider handle there).
		if id := m.backend.ModelID(); id != "" {
			segs = append(segs, statusSeg{text: modelShort(id), style: styleUser, action: actModelPicker})
		} else if p := m.backend.Provider(); p != nil {
			segs = append(segs, statusSeg{text: modelShort(p.Name()), style: styleUser, action: actModelPicker})
		}
	}
	if m.backend != nil {
		// perm: green when gated (safe), amber when auto (runs tools freely).
		permStyle := styleStatus
		if m.backend.Perm() == agent.PermAuto {
			permStyle = styleAsk
		}
		segs = append(segs, statusSeg{text: "perm=" + string(m.backend.Perm()), style: permStyle, action: actPermPicker})
		// input mode: steer (inject mid-turn) vs queue (next turn) — clickable.
		segs = append(segs, statusSeg{text: "input=" + normalizeInputMode(m.inputMode), style: styleTool, action: actInputModeToggle})
		if e := m.backend.Effort(); e != "" {
			segs = append(segs, statusSeg{text: "effort=" + e, style: styleTool, action: actEffortCycle})
		}
		if sm := m.backend.SearchMode(); sm != "" && sm != "off" {
			segs = append(segs, statusSeg{text: "search=" + sm, style: styleCode, action: actSearchCycle})
		}
	}
	if ind := m.ctxIndicator(); ind != "" {
		segs = append(segs, statusSeg{text: ind, style: m.ctxStyle(), action: actCompactPrompt})
	}
	if m.lastTokRate > 0 && m.state != stRunning {
		// Only on the idle status bar — while running, the live tok/s shows on
		// the spinner line above the input, so this would be a stale duplicate.
		// With provider-reported usage, show real in/out for the last turn.
		seg := fmt.Sprintf("↓%s %.0f tok/s", humanToks(m.lastOutToks), m.lastTokRate)
		if m.lastInToks > 0 {
			seg = fmt.Sprintf("↑%s ↓%s %.0f tok/s", humanToks(m.lastInToks), humanToks(m.lastOutToks), m.lastTokRate)
		}
		segs = append(segs, statusSeg{text: seg, style: styleReason})
	}
	if m.loopPrompt != "" {
		segs = append(segs, statusSeg{text: "loop=" + m.loopEvery.String(), style: styleAsk})
	}
	if llm.HasVision(m.modelID) {
		segs = append(segs, statusSeg{text: "vision", style: styleCode})
	}
	if m.router != nil && m.router.Enabled() {
		segs = append(segs, statusSeg{text: "route", style: styleStatus, action: actRouteToggle})
	}
	if m.readAloud {
		segs = append(segs, statusSeg{text: "read-aloud", style: styleStatus, action: actReadAloudToggle})
	}
	return segs
}

// ctxStyle colors the context indicator by how full the budget is: calm green
// under ~75%, amber approaching the limit, red when nearly full (a nudge to
// /compact before a 429).
func (m *model) ctxStyle() lipgloss.Style {
	if m.backend == nil || m.backend.MaxContextTokens() <= 0 {
		return styleReason
	}
	frac := float64(m.ctxTokens) / float64(m.backend.MaxContextTokens())
	switch {
	case frac >= 0.9:
		return styleErr
	case frac >= 0.75:
		return styleAsk
	default:
		return styleStatus
	}
}

// statusSegBox is a packed segment with its row and plain-text column range
// (start inclusive, end exclusive) — the geometry a click maps against.
type statusSegBox struct {
	seg      statusSeg
	row      int
	startCol int
	endCol   int
}

// statusBarLayout packs the segments into rows by display width (the SAME
// greedy packing statusBarLines renders), recording each segment's row and
// column range so a click can be mapped to its action. plainSepW is the width
// of the " · " separator drawn between segments.
func (m *model) statusBarLayout() []statusSegBox {
	segs := m.statusBarParts()
	w := m.width
	plainSepW := ansi.StringWidth(" · ")
	var boxes []statusSegBox
	row, col := 0, 0
	for _, s := range segs {
		segW := ansi.StringWidth(s.text)
		addW := segW
		if col > 0 {
			addW += plainSepW
		}
		// Wrap to a second row when the segment won't fit (mirrors
		// statusBarLines, which only ever uses up to 2 rows).
		if w > 0 && col+addW > w && col > 0 && row < 1 {
			row++
			col = 0
			addW = segW
		}
		start := col
		if col > 0 {
			start = col + plainSepW
		}
		boxes = append(boxes, statusSegBox{seg: s, row: row, startCol: start, endCol: start + segW})
		col = start + segW
	}
	return boxes
}

// statusActionAt maps an absolute screen (x,y) on the status bar to the action
// of the segment under it (actNone when between segments or on a non-clickable
// one). The status bar's top screen row is layout.status.y.
func (m *model) statusActionAt(x, y int) actionID {
	l := m.computeLayout()
	if l.status.empty() {
		return actNone
	}
	relRow := y - l.status.y
	for _, b := range m.statusBarLayout() {
		if b.row == relRow && x >= b.startCol && x < b.endCol {
			return b.seg.action
		}
	}
	return actNone
}

// statusBarLines packs the colored status segments into 1–2 width-respecting
// rows, the last padded with a faint accent rule. Width is measured in display
// columns (plain text), not bytes; each segment keeps its own color and the
// separators are dim.
func (m *model) statusBarLines() []string {
	segs := m.statusBarParts()
	w := m.width
	sep := dim(" · ")
	plainSep := " · "
	if w <= 0 {
		var parts []string
		for _, s := range segs {
			parts = append(parts, s.style.Render(s.text))
		}
		return []string{strings.Join(parts, sep)}
	}
	// Pack into rows by plain display width, rendering styled segments.
	var rows []string    // styled
	var rowsPlainW []int // plain width of each row
	cur := ""            // styled accumulator
	curW := 0            // plain width accumulator
	for _, s := range segs {
		addW := ansi.StringWidth(s.text)
		if cur != "" {
			addW += ansi.StringWidth(plainSep)
		}
		if curW+addW > w && cur != "" && len(rows) < 1 {
			rows = append(rows, cur)
			rowsPlainW = append(rowsPlainW, curW)
			cur, curW = "", 0
			addW = ansi.StringWidth(s.text)
		}
		if cur != "" {
			cur += sep
		}
		cur += s.style.Render(s.text)
		curW += addW
	}
	if cur != "" {
		rows = append(rows, cur)
		rowsPlainW = append(rowsPlainW, curW)
	}
	// Pad the final row with an accent rule to the width.
	last := len(rows) - 1
	if last >= 0 {
		if pad := w - rowsPlainW[last] - 1; pad > 0 {
			rows[last] += " " + styleAccent.Render(strings.Repeat("─", pad))
		}
	}
	return rows
}

// ctxIndicator is the approximate context usage vs the budget, e.g. "~12k/200k".
// It reads the cached token count (refreshCtx updates it at safe points) so the
// render never races the agent goroutine appending to the session.
func (m *model) ctxIndicator() string {
	used := m.ctxTokens
	if m.backend != nil && m.backend.MaxContextTokens() > 0 {
		return fmt.Sprintf("~%s/%s", kfmt(used), kfmt(m.backend.MaxContextTokens()))
	}
	return "~" + kfmt(used)
}

// refreshCtx recomputes the cached context-token estimate. Call only on the main
// goroutine when no turn is running (the session slice is otherwise mutated by
// the agent goroutine). It also fires a one-time proactive nudge as usage
// approaches the budget, so the user can /compact deliberately before
// auto-compaction (or a 429) kicks in.
func (m *model) refreshCtx() {
	if m.backend == nil {
		m.ctxTokens = 0
		m.ctxNudged = false
		return
	}
	m.ctxTokens = m.backend.Tokens()
	if m.backend.MaxContextTokens() <= 0 {
		return
	}
	frac := float64(m.ctxTokens) / float64(m.backend.MaxContextTokens())
	switch {
	case frac >= ctxNudgeFrac && !m.ctxNudged:
		m.ctxNudged = true
		m.note(fmt.Sprintf("context ~%d%% full — consider /compact (or keep going; it auto-compacts at the limit)", int(frac*100)))
	case frac < ctxNudgeFrac:
		m.ctxNudged = false // refilled headroom (e.g. after /compact): re-arm
	}
}

// ctxNudgeFrac is the budget fraction at which the proactive context nudge
// fires — below auto-compaction (which happens at the limit), so the user gets
// a chance to compact or refocus deliberately first.
const ctxNudgeFrac = 0.8

// kfmt formats a token count compactly (e.g. 12345 -> "12k").
func kfmt(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// modelShort trims a provider Name() like "openai.gpt-5.5 (bedrock mantle)" to
// the model id before the parenthetical.
func modelShort(name string) string {
	if i := strings.Index(name, " ("); i >= 0 {
		return name[:i]
	}
	return name
}

// planView renders the pinned plan panel shown above the transcript. Returns
// exactly topHeight() newline-terminated lines (or "" when empty).
func (m *model) planView() string {
	if len(m.todos) == 0 {
		return ""
	}
	done := 0
	for _, t := range m.todos {
		if t.Status == "completed" {
			done++
		}
	}
	var b strings.Builder
	b.WriteString(styleAccent.Render("▸ ") + styleUser.Render(fmt.Sprintf("plan (%d/%d)", done, len(m.todos))) + "\n")
	rows := len(m.todos)
	if rows > maxTodoRows {
		rows = maxTodoRows
	}
	for i := 0; i < rows; i++ {
		t := m.todos[i]
		line := todoGlyphStyled(t.Status) + " " + t.Content
		if t.Status == "completed" {
			line = todoGlyphStyled(t.Status) + " " + dim(t.Content)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// finishTurnStats records the completed turn's tokens and rate for the
// status bar. Provider-reported usage (EventDone) wins; the chars/4 estimate
// is the fallback (same heuristic as the context budget).
func (m *model) finishTurnStats() {
	if m.turnStarted.IsZero() {
		return
	}
	out := m.turnOutChars / 4
	if m.turnOutToks > 0 {
		out = m.turnOutToks
	}
	m.lastInToks = m.turnInToks
	m.turnInToks, m.turnOutToks = 0, 0
	if out == 0 {
		return
	}
	secs := time.Since(m.turnStarted).Seconds()
	m.lastOutToks = out
	if secs > 0 {
		m.lastTokRate = float64(out) / secs
	}
}

// humanToks renders a token count compactly ("843", "12k").
func humanToks(n int) string {
	if n >= 10000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// liveTokRate returns the in-flight output rate for the running status line
// ("" until enough has streamed to be meaningful).
func (m *model) liveTokRate() string {
	if m.turnStarted.IsZero() || m.turnOutChars < 200 {
		return ""
	}
	secs := time.Since(m.turnStarted).Seconds()
	if secs < 1 {
		return ""
	}
	return fmt.Sprintf(" · ↓%.0f tok/s", float64(m.turnOutChars/4)/secs)
}

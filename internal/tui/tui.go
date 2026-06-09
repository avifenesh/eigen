// Package tui renders an eigen session with Bubble Tea: a multi-turn REPL with
// a scrolling transcript of streamed model output, collapsible thinking and
// tool blocks, an input box, and inline gated approvals. It consumes the agent
// event sink.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/clipboard"
	"github.com/avifenesh/eigen/internal/dream"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/session"
	"github.com/avifenesh/eigen/internal/skill"
	"github.com/avifenesh/eigen/internal/speech"
	"github.com/avifenesh/eigen/internal/transcript"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	// Palette. 256-color indices chosen to be legible on both dark and light
	// terminals and to read as a small, coherent set rather than a rainbow:
	//   user = cyan, assistant prose = default fg, thinking = slate/grey,
	//   tool = lavender/violet, ok = green, warn/active = amber, error = red,
	//   accent (borders/rules) = soft blue.
	styleUser   = lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Bold(true)  // bright cyan
	styleTool   = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))            // soft violet
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))            // warm red
	styleReason = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))            // mid grey
	styleStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))             // green
	styleAsk    = lipgloss.NewStyle().Foreground(lipgloss.Color("215")).Bold(true) // amber
	styleCode   = lipgloss.NewStyle().Foreground(lipgloss.Color("80"))             // teal

	// accent is the calm structural color for borders, rules, and the prompt
	// caret — present but not loud.
	accent      = lipgloss.Color("67") // muted steel blue
	styleAccent = lipgloss.NewStyle().Foreground(accent)

	// Markdown prose styles for assistant answers.
	styleHeading    = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true) // blue
	styleBold       = lipgloss.NewStyle().Bold(true)
	styleItalic     = lipgloss.NewStyle().Italic(true)
	styleInlineCode = lipgloss.NewStyle().Foreground(lipgloss.Color("80"))            // teal
	styleQuote      = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	styleBullet     = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))           // violet
)

type uiState int

const (
	stInput uiState = iota
	stRunning
)

// inputMaxRows caps how tall the multi-line input box can grow before it
// scrolls internally, so a long pasted prompt never eats the whole screen.
const inputMaxRows = 8

type agentEvent struct{ e agent.Event }

type approvalMsg struct {
	name  string
	args  json.RawMessage
	reply chan bool
}

type turnDoneMsg struct{ err error }

type submitMsg struct{ task string }

// idleTickMsg fires after the idle delay; gen guards against stale timers.
type idleTickMsg struct{ gen int }

// dreamDoneMsg carries notes distilled by idle dreaming.
type dreamDoneMsg struct{ notes []string }

// compactDoneMsg reports the result of an on-demand /compact.
type compactDoneMsg struct {
	before, after       int
	beforeTok, afterTok int
	err                 error
}

type model struct {
	vp      viewport.Model
	sp      spinner.Model
	ti      textarea.Model
	a       *agent.Agent
	session *agent.Session
	ctx     context.Context

	blocks  []*block
	sel     int // index of the selected block (-1 = none / following tail)
	state   uiState
	pending *approvalMsg
	status  string

	// approvedTools are tool names the user chose to always allow this session.
	approvedTools map[string]bool

	initialTask string
	width       int
	height      int
	ready       bool

	rebuild     bool
	rebuildBin  string
	srcDir      string
	sessionPath string

	// session picker
	store   *session.Store
	picking bool
	picks   []*session.Meta
	pickIdx int

	// model picker (bare /model)
	modelPicking bool
	modelPicks   []llm.ModelInfo
	modelPickIdx int

	// steer + queue: messages typed while a turn runs are queued and sent when
	// it finishes; esc interrupts the running turn via cancel.
	queued []string
	cancel context.CancelFunc

	// slash-command + @file autocomplete menu for the input box.
	comp      completion
	fileIdx   []string
	fileIdxAt time.Time

	// live plan panel, driven by the agent's todo tool calls.
	todos []todoItem

	// blockStart[i] is the first viewport line of block i; the final entry is
	// the total line count. Rebuilt by sync; used to map mouse clicks to blocks.
	blockStart []int

	// plainLines is the transcript rendered with ANSI escapes stripped, one
	// entry per viewport content line — the source for drag-to-copy selection.
	// Rebuilt by sync.
	plainLines []string

	// drag selection: while the left button is held, selecting is true and
	// selAnchor/selCursor hold the start and current points in content
	// coordinates (line = viewport content line, col = rune column). dragMoved
	// distinguishes a real drag (copy on release) from a plain click (toggle).
	selecting bool
	dragMoved bool
	selAnchor point
	selCursor point

	// read-aloud: speak assistant answers via TTS when enabled.
	speaker   speakerIface
	readAloud bool

	// copy-to-clipboard support.
	clip clipIface

	// live model switching (/model): the current provider/model and a
	// constructor (injected; nil disables switching, e.g. in tests).
	provName    string
	modelID     string
	newProvider func(provider, model string) (llm.Provider, error)

	// idle dreaming: after the session is idle, reflect into project memory.
	mem         *memory.Store
	dreamOnIdle bool
	idleMinutes int
	idleGen     int // bumped on each turn; stale idle ticks are ignored

	// skills are the discovered SKILL.md skills, for /skills browse + preview.
	skills *skill.Set

	// overload failover: after a persistent provider overload (503 after all
	// retries), the session is redirected to a known-good fallback model for
	// failoverTurns turns, then switched back. failoverFrom remembers the
	// original provider/model; failoverLeft counts down remaining turns.
	failoverFrom *failoverOrigin
	failoverLeft int

	// ctxTokens caches the estimated context size; recomputed only at safe
	// points (never while the agent goroutine is appending to the session) so
	// the status bar render stays race-free and cheap.
	ctxTokens int

	// streamedText reports whether any assistant text delta arrived this turn,
	// so EventDone only renders the final answer when nothing was streamed.
	streamedText bool

	// input history: previously entered lines, recalled with ↑/↓ (shell-style).
	history   []string
	histIdx   int    // browse cursor; len(history) == live draft (not browsing)
	histDraft string // the in-progress input saved while browsing history
}

// speakerIface is the slice of speech.Speaker the TUI uses (and that tests fake).
type speakerIface interface {
	Speak(text string)
	Available() bool
	Stop()
}

// clipIface is the slice of clipboard.Copier the TUI uses (and that tests fake).
type clipIface interface {
	Copy(text string) error
	Available() bool
	CanPaste() bool
	Paste() (string, error)
}

// Result reports why the TUI exited.
type Result struct {
	Rebuild     bool
	SessionPath string
	BinPath     string
	// Provider/Model/Perm/Effort/Search are the live session config at exit
	// (possibly changed via /model, /perm, /effort, /search), so a
	// rebuild-resume continues exactly as the conversation was — not reset to
	// the original launch flags. Effort/Search are empty when the model does
	// not support them.
	Provider string
	Model    string
	Perm     string
	Effort   string
	Search   string
}

type buildDoneMsg struct {
	bin string
	out string
	err error
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink, m.sp.Tick}
	if m.initialTask != "" {
		task := m.initialTask
		cmds = append(cmds, func() tea.Msg { return submitMsg{task} })
	}
	return tea.Batch(cmds...)
}

// --- block helpers ---------------------------------------------------------

func (m *model) push(b *block) *block {
	m.blocks = append(m.blocks, b)
	m.sel = -1 // new content: follow the tail
	m.sync()
	return b
}

func (m *model) note(s string) { m.push(&block{kind: blockNote, body: sb(s)}) }
func (m *model) text(role, s string) *block {
	return m.push(&block{kind: blockText, role: role, body: sb(s)})
}

// autosave persists the current conversation to the session file. It is safe
// to call repeatedly and never panics: a save failure must not crash the UI.
// It also writes a sidecar meta file recording the live config (provider,
// model, perm, effort, search) so a plain restart/--resume continues exactly as
// the conversation was.
func (m *model) autosave() {
	if m == nil || m.sessionPath == "" || m.session == nil {
		return
	}
	defer func() { _ = recover() }()
	_ = transcript.Save(m.sessionPath, m.session.Messages())
	m.saveMeta()
}

// saveMeta writes the session meta sidecar capturing the live session config.
func (m *model) saveMeta() {
	if m == nil || m.sessionPath == "" {
		return
	}
	meta := transcript.SessionMeta{
		Provider: m.provName,
		Model:    m.modelID,
	}
	// During an overload failover window, persist the ORIGINAL model: the
	// fallback is temporary, and a restart should resume on the user's choice
	// (failing over again if the model is still overloaded).
	if m.failoverFrom != nil {
		meta.Provider = m.failoverFrom.provider
		meta.Model = m.failoverFrom.model
	}
	if m.a != nil {
		meta.Perm = string(m.a.Perm)
		meta.Effort = liveEffort(m.a.Provider)
		meta.Search = liveSearch(m.a.Provider)
	}
	_ = transcript.SaveMeta(m.sessionPath, meta)
}

// lastAssistantText returns the body of the most recent assistant text block
// (for read-aloud); empty if there is none.
func (m *model) lastAssistantText() string {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == blockText && m.blocks[i].role == "assistant" {
			return m.blocks[i].body
		}
	}
	return ""
}

// scheduleIdleDream returns a timer that fires an idleTickMsg after the idle
// delay, tagged with the current generation. Returns nil when idle dreaming is
// disabled, so it is a no-op cost otherwise.
func (m *model) scheduleIdleDream() tea.Cmd {
	if !m.dreamOnIdle || m.mem == nil {
		return nil
	}
	gen := m.idleGen
	delay := time.Duration(m.idleMinutes) * time.Minute
	return tea.Tick(delay, func(time.Time) tea.Msg { return idleTickMsg{gen: gen} })
}

// dreamCmd reflects over the current session into project memory in the
// background, returning the distilled notes via dreamDoneMsg.
func (m *model) dreamCmd() tea.Cmd {
	if m.mem == nil || m.a == nil || m.a.Provider == nil || m.session == nil {
		return nil
	}
	prov := m.a.Provider
	convo := dream.RenderSession(m.session.Messages())
	existing := m.mem.Read()
	return func() tea.Msg {
		notes, err := dream.Distill(context.Background(), prov, []string{convo}, existing)
		if err != nil {
			return dreamDoneMsg{}
		}
		return dreamDoneMsg{notes: notes}
	}
}

func sb(s string) string { return s }

// compactCmd summarizes the conversation on demand (the /compact command),
// running the summarizer off the UI goroutine and reporting via compactDoneMsg.
func (m *model) compactCmd() tea.Cmd {
	beforeTok := m.session.Tokens()
	sess := m.session
	return func() tea.Msg {
		before, after, err := sess.Compact(context.Background(), 0)
		return compactDoneMsg{before: before, after: after, beforeTok: beforeTok, afterTok: sess.Tokens(), err: err}
	}
}

// lastOpen returns the most recent block of kind k that is still being streamed
// into (used to append deltas), or nil.
func (m *model) lastOpen(k blockKind) *block {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		if m.blocks[i].kind == k {
			return m.blocks[i]
		}
		if m.blocks[i].kind == blockText && m.blocks[i].role == "assistant" {
			continue
		}
		break
	}
	return nil
}

// sync rebuilds the viewport content from blocks, wrapping to width, and
// records each block's first viewport line in blockStart (so mouse clicks can
// be mapped back to a block). It also records the plain-text (ANSI-stripped)
// content lines used by drag-to-copy selection.
func (m *model) sync() {
	if !m.ready {
		return
	}
	w := m.vp.Width
	var out strings.Builder
	m.blockStart = m.blockStart[:0]
	m.plainLines = m.plainLines[:0]
	line := 0
	for i, b := range m.blocks {
		// Breathing room: a blank separator line before every block but the
		// first, so messages / thoughts / tool actions don't run together. It is
		// tracked in plainLines + line count so click-mapping and drag-selection
		// stay aligned with the rendered viewport.
		if i > 0 {
			out.WriteString("\n")
			m.plainLines = append(m.plainLines, "")
			line++
		}
		rendered := b.renderWrapped(i == m.sel, w)
		m.blockStart = append(m.blockStart, line)
		line += strings.Count(rendered, "\n") + 1
		out.WriteString(rendered)
		out.WriteString("\n")
		for _, l := range strings.Split(rendered, "\n") {
			m.plainLines = append(m.plainLines, ansi.Strip(l))
		}
	}
	m.blockStart = append(m.blockStart, line) // sentinel: total line count
	m.vp.SetContent(out.String())
	if m.sel < 0 {
		m.vp.GotoBottom()
	}
}

// inputRows returns how many terminal rows the input box occupies including its
// border (top+bottom): the textarea grows with its wrapped content up to
// inputMaxRows, plus 2 for the rounded border frame.
func (m *model) inputRows() int {
	h := m.ti.Height()
	if h < 1 {
		h = 1
	}
	return h + 2 // + top & bottom border rows
}

// visualInputRows counts the number of *visual* (soft-wrapped) rows the current
// input text occupies, so a single long line that wraps grows the box. It must
// match the textarea's own word-wrap (which can break earlier than a hard
// column split), otherwise the box is sized too short and the textarea scrolls
// its first line out of view. We replicate the bubbles word-wrap row count.
func (m *model) visualInputRows() int {
	w := m.ti.Width()
	if w < 1 {
		return m.ti.LineCount()
	}
	total := 0
	for _, line := range strings.Split(m.ti.Value(), "\n") {
		total += wrappedRowCount(line, w)
	}
	if total < 1 {
		total = 1
	}
	return total
}

// wrappedRowCount returns how many visual rows a single logical line occupies
// when word-wrapped to width w, matching bubbles/textarea's wrap(): words are
// kept whole and moved to the next row when they would overflow; a word longer
// than the line is hard-split. Always at least 1.
func wrappedRowCount(line string, w int) int {
	if w < 1 {
		return 1
	}
	rows := 1
	col := 0
	for _, field := range splitKeepingSpaces(line) {
		fw := ansi.StringWidth(field)
		if strings.TrimSpace(field) == "" {
			// trailing/leading spaces: they extend the current column
			col += fw
			continue
		}
		if col > 0 && col+fw > w {
			rows++
			col = 0
		}
		// A word longer than the whole line hard-wraps across rows.
		for fw > w {
			rows++
			fw -= w
		}
		col += fw
	}
	return rows
}

// splitKeepingSpaces splits s into alternating word / whitespace chunks so the
// wrap estimator can treat spaces like the textarea does.
func splitKeepingSpaces(s string) []string {
	var out []string
	var cur strings.Builder
	inSpace := false
	for i, r := range s {
		sp := r == ' ' || r == '\t'
		if i > 0 && sp != inSpace {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
		inSpace = sp
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// wrapSegments returns the visual rows a single logical line breaks into when
// word-wrapped to width w (mirrors bubbles/textarea wrap closely enough for
// click mapping). Long words are hard-split by runes.
func wrapSegments(line string, w int) []string {
	if w < 1 {
		return []string{line}
	}
	var rows []string
	var cur strings.Builder
	curw := 0
	flush := func() { rows = append(rows, cur.String()); cur.Reset(); curw = 0 }
	for _, field := range splitKeepingSpaces(line) {
		fw := ansi.StringWidth(field)
		if strings.TrimSpace(field) == "" {
			cur.WriteString(field)
			curw += fw
			continue
		}
		if curw > 0 && curw+fw > w {
			flush()
		}
		if fw > w {
			// Hard-split a word longer than the line, rune by rune.
			for _, r := range field {
				rwi := ansi.StringWidth(string(r))
				if curw+rwi > w && curw > 0 {
					flush()
				}
				cur.WriteRune(r)
				curw += rwi
			}
			continue
		}
		cur.WriteString(field)
		curw += fw
	}
	rows = append(rows, cur.String())
	return rows
}

// inputTopRow is the absolute screen row of the input box's top border.
func (m *model) inputTopRow() int {
	r := m.topHeight() + m.vp.Height
	if m.state == stRunning {
		r++ // spinner/status line above the input
	}
	if m.comp.active() {
		r += m.comp.rows()
	}
	return r
}

// inputPromptWidth is the visual width of the prompt caret rendered on each
// text row (e.g. "│ ").
func (m *model) inputPromptWidth() int { return ansi.StringWidth(m.ti.Prompt) }

// clickInInput reports whether an absolute screen (x,y) falls on a text row of
// the input box and, if so, the visual text-row index (0-based, from the top of
// the box) and the rune column within that row.
func (m *model) clickInInput(x, y int) (vrow, col int, ok bool) {
	if m.pending != nil {
		return 0, 0, false
	}
	top := m.inputTopRow()
	// Row 0 of the box is the top border; text rows follow; then bottom border.
	vrow = y - top - 1
	if vrow < 0 || vrow >= m.ti.Height() {
		return 0, 0, false
	}
	// Columns: 1 border + prompt width before the text begins.
	col = x - 1 - m.inputPromptWidth()
	if col < 0 {
		col = 0
	}
	return vrow, col, true
}

// positionCursorAt moves the textarea cursor to the visual row (from the top of
// the visible text) and rune column, mapping back through the word-wrap to a
// logical (line, offset). Best-effort: when the box is scrolled (content taller
// than inputMaxRows) the mapping is approximate.
func (m *model) positionCursorAt(vrow, col int) {
	w := m.ti.Width()
	lines := strings.Split(m.ti.Value(), "\n")
	vr := 0
	for li, line := range lines {
		segs := wrapSegments(line, w)
		if vrow < vr+len(segs) {
			segIdx := vrow - vr
			off := 0
			for k := 0; k < segIdx; k++ {
				off += len([]rune(segs[k]))
			}
			seg := []rune(segs[segIdx])
			cc := col
			if cc > len(seg) {
				cc = len(seg)
			}
			off += cc
			// Move to logical line li, then set the column.
			for m.ti.Line() < li {
				m.ti.CursorDown()
			}
			for m.ti.Line() > li {
				m.ti.CursorUp()
			}
			m.ti.SetCursor(off)
			return
		}
		vr += len(segs)
	}
	m.ti.CursorEnd()
}

// pasteIntoInput inserts the clipboard contents at the input cursor (right-click
// paste). Newlines are kept literal so a multi-line paste fills the box.
func (m *model) pasteIntoInput() {
	if m.clip == nil || !m.clip.CanPaste() {
		m.push(&block{kind: blockNote, isErr: true, body: sb("no paste command found (set EIGEN_CLIPBOARD_PASTE_CMD or install wl-paste/xclip/xsel/pbpaste)")})
		return
	}
	text, err := m.clip.Paste()
	if err != nil || text == "" {
		if err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("paste failed: " + err.Error())})
		}
		return
	}
	m.ti.InsertString(text)
	m.resizeInput()
	m.refreshCompletion()
}

// bottomHeight is the number of terminal rows the bottom UI occupies: the input
// box (1+ rows incl. border), the persistent status bar (1–2 rows), plus a
// status/spinner line while a turn runs, plus the autocomplete menu.
func (m *model) bottomHeight() int {
	if m.pending != nil {
		return 1 + m.statusBarHeight() // approval prompt + status bar
	}
	h := m.inputRows() // input box (grows with content, incl. border)
	h += m.statusBarHeight()
	if m.state == stRunning {
		h++ // status/spinner line above the input
	}
	if m.comp.active() {
		h += m.comp.rows()
	}
	return h
}

// resizeInput grows/shrinks the input box to fit its content (1..inputMaxRows)
// and relays out when the height changes. It counts soft-wrapped visual rows,
// so a long single line that wraps also grows the box.
func (m *model) resizeInput() {
	want := m.visualInputRows()
	if want < 1 {
		want = 1
	}
	if want > inputMaxRows {
		want = inputMaxRows
	}
	if want != m.ti.Height() {
		m.ti.SetHeight(want)
		m.relayout()
	}
}

// relayout sizes the viewport to leave room for the top plan panel and the
// bottom UI.
func (m *model) relayout() {
	if !m.ready {
		return
	}
	h := m.height - 1 - m.bottomHeight() - m.topHeight()
	if h < 1 {
		h = 1
	}
	m.vp.Width = m.width
	m.vp.Height = h
	m.sync()
}

// collapsibleIdx returns block indices that can be selected/toggled.
func (m *model) collapsibleIdx() []int {
	var idx []int
	for i, b := range m.blocks {
		if b.collapsible() {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *model) moveSel(dir int) {
	idx := m.collapsibleIdx()
	if len(idx) == 0 {
		return
	}
	cur := -1
	for j, i := range idx {
		if i == m.sel {
			cur = j
			break
		}
	}
	switch {
	case cur == -1 && dir < 0:
		m.sel = idx[len(idx)-1] // entering from tail → last
	case cur == -1:
		m.sel = idx[0]
	default:
		n := cur + dir
		if n < 0 {
			n = 0
		}
		if n >= len(idx) {
			m.sel = -1 // past the end → back to following tail
			m.sync()
			return
		}
		m.sel = idx[n]
	}
	m.sync()
}

func (m *model) toggleSel() {
	if m.sel >= 0 && m.sel < len(m.blocks) && m.blocks[m.sel].collapsible() {
		m.blocks[m.sel].collapsed = !m.blocks[m.sel].collapsed
		m.sync()
	}
}

// togglePerm flips the permission posture between gated and auto — the keyboard
// shortcut (ctrl+a) for fast mode changes, equivalent to /perm gated|auto. It
// persists the new posture to the session meta so it survives rebuild/resume.
func (m *model) togglePerm() {
	if m.a == nil {
		return
	}
	if m.a.Perm == agent.PermAuto {
		m.a.Perm = agent.PermGated
	} else {
		m.a.Perm = agent.PermAuto
	}
	m.saveMeta()
	m.note("permission posture → " + string(m.a.Perm))
}

// cycleEffort steps the reasoning effort to the next level (wrapping) — the
// keyboard shortcut (ctrl+e) for fast effort changes, equivalent to /effort. It
// is a no-op (with a note) when the current model has no effort setting.
func (m *model) cycleEffort() {
	if m.a == nil {
		return
	}
	es, ok := m.a.Provider.(llm.EffortSetter)
	if !ok {
		m.note("the current model does not support a reasoning-effort setting")
		return
	}
	cur := es.Effort()
	next := cur
	for i, l := range llm.EffortLevels {
		if l == cur {
			next = llm.EffortLevels[(i+1)%len(llm.EffortLevels)]
			break
		}
	}
	if next == cur || !es.SetEffort(next) {
		// Current level not found in the list, or set failed: start at the first.
		if len(llm.EffortLevels) > 0 {
			_ = es.SetEffort(llm.EffortLevels[0])
		}
	}
	m.saveMeta()
	m.note("reasoning effort → " + es.Effort())
}

// cycleModel switches to the next model in the catalog (wrapping) — the
// keyboard shortcut (ctrl+o) for fast model changes, equivalent to /model. The
// provider is reconciled from the catalog so it never desyncs.
func (m *model) cycleModel() {
	if m.newProvider == nil {
		m.push(&block{kind: blockNote, isErr: true, body: sb("model switching unavailable")})
		return
	}
	models := llm.Models()
	if len(models) == 0 {
		return
	}
	// Find the current model, then advance to the next entry (wrapping).
	idx := -1
	for i, mi := range models {
		if mi.ID == m.modelID {
			idx = i
			break
		}
	}
	next := models[(idx+1)%len(models)]
	prov := llm.ResolveProvider(next.Provider, next.ID)
	np, err := m.newProvider(prov, next.ID)
	if err != nil {
		m.push(&block{kind: blockNote, isErr: true, body: sb("switch failed: " + err.Error())})
		return
	}
	m.a.Provider = np
	m.a.Compactor = llm.NewCompactor(np)
	m.provName, m.modelID = prov, next.ID
	if w := llm.EffectiveContextWindow(next.ID); w > 0 {
		m.a.MaxContextTokens = contextBudgetFor(w)
	}
	// A manual switch takes precedence over any overload failover window.
	m.failoverFrom = nil
	m.failoverLeft = 0
	m.saveMeta()
	m.note("model → " + np.Name())
}

// recordHistory appends a submitted line to the input history and resets the
// browse cursor to the live end.
func (m *model) recordHistory(line string) {
	if line == "" {
		return
	}
	// Avoid consecutive duplicates.
	if n := len(m.history); n == 0 || m.history[n-1] != line {
		m.history = append(m.history, line)
	}
	m.histIdx = len(m.history)
	m.histDraft = ""
}

// historyPrev recalls an older input (↑), saving the live draft first.
func (m *model) historyPrev() {
	if len(m.history) == 0 {
		return
	}
	if m.histIdx >= len(m.history) {
		m.histDraft = m.ti.Value()
		m.histIdx = len(m.history)
	}
	if m.histIdx > 0 {
		m.histIdx--
		m.ti.SetValue(m.history[m.histIdx])
		m.ti.CursorEnd()
		m.resizeInput()
	}
}

// historyNext recalls a newer input (↓), restoring the live draft past the end.
func (m *model) historyNext() {
	if m.histIdx >= len(m.history) {
		return
	}
	m.histIdx++
	if m.histIdx >= len(m.history) {
		m.ti.SetValue(m.histDraft)
	} else {
		m.ti.SetValue(m.history[m.histIdx])
	}
	m.ti.CursorEnd()
	m.resizeInput()
}

// copySelected copies the selected block (or the last answer) to the clipboard.
func (m *model) copySelected() {
	if m.clip == nil || !m.clip.Available() {
		return
	}
	if text := m.copyTarget(); text != "" {
		if err := m.clip.Copy(text); err == nil {
			m.note("copied to clipboard")
		}
	}
}

// findBlocks returns indices of blocks whose text matches q (case-insensitive),
// searching body, tool result, title, and rich header.
func (m *model) findBlocks(q string) []int {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	var out []int
	for i, b := range m.blocks {
		hay := strings.ToLower(b.body + "\n" + b.result + "\n" + b.title + "\n" + b.header())
		if strings.Contains(hay, q) {
			out = append(out, i)
		}
	}
	return out
}

// scrollToSelected scrolls the viewport so the selected block is in view.
func (m *model) scrollToSelected() {
	if m.sel < 0 || m.sel >= len(m.blockStart) {
		return
	}
	m.vp.SetYOffset(m.blockStart[m.sel])
}

// copyTarget is the text /copy puts on the clipboard: the selected block (body +
// tool result) if one is selected, otherwise the latest assistant message.
func (m *model) copyTarget() string {
	if m.sel >= 0 && m.sel < len(m.blocks) {
		b := m.blocks[m.sel]
		text := b.body
		if b.result != "" {
			if text != "" {
				text += "\n"
			}
			text += b.result
		}
		return text
	}
	return m.lastAssistantText()
}

// toggleAtRow maps an absolute screen row (msg.Y) to a transcript block and
// toggles it if collapsible — the click handler for thinking/tool blocks. The
// viewport starts topHeight() rows down (below the plan panel), so the click is
// rebased into viewport space first.
func (m *model) toggleAtRow(y int) {
	y -= m.topHeight() // rebase: rows above the viewport (plan panel) don't count
	if y < 0 || y >= m.vp.Height || len(m.blockStart) < 2 {
		return
	}
	target := m.vp.YOffset + y
	for i := 0; i+1 < len(m.blockStart); i++ {
		if target >= m.blockStart[i] && target < m.blockStart[i+1] {
			if i < len(m.blocks) && m.blocks[i].collapsible() {
				m.sel = i
				m.blocks[i].collapsed = !m.blocks[i].collapsed
				m.sync()
			}
			return
		}
	}
}

// --- update ----------------------------------------------------------------

func (m *model) submit(task string) tea.Cmd {
	m.text("user", task)
	m.state = stRunning
	m.status = "thinking"
	m.comp = completion{kind: compNone}
	m.streamedText = false
	m.idleGen++ // invalidate any pending idle-dream timer
	// Keep the input focused so the user can steer/queue while the turn runs.
	m.relayout()
	tctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	return tea.Batch(m.sp.Tick, func() (msg tea.Msg) {
		// Recover panics in the agent goroutine so a bug becomes a recoverable
		// error in the UI instead of taking down the whole program.
		defer func() {
			if r := recover(); r != nil {
				msg = turnDoneMsg{err: fmt.Errorf("internal panic: %v", r)}
			}
		}()
		_, err := m.session.Send(tctx, task)
		return turnDoneMsg{err: err}
	})
}

// resend re-drives the current turn (history already holds the user message)
// after a failover switched the provider — the turn resumes where it stopped.
func (m *model) resend() tea.Cmd {
	m.state = stRunning
	m.status = "retrying on " + m.modelID
	m.streamedText = false
	m.idleGen++
	m.relayout()
	tctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	return tea.Batch(m.sp.Tick, func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = turnDoneMsg{err: fmt.Errorf("internal panic: %v", r)}
			}
		}()
		_, err := m.session.Resend(tctx)
		return turnDoneMsg{err: err}
	})
}

func (m *model) Update(msg tea.Msg) (next tea.Model, cmd tea.Cmd) {
	// Safety net: a panic anywhere in the UI loop becomes a recoverable error
	// line instead of crashing the program and losing the session.
	defer func() {
		if r := recover(); r != nil {
			m.picking = false
			m.modelPicking = false
			m.state = stInput
			m.push(&block{kind: blockNote, isErr: true, body: sb(fmt.Sprintf("internal error (recovered): %v", r))})
			m.ti.Focus()
			next, cmd = m, textarea.Blink
		}
	}()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.ready {
			m.vp = viewport.New(msg.Width, 1)
			m.ready = true
		}
		m.ti.SetWidth(msg.Width - 2)
		m.relayout()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			// Autosave already happens continuously via the agent's Persist hook;
			// only save here when idle (no turn running) to avoid racing the
			// agent goroutine that owns the session during a turn.
			if m.state != stRunning {
				m.autosave()
			}
			return m, tea.Quit
		}
		// Session picker captures keys while open.
		if m.picking {
			switch msg.String() {
			case "up", "ctrl+p", "alt+up", "k":
				if m.pickIdx > 0 {
					m.pickIdx--
				}
			case "down", "ctrl+n", "alt+down", "j":
				if m.pickIdx < len(m.picks)-1 {
					m.pickIdx++
				}
			case "enter":
				sel := m.picks[m.pickIdx]
				m.picking = false
				m.loadSessionByID(sel.ID)
			case "esc", "q":
				m.picking = false
				m.sync()
			}
			return m, nil
		}
		// Model picker (bare /model) captures keys while open.
		if m.modelPicking {
			switch msg.String() {
			case "up", "ctrl+p", "alt+up", "k":
				if m.modelPickIdx > 0 {
					m.modelPickIdx--
				}
			case "down", "ctrl+n", "alt+down", "j":
				if m.modelPickIdx < len(m.modelPicks)-1 {
					m.modelPickIdx++
				}
			case "enter":
				sel := m.modelPicks[m.modelPickIdx]
				m.modelPicking = false
				m.sync()
				return m, m.command("/model " + sel.ID)
			case "esc", "q":
				m.modelPicking = false
				m.sync()
			}
			return m, nil
		}
		if m.pending != nil {
			switch strings.ToLower(msg.String()) {
			case "y":
				m.pending.reply <- true
				m.note("approved")
				m.pending = nil
				m.relayout()
			case "a":
				// Always allow this tool for the rest of the session.
				if m.approvedTools == nil {
					m.approvedTools = map[string]bool{}
				}
				m.approvedTools[m.pending.name] = true
				m.pending.reply <- true
				m.note("always allowing " + m.pending.name + " this session")
				m.pending = nil
				m.relayout()
			case "n", "esc":
				m.pending.reply <- false
				m.note("denied")
				m.pending = nil
				m.relayout()
			}
			return m, nil
		}
		// Autocomplete menu (slash commands / @file) captures nav + select keys.
		if m.comp.active() {
			switch msg.String() {
			case "up", "ctrl+p", "alt+up":
				if m.comp.idx > 0 {
					m.comp.idx--
				}
				return m, nil
			case "down", "ctrl+n", "alt+down":
				if m.comp.idx < len(m.comp.items)-1 {
					m.comp.idx++
				}
				return m, nil
			case "tab":
				m.applyCompletion()
				return m, nil
			case "enter":
				// Slash: run the highlighted command. Mention: just insert it.
				if m.comp.kind == compSlash {
					name := ""
					if m.comp.idx < len(m.comp.items) {
						name = m.comp.items[m.comp.idx].label
					}
					m.ti.Reset()
					m.comp = completion{kind: compNone}
					m.relayout()
					if name != "" {
						return m, m.command(name)
					}
					return m, nil
				}
				m.applyCompletion()
				return m, nil
			case "esc":
				m.comp = completion{kind: compNone}
				m.relayout()
				return m, nil
			}
			// Other keys fall through to editing the input (to narrow the filter).
		}
		// Navigation/history works in any state (input box keeps focus for text).
		// alt+… variants are provided alongside ctrl+… because terminal
		// multiplexers (zellij, tmux) capture ctrl+p/ctrl+n/ctrl+o before they
		// reach the app.
		switch msg.String() {
		case "up":
			m.historyPrev()
			return m, nil
		case "down":
			m.historyNext()
			return m, nil
		case "ctrl+p", "alt+up", "alt+k":
			m.moveSel(-1)
			return m, nil
		case "ctrl+n", "alt+down", "alt+j":
			m.moveSel(1)
			return m, nil
		case "tab", "shift+tab":
			m.toggleSel()
			return m, nil
		case "ctrl+y", "alt+y":
			m.copySelected()
			return m, nil
		case "ctrl+a", "alt+a":
			// Quick toggle of the permission posture (gated ↔ auto) without
			// typing /perm. "a" = auto/approval mode.
			m.togglePerm()
			return m, nil
		case "ctrl+e", "alt+r":
			// Quick cycle of the reasoning effort (wraps) without typing /effort.
			// alt+r ("reasoning") is the multiplexer-safe alternative.
			m.cycleEffort()
			return m, nil
		case "ctrl+o", "alt+m":
			// Quick cycle to the next model in the catalog (wraps), without
			// typing /model. "o" for mOdel (ctrl+m is Enter in terminals);
			// alt+m is the multiplexer-safe alternative (ctrl+o is a zellij key).
			m.cycleModel()
			return m, nil
		case "pgup":
			m.vp.HalfViewUp()
			return m, nil
		case "pgdown":
			m.vp.HalfViewDown()
			return m, nil
		}
		switch m.state {
		case stInput:
			switch msg.String() {
			case "enter":
				task := strings.TrimSpace(m.ti.Value())
				if task == "" {
					return m, nil
				}
				m.recordHistory(task)
				m.ti.Reset()
				m.ti.SetHeight(1)
				m.comp = completion{kind: compNone}
				m.relayout()
				if strings.HasPrefix(task, "/") {
					return m, m.command(task)
				}
				return m, m.submit(task)
			case "ctrl+j", "alt+enter":
				// Insert a literal newline (multi-line prompts) without submitting.
				m.ti.InsertString("\n")
				m.resizeInput()
				m.refreshCompletion()
				return m, nil
			}
			// Do not bind the spacebar while the input is focused: it must insert
			// spaces in prompts even when a transcript block is selected. Use tab to
			// expand/collapse blocks.
			var cmd tea.Cmd
			m.ti, cmd = m.ti.Update(msg)
			m.resizeInput()
			m.refreshCompletion()
			return m, cmd

		case stRunning:
			// Steer + queue: Enter queues the typed message for the next turn;
			// esc interrupts the running turn so a queued message starts now.
			switch msg.String() {
			case "enter":
				task := strings.TrimSpace(m.ti.Value())
				if task == "" {
					return m, nil
				}
				// Slash commands are control input, not conversation. Settings
				// and read-only commands run immediately mid-turn (e.g. /effort,
				// /perm, /model, /find). Commands that mutate or replace the
				// session the agent goroutine is using (/clear, /compact,
				// /resume, /rebuild, /quit, /save) are unsafe to race a running
				// turn, so they are refused with a hint to interrupt first.
				if strings.HasPrefix(task, "/") {
					name := strings.Fields(task)[0]
					if !safeWhileRunning(name) {
						m.note(name + " can't run mid-turn — press esc to interrupt first")
						return m, nil
					}
					m.recordHistory(task)
					m.ti.Reset()
					m.ti.SetHeight(1)
					m.comp = completion{kind: compNone}
					m.relayout()
					return m, m.command(task)
				}
				m.recordHistory(task)
				m.queued = append(m.queued, task)
				m.ti.Reset()
				m.ti.SetHeight(1)
				m.note(fmt.Sprintf("queued (%d): %s", len(m.queued), compact(task)))
				return m, nil
			case "ctrl+j", "alt+enter":
				m.ti.InsertString("\n")
				m.resizeInput()
				return m, nil
			case "esc":
				if m.cancel != nil {
					m.cancel()
					m.status = "interrupting…"
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.ti, cmd = m.ti.Update(msg)
			m.resizeInput()
			return m, cmd
		}
		return m, nil

	case tea.MouseMsg:
		switch {
		case msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress:
			// Right-click pastes the clipboard into the input.
			m.pasteIntoInput()
			return m, nil
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
			// A click inside the input box positions the text cursor there.
			if vrow, col, ok := m.clickInInput(msg.X, msg.Y); ok {
				m.positionCursorAt(vrow, col)
				return m, nil
			}
			// Otherwise begin a potential drag selection in the transcript; a
			// press with no motion before release is a click (block toggle).
			if p, ok := m.screenToContent(msg.X, msg.Y); ok {
				m.selecting = true
				m.dragMoved = false
				m.selAnchor = p
				m.selCursor = p
			}
			return m, nil
		case msg.Action == tea.MouseActionMotion && m.selecting:
			// Drag: extend the selection to the current cell and show it.
			if p, ok := m.screenToContent(msg.X, msg.Y); ok {
				m.selCursor = p
				m.dragMoved = true
				m.showSelection()
			}
			return m, nil
		case msg.Action == tea.MouseActionRelease && m.selecting:
			m.selecting = false
			if m.dragMoved {
				// A real drag: auto-copy the marked text to the clipboard.
				if p, ok := m.screenToContent(msg.X, msg.Y); ok {
					m.selCursor = p
				}
				text := m.selectedText()
				m.sync() // restore the styled transcript (drop the highlight)
				if strings.TrimSpace(text) != "" {
					if m.clip != nil && m.clip.Available() {
						if err := m.clip.Copy(text); err == nil {
							m.note(fmt.Sprintf("copied %d chars to clipboard", len([]rune(text))))
						} else {
							m.push(&block{kind: blockNote, isErr: true, body: sb("copy failed: " + err.Error())})
						}
					} else {
						m.push(&block{kind: blockNote, isErr: true, body: sb("no clipboard command found (set EIGEN_CLIPBOARD_CMD or install wl-copy/xclip)")})
					}
				}
				m.dragMoved = false
				return m, nil
			}
			// No motion: treat as a click that toggles the block under the cursor.
			m.toggleAtRow(msg.Y)
			return m, nil
		}
		if tea.MouseEvent(msg).IsWheel() {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
		return m, nil

	case submitMsg:
		return m, m.submit(msg.task)

	case agentEvent:
		m.renderEvent(msg.e)
		return m, nil

	case approvalMsg:
		// Auto-approve tools the user marked "always allow" this session.
		if m.approvedTools[msg.name] {
			msg.reply <- true
			return m, nil
		}
		m.pending = &msg
		m.note(fmt.Sprintf("approve %s %s ? [y]es / [n]o / [a]lways", msg.name, compact(string(msg.args))))
		m.relayout()
		return m, nil

	case turnDoneMsg:
		if msg.err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("error: " + msg.err.Error())})
			switch {
			case isOverloaded(msg.err) && m.ctx.Err() == nil:
				// Persistent overload (503 after all retries): redirect the next
				// turns to the known-good fallback model and retry this turn there.
				if m.startFailover() {
					m.cancel = nil
					m.autosave() // history is intact; the retry continues it
					m.note(fmt.Sprintf("model overloaded (Bedrock 503) — redirecting to %s for the next %d turns, then switching back", failoverModelID, failoverTurns))
					return m, m.resend()
				}
				m.note("model overloaded (Bedrock 503: capacity on the provider side) — try /model to switch, or retry shortly")
			case isRateLimit(msg.err):
				// Actionable hint for token-rate throttling (Bedrock 429).
				m.note("rate-limited (too many tokens/min). Try: /compact to shrink context, /effort low to reduce thinking tokens, or wait a moment and retry.")
			}
		}
		m.cancel = nil
		m.state = stInput
		m.status = ""
		m.ti.Focus()
		m.refreshCtx() // safe: the turn's goroutine has returned
		// Autosave so the conversation survives a crash or failed rebuild.
		m.autosave()
		// Failover window bookkeeping: count down successful turns on the
		// fallback model; when the window ends, switch back to the original.
		if msg.err == nil && m.failoverFrom != nil {
			m.failoverLeft--
			if m.failoverLeft <= 0 {
				m.endFailover()
			} else {
				m.note(fmt.Sprintf("on fallback %s — %d turn(s) until switching back", m.modelID, m.failoverLeft))
			}
		}
		// Read the answer aloud if enabled.
		if msg.err == nil && m.readAloud && m.speaker != nil {
			if ans := m.lastAssistantText(); ans != "" {
				m.speaker.Speak(ans)
			}
		}
		// Drain a queued message (steer/queue): send the next one immediately.
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			return m, m.submit(next)
		}
		m.relayout()
		return m, tea.Batch(textarea.Blink, m.scheduleIdleDream())

	case idleTickMsg:
		// Only dream if still idle on the same generation we scheduled for.
		if msg.gen != m.idleGen || m.state != stInput || m.mem == nil || !m.dreamOnIdle {
			return m, nil
		}
		return m, m.dreamCmd()

	case dreamDoneMsg:
		if len(msg.notes) > 0 && m.mem != nil {
			for _, n := range msg.notes {
				_ = m.mem.Append(n)
			}
			m.note(fmt.Sprintf("dreamt %d note(s) into project memory", len(msg.notes)))
		}
		return m, nil

	case compactDoneMsg:
		m.cancel = nil
		m.state = stInput
		m.status = ""
		m.ti.Focus()
		if msg.err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("compact failed: " + msg.err.Error())})
		} else if msg.after >= msg.before {
			m.note("nothing to compact (conversation already small)")
		} else {
			// Re-render the transcript from the compacted messages so the UI
			// matches what will be sent to the model.
			m.blocks = nil
			m.sel = -1
			renderHistory(m, m.session.Messages())
			m.refreshCtx()
			m.note(fmt.Sprintf("compacted: %d→%d messages, ~%s→~%s tokens",
				msg.before, msg.after, kfmt(msg.beforeTok), kfmt(msg.afterTok)))
		}
		m.relayout()
		return m, textarea.Blink

	case buildDoneMsg:
		if msg.err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("rebuild failed — kept the current build:")})
			detail := strings.TrimSpace(msg.out)
			if detail == "" {
				detail = msg.err.Error()
			}
			m.push(&block{kind: blockNote, isErr: true, body: sb(detail)})
			m.cancel = nil
			m.state = stInput
			m.status = ""
			m.ti.Focus()
			m.relayout()
			return m, textarea.Blink
		}
		m.rebuild = true
		m.rebuildBin = msg.bin
		return m, tea.Quit

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) renderEvent(e agent.Event) {
	switch e.Kind {
	case agent.EventTextDelta:
		m.streamedText = true
		// Real output started: collapse the live thinking block(s) for this turn.
		m.collapseThinking()
		if b := m.lastOpen(blockText); b != nil && b.role == "assistant" {
			b.body += e.Text
			m.sync()
		} else {
			m.text("assistant", e.Text)
		}
	case agent.EventReasoningDelta:
		// Stream reasoning into a live "thinking" block, shown expanded so the
		// user sees the thoughts as they arrive; it is collapsed once the turn
		// produces text or a tool call (collapseThinking).
		if b := m.lastOpen(blockThinking); b != nil {
			b.body += e.Text
			m.sync()
		} else {
			m.push(&block{kind: blockThinking, title: "thinking", collapsed: false, body: sb(e.Text)})
		}
	case agent.EventToolStart:
		// Real action started: collapse the live thinking block(s) for this turn.
		m.collapseThinking()
		// The todo tool drives the pinned plan panel instead of a tool block.
		if e.ToolName == "todo" {
			m.updateTodos(e.ToolArgs)
			m.status = "updated plan"
			return
		}
		m.status = "running " + e.ToolName
		m.push(&block{
			kind:      blockTool,
			toolName:  e.ToolName,
			toolArgs:  e.ToolArgs,
			title:     e.ToolName + " " + compact(string(e.ToolArgs)),
			collapsed: true,
			state:     toolRunning,
		})
	case agent.EventToolResult:
		if e.ToolName == "todo" {
			return // already reflected in the plan panel
		}
		// attach result to the matching open tool block (most recent)
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockTool && m.blocks[i].result == "" && m.blocks[i].state == toolRunning {
				m.blocks[i].result = e.Result
				m.blocks[i].isErr = e.IsError
				if e.IsError {
					m.blocks[i].state = toolFailed
				} else {
					m.blocks[i].state = toolDone
				}
				break
			}
		}
		m.sync()
	case agent.EventDone:
		m.status = "done"
		m.collapseThinking()
		// Show the final answer when the provider didn't stream any text this
		// turn (non-streaming, or a reasoning-only stream) — otherwise the
		// streamed assistant block already holds it.
		if !m.streamedText && strings.TrimSpace(e.Text) != "" {
			m.text("assistant", e.Text)
		}
	}
}

// collapseThinking collapses the most recent still-expanded "thinking" block —
// called when real output (text/tool/done) follows streamed reasoning, so the
// thoughts are shown live then tucked away into a one-line, expandable header.
func (m *model) collapseThinking() {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := m.blocks[i]
		if b.kind == blockThinking {
			if !b.collapsed {
				b.collapsed = true
				m.sync()
			}
			return
		}
		// Stop at the previous turn's assistant text (don't collapse older turns).
		if b.kind == blockText && b.role == "assistant" {
			return
		}
	}
}

func (m *model) View() string {
	if !m.ready {
		return "starting…"
	}
	if m.picking {
		return m.pickerView()
	}
	if m.modelPicking {
		return m.modelPickerView()
	}
	var bottom string
	switch {
	case m.pending != nil:
		bottom = styleAsk.Render("[y]es approve · [n]o deny · [a]lways allow this tool")
	case m.state == stRunning:
		// Status/spinner on its own line, with the input below so the user can
		// type a message to queue (enter) or interrupt (esc) while it runs.
		hint := dim("   enter queue · esc interrupt · alt+↑/↓ select · tab expand")
		bottom = m.sp.View() + " " + m.status + m.queuedHint() + hint + "\n" + m.ti.View()
	default:
		bottom = m.compMenuView() + m.ti.View()
	}
	return m.planView() + m.vp.View() + "\n" + bottom + "\n" + m.statusBarView()
}

// queuedHint summarizes how many messages are waiting to be sent.
func (m *model) queuedHint() string {
	if len(m.queued) == 0 {
		return ""
	}
	return styleAsk.Render(fmt.Sprintf("  [%d queued]", len(m.queued)))
}

// pickerView renders the session chooser.
func (m *model) pickerView() string {
	var b strings.Builder
	b.WriteString(styleUser.Render("resume a session") + dim("   ↑↓ move · enter open · esc cancel") + "\n\n")
	rows := m.height - 4
	if rows < 1 {
		rows = 1
	}
	// window around the selection
	start := 0
	if m.pickIdx >= rows {
		start = m.pickIdx - rows + 1
	}
	end := start + rows
	if end > len(m.picks) {
		end = len(m.picks)
	}
	for i := start; i < end; i++ {
		p := m.picks[i]
		title := p.Title
		if title == "" {
			title = dim("(untitled)")
		}
		when := time.Unix(0, p.Updated).Format("01-02 15:04")
		line := fmt.Sprintf("%s  %-7s  %s", when, p.Source, title)
		if i == m.pickIdx {
			line = styleAsk.Render("› " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// modelPickerView renders the interactive model chooser (bare /model).
func (m *model) modelPickerView() string {
	var b strings.Builder
	b.WriteString(styleUser.Render("choose a model") + dim("   ↑↓ move · enter switch · esc cancel") + "\n\n")
	rows := m.height - 4
	if rows < 1 {
		rows = 1
	}
	start := 0
	if m.modelPickIdx >= rows {
		start = m.modelPickIdx - rows + 1
	}
	end := start + rows
	if end > len(m.modelPicks) {
		end = len(m.modelPicks)
	}
	for i := start; i < end; i++ {
		mi := m.modelPicks[i]
		// Window: prefer 1M when available.
		win := mi.ContextWindow
		if mi.Context1M && mi.ContextWindow1M > 0 {
			win = mi.ContextWindow1M
		}
		winStr := ""
		if win > 0 {
			winStr = fmt.Sprintf("%dk", win/1000)
		}
		// Capability tags.
		var tags []string
		if mi.Cache {
			tags = append(tags, "cache")
		}
		if mi.Context1M {
			tags = append(tags, "1M")
		}
		if mi.Reasoning {
			if mi.Effort != "" {
				tags = append(tags, "effort:"+mi.Effort)
			} else if mi.ThinkingBudget > 0 {
				tags = append(tags, "thinking")
			}
		}
		if mi.Search {
			tags = append(tags, "search")
		}
		tagStr := ""
		if len(tags) > 0 {
			tagStr = "  [" + strings.Join(tags, " ") + "]"
		}
		active := mi.ID == m.modelID
		line := fmt.Sprintf("%-34s %-9s %-5s%s", mi.ID, mi.Provider, winStr, tagStr)
		if active {
			line = styleStatus.Render("● " + line)
		} else {
			line = "  " + line
		}
		if i == m.modelPickIdx {
			line = styleAsk.Render("› ") + strings.TrimPrefix(strings.TrimPrefix(line, "  "), styleStatus.Render("● ")[:len("● ")])
			// Re-render the whole line highlighted.
			raw := fmt.Sprintf("%-34s %-9s %-5s%s", mi.ID, mi.Provider, winStr, tagStr)
			if active {
				line = styleAsk.Render("›●" + raw)
			} else {
				line = styleAsk.Render("› " + raw)
			}
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m *model) loadSessionByID(id string) {
	if m.store == nil {
		return
	}
	msgs, err := m.store.Load(id)
	if err != nil {
		m.push(&block{kind: blockNote, isErr: true, body: sb("resume failed: " + err.Error())})
		return
	}
	m.applyResumed(msgs)
}

// loadSession resumes from a store id or a transcript file path.
func (m *model) loadSession(arg string) {
	if m.store != nil && m.store.Get(arg) != nil {
		m.loadSessionByID(arg)
		return
	}
	msgs, err := transcript.Import(arg)
	if err != nil {
		m.push(&block{kind: blockNote, isErr: true, body: sb("resume failed: " + err.Error())})
		return
	}
	m.applyResumed(msgs)
}

func (m *model) applyResumed(msgs []llm.Message) {
	m.session = m.a.Resume(msgs)
	m.blocks = nil
	m.sel = -1
	renderHistory(m, msgs)
	m.refreshCtx()
	m.note(fmt.Sprintf("— resumed %d messages —", len(msgs)))
}

func dim(s string) string { return styleReason.Render(s) }

// --- commands --------------------------------------------------------------

// modelCatalog renders the current model plus the catalog of models the user
// can switch to, marking the active one. It powers bare `/model`.
func (m *model) modelCatalog() string {
	current := ""
	if m.a != nil && m.a.Provider != nil {
		current = m.a.Provider.Name()
	}
	var b strings.Builder
	if current != "" {
		b.WriteString("model: " + current)
	} else {
		b.WriteString("no model configured")
	}
	b.WriteString("\navailable models (/model <id> or /model <provider> <id> to switch):")
	for _, mi := range llm.Models() {
		marker := "  "
		if mi.ID == m.modelID {
			marker = "› "
		}
		line := fmt.Sprintf("\n  %s%-32s %-9s", marker, mi.ID, mi.Provider)
		win := mi.ContextWindow
		if mi.Context1M && mi.ContextWindow1M > 0 {
			win = mi.ContextWindow1M
		}
		if win > 0 {
			line += fmt.Sprintf(" %dk", win/1000)
		}
		// Capability tags.
		var tags []string
		if mi.Cache {
			tags = append(tags, "cache")
		}
		if mi.Context1M {
			tags = append(tags, "1M")
		}
		if mi.Reasoning {
			if mi.Effort != "" {
				tags = append(tags, "effort:"+mi.Effort)
			} else if mi.ThinkingBudget > 0 {
				tags = append(tags, "thinking")
			} else {
				tags = append(tags, "reasoning")
			}
		}
		if len(tags) > 0 {
			line += "  [" + strings.Join(tags, " ") + "]"
		}
		b.WriteString(line)
	}
	return b.String()
}

// safeWhileRunning reports whether a slash command can run while a turn is in
// flight. Settings and read-only commands are safe; commands that replace or
// mutate the session the agent goroutine is using (or exit) are not — they must
// wait until the turn finishes (press esc to interrupt).
func safeWhileRunning(name string) bool {
	switch name {
	case "/effort", "/search", "/perm", "/model", "/help",
		"/skills", "/tools", "/find", "/copy", "/read":
		return true
	default:
		// /clear, /compact, /resume, /rebuild, /save, /export, /quit, /exit
		return false
	}
}

func (m *model) command(line string) tea.Cmd {
	fields := strings.Fields(line)
	name := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(line, name))
	switch name {
	case "/help":
		m.note("commands: /help  /resume  /save  /export  /clear  /compact  /model  /effort  /search  /perm  /skills  /tools  /find  /copy  /read  /rebuild  /quit")
		m.note("keys: / commands · @ files · ↑↓ history · select ctrl+p/n (or alt+↑/↓) · tab expand · drag select+copy · copy ctrl+y/alt+y · perm ctrl+a/alt+a · effort ctrl+e/alt+r · model ctrl+o/alt+m · pgup/pgdn scroll")
		m.note("multiplexer note: zellij/tmux capture ctrl+p/n/o — use the alt+… keys (alt+↑/↓ select, alt+m model, alt+r effort, alt+a perm, alt+y copy)")
		m.note("while running: enter queues a message · esc interrupts · settings commands (/effort /perm /model /search) run immediately")
	case "/clear":
		m.session = m.a.NewSession()
		m.blocks = nil
		m.sel = -1
		m.refreshCtx()
		m.note("— cleared —")
	case "/compact":
		if m.session == nil {
			break
		}
		m.state = stRunning
		m.status = "compacting…"
		m.relayout()
		return tea.Batch(m.sp.Tick, m.compactCmd())
	case "/save":
		path := arg
		if path == "" {
			path = defaultSessionPath()
		}
		if err := transcript.Save(path, m.session.Messages()); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("save failed: " + err.Error())})
		} else {
			m.note("saved → " + path)
		}
	case "/resume":
		if arg == "" {
			// open the picker
			if m.store == nil {
				m.push(&block{kind: blockNote, isErr: true, body: sb("no session store")})
				break
			}
			m.picks = m.store.List()
			if len(m.picks) == 0 {
				m.note("no sessions found")
				break
			}
			m.picking = true
			m.pickIdx = 0
			m.sync()
			break
		}
		m.loadSession(arg)
	case "/rebuild":
		if err := transcript.Save(m.sessionPath, m.session.Messages()); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("rebuild: save failed: " + err.Error())})
			break
		}
		m.saveMeta()
		m.state = stRunning
		m.status = "rebuilding…"
		return tea.Batch(m.sp.Tick, m.buildCmd())
	case "/quit", "/exit":
		return tea.Quit
	case "/perm":
		switch agent.Permission(arg) {
		case agent.PermGated, agent.PermAuto:
			m.a.Perm = agent.Permission(arg)
			m.note("permission posture → " + arg)
		case "":
			m.note(fmt.Sprintf("permission posture: %s  (use /perm gated|auto to change)", m.a.Perm))
		default:
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown posture " + arg + " (want gated|auto)")})
		}
	case "/effort":
		es, ok := m.a.Provider.(llm.EffortSetter)
		if !ok {
			m.note("the current model does not support a reasoning-effort setting")
			break
		}
		if arg == "" {
			m.note(fmt.Sprintf("reasoning effort: %s   (/effort %s)", es.Effort(), strings.Join(llm.EffortLevels, "|")))
			break
		}
		if !es.SetEffort(arg) {
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown effort " + arg + " (want " + strings.Join(llm.EffortLevels, "|") + ")")})
			break
		}
		m.note("reasoning effort → " + es.Effort())
	case "/search":
		sr, ok := m.a.Provider.(llm.Searcher)
		if !ok {
			m.note("the current model does not support live search (grok only)")
			break
		}
		if arg == "" {
			m.note(fmt.Sprintf("live search: %s   (/search off|auto|on)", sr.SearchMode()))
			break
		}
		if !sr.SetSearch(arg) {
			m.push(&block{kind: blockNote, isErr: true, body: sb("unknown search mode " + arg + " (want off|auto|on)")})
			break
		}
		m.note("live search → " + sr.SearchMode())
	case "/model":
		if arg == "" {
			m.modelPicks = llm.Models()
			// Pre-select the currently active model.
			m.modelPickIdx = 0
			for i, mi := range m.modelPicks {
				if mi.ID == m.modelID {
					m.modelPickIdx = i
					break
				}
			}
			m.modelPicking = true
			m.sync()
			break
		}
		if m.newProvider == nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("model switching unavailable")})
			break
		}
		// Resolve provider + model. Forms:
		//   /model <provider> <id>   explicit provider
		//   /model <id>              provider inferred from the catalog, else
		//                            the current provider.
		prov, id := m.provName, arg
		if fs := strings.Fields(arg); len(fs) >= 2 {
			prov, id = fs[0], fs[1]
		}
		// Reconcile against the catalog so a known model never goes to the wrong
		// backend (e.g. /model us.anthropic.claude-opus-4-8 while on mantle).
		prov = llm.ResolveProvider(prov, id)
		np, perr := m.newProvider(prov, id)
		if perr != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("switch failed: " + perr.Error())})
			break
		}
		m.a.Provider = np
		m.a.Compactor = llm.NewCompactor(np)
		m.provName, m.modelID = prov, id
		// Auto-detect the new model's context budget from the catalog (capped,
		// like main's auto budget, so 1M windows don't exceed minute quotas).
		if w := llm.EffectiveContextWindow(id); w > 0 {
			m.a.MaxContextTokens = contextBudgetFor(w)
		}
		// A manual switch takes precedence over any overload failover window.
		m.failoverFrom = nil
		m.failoverLeft = 0
		m.note("model → " + np.Name())
	case "/skills":
		if m.skills == nil || m.skills.Len() == 0 {
			m.note("no skills discovered (see --list-skills; add SKILL.md under ~/.eigen/skills or .eigen/skills)")
			break
		}
		// /skills <name> previews that skill's full body; bare /skills lists them.
		if arg != "" {
			body, err := m.skills.Body(arg)
			if err != nil {
				m.push(&block{kind: blockNote, isErr: true, body: sb(err.Error())})
				break
			}
			sk, _ := m.skills.Get(arg)
			m.push(&block{
				kind:      blockThinking, // reuse the collapsible block for a scrollable preview
				title:     "skill: " + arg,
				collapsed: false,
				body:      sb(strings.TrimSpace(sk.Description + "\n\n" + body)),
			})
			break
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("%d skill(s) — /skills <name> to preview, or let the model load one automatically:", m.skills.Len()))
		for _, sk := range m.skills.List() {
			b.WriteString("\n  • " + sk.Name)
			if d := firstLineOf(sk.Description); d != "" {
				b.WriteString(" — " + d)
			}
		}
		m.note(b.String())
	case "/tools":
		if m.a.Tools == nil {
			m.note("no tools")
			break
		}
		var b strings.Builder
		b.WriteString("tools:")
		for _, d := range m.a.Tools.Definitions() {
			posture := "·"
			if !d.ReadOnly {
				posture = "✎"
			}
			b.WriteString("\n  " + posture + " " + d.Name)
		}
		m.note(b.String())
	case "/read":
		if m.speaker == nil || !m.speaker.Available() {
			m.push(&block{kind: blockNote, isErr: true, body: sb("no TTS command found (set EIGEN_TTS_CMD or install espeak-ng)")})
			break
		}
		m.readAloud = !m.readAloud
		if m.readAloud {
			m.note("read-aloud on — assistant answers will be spoken")
		} else {
			m.speaker.Stop()
			m.note("read-aloud off")
		}
	case "/copy":
		if m.clip == nil || !m.clip.Available() {
			m.push(&block{kind: blockNote, isErr: true, body: sb("no clipboard command found (set EIGEN_CLIPBOARD_CMD or install wl-copy/xclip)")})
			break
		}
		text := m.copyTarget()
		if text == "" {
			m.note("nothing to copy")
			break
		}
		if err := m.clip.Copy(text); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("copy failed: " + err.Error())})
		} else {
			m.note("copied to clipboard")
		}
	case "/export":
		path := arg
		if path == "" {
			path = defaultExportPath()
		}
		if err := os.WriteFile(path, []byte(sessionMarkdown(m.session.Messages())), 0o644); err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("export failed: " + err.Error())})
		} else {
			m.note("exported → " + path)
		}
	case "/find":
		if arg == "" {
			m.note("usage: /find <text>")
			break
		}
		matches := m.findBlocks(arg)
		if len(matches) == 0 {
			m.note("no matches for " + arg)
			break
		}
		// Note first: push resets the selection, so select after noting.
		m.note(fmt.Sprintf("%d match(es) for %q — showing the first", len(matches), arg))
		m.sel = matches[0]
		if m.blocks[m.sel].collapsible() {
			m.blocks[m.sel].collapsed = false
		}
		m.sync()
		m.scrollToSelected()
	default:
		m.push(&block{kind: blockNote, isErr: true, body: sb("unknown command " + name + " (try /help)")})
	}
	return nil
}

func defaultSessionPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".eigen", "sessions")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, time.Now().Format("20060102-150405")+".eigen.jsonl")
}

// defaultExportPath is where /export writes a markdown transcript by default.
func defaultExportPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".eigen", "exports")
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, time.Now().Format("20060102-150405")+".md")
}

// sessionMarkdown renders a conversation as a readable markdown transcript.
func sessionMarkdown(msgs []llm.Message) string {
	var b strings.Builder
	b.WriteString("# eigen session\n\n")
	for _, msg := range msgs {
		switch msg.Role {
		case llm.RoleUser:
			if t := strings.TrimSpace(msg.Text); t != "" {
				b.WriteString("## You\n\n" + t + "\n\n")
			}
		case llm.RoleAssistant:
			if t := strings.TrimSpace(msg.Text); t != "" {
				b.WriteString("## eigen\n\n" + t + "\n\n")
			}
			for _, tc := range msg.ToolCalls {
				b.WriteString("> tool `" + tc.Name + "` " + compact(string(tc.Arguments)) + "\n\n")
			}
		case llm.RoleTool:
			if t := strings.TrimSpace(msg.Text); t != "" {
				b.WriteString("```\n" + t + "\n```\n\n")
			}
		}
	}
	return b.String()
}

// buildCmd rebuilds eigen to a staging binary, smoke-tests it, and only on
// success atomically swaps it into place — so a broken build never replaces the
// working binary or kills the session. Failures are reported back via buildDoneMsg.
func (m *model) buildCmd() tea.Cmd {
	src := m.srcDir
	return func() tea.Msg {
		bin := filepath.Join(src, "bin", "eigen")
		staging := bin + ".new"

		build := exec.Command("go", "build", "-o", staging, ".")
		build.Dir = src
		if out, err := build.CombinedOutput(); err != nil {
			return buildDoneMsg{err: fmt.Errorf("build failed"), out: string(out)}
		}
		// Smoke test: the new binary must at least run --version cleanly.
		smoke := exec.Command(staging, "--version")
		if out, err := smoke.CombinedOutput(); err != nil {
			os.Remove(staging)
			return buildDoneMsg{err: fmt.Errorf("smoke test failed: %v", err), out: string(out)}
		}
		if err := os.Rename(staging, bin); err != nil {
			os.Remove(staging)
			return buildDoneMsg{err: fmt.Errorf("swap failed: %w", err)}
		}
		return buildDoneMsg{bin: bin}
	}
}

func eigenSrcDir() string {
	if s := os.Getenv("EIGEN_SRC"); s != "" {
		return s
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "projects", "eigen")
}

// Options configures a TUI run.
type Options struct {
	InitialTask string
	History     []llm.Message
	Store       *session.Store
	Provider    string // provider name (for live /model switch)
	Model       string // model id
	Memory      *memory.Store
	Skills      *skill.Set // discovered skills (for /skills browse + preview)
	DreamOnIdle bool // reflect into memory after the session goes idle
	IdleMinutes int  // idle delay before dreaming (default 5)
}

// Run drives the agent under a multi-turn Bubble Tea REPL.
func Run(a *agent.Agent, o Options) (Result, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initialTask := o.InitialTask
	history := o.History
	store := o.Store

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ti := textarea.New()
	ti.Placeholder = "type a task…  (enter send · ctrl+j newline · / commands · ↑↓ history · ctrl+c quit)"
	ti.Prompt = "│ "
	ti.ShowLineNumbers = false
	ti.CharLimit = 0
	ti.MaxHeight = inputMaxRows
	ti.SetHeight(1)
	// Flat look inside the box: no cursor-line background highlight.
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.BlurredStyle.CursorLine = lipgloss.NewStyle()
	// A rounded border draws the input as a box; the accent color when focused,
	// dim when blurred. The prompt caret and placeholder pick up the palette.
	ti.FocusedStyle.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent)
	ti.BlurredStyle.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238"))
	ti.FocusedStyle.Prompt = styleAccent
	ti.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	ti.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	// Enter is reserved for submit; newlines are inserted with ctrl+j / alt+enter
	// (handled in Update). Disabling the textarea's own newline binding stops it
	// from inserting a line break on Enter.
	ti.KeyMap.InsertNewline.SetEnabled(false)
	ti.Focus()

	session := a.NewSession()
	if len(history) > 0 {
		session = a.Resume(history)
	}

	m := &model{
		a:           a,
		sp:          sp,
		ti:          ti,
		session:     session,
		ctx:         ctx,
		state:       stInput,
		initialTask: initialTask,
		srcDir:      eigenSrcDir(),
		sessionPath: defaultSessionPath(),
		store:       store,
		speaker:     speech.Detect(),
		clip:        clipboard.Detect(),
		provName:    o.Provider,
		modelID:     o.Model,
		newProvider: llm.New,
		mem:         o.Memory,
		skills:      o.Skills,
		dreamOnIdle: o.DreamOnIdle,
		idleMinutes: o.IdleMinutes,
	}
	if m.idleMinutes <= 0 {
		m.idleMinutes = 5
	}
	if len(history) > 0 {
		renderHistory(m, history)
		m.note(fmt.Sprintf("— resumed %d messages —", len(history)))
	}
	m.refreshCtx()

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	a.OnEvent = func(e agent.Event) { p.Send(agentEvent{e}) }
	// Continuous, race-free autosave: persist runs in the agent goroutine after
	// every message, so a crash or kill mid-turn still leaves a complete JSONL.
	a.Persist = func(msgs []llm.Message) {
		_ = transcript.Save(m.sessionPath, msgs)
		m.saveMeta()
	}
	a.Approve = func(ctx context.Context, name string, args json.RawMessage) (bool, error) {
		reply := make(chan bool, 1)
		p.Send(approvalMsg{name: name, args: args, reply: reply})
		select {
		case ok := <-reply:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}

	final, err := p.Run()
	if err != nil {
		return Result{}, err
	}
	fm := final.(*model)
	return Result{
		Rebuild:     fm.rebuild,
		SessionPath: fm.sessionPath,
		BinPath:     fm.rebuildBin,
		Provider:    fm.provName,
		Model:       fm.modelID,
		Perm:        string(fm.a.Perm),
		Effort:      liveEffort(fm.a.Provider),
		Search:      liveSearch(fm.a.Provider),
	}, nil
}

// liveEffort returns the provider's current reasoning-effort label, or "" when
// the provider has no effort setting.
func liveEffort(p llm.Provider) string {
	if es, ok := p.(llm.EffortSetter); ok {
		return es.Effort()
	}
	return ""
}

// liveSearch returns the provider's current live-search mode, or "" when the
// provider has no search setting.
func liveSearch(p llm.Provider) string {
	if sr, ok := p.(llm.Searcher); ok {
		return sr.SearchMode()
	}
	return ""
}

func compact(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// firstLineOf returns the first non-empty line of s, trimmed and truncated, for
// compact one-line skill descriptions in the /skills list.
func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 100 {
		s = s[:100] + "…"
	}
	return s
}

// isRateLimit reports whether err looks like a provider rate-limit / throttle
// (HTTP 429 or a "too many tokens" message), so the UI can suggest /compact.
func isRateLimit(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "429") ||
		strings.Contains(s, "too many tokens") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, "throttl")
}

// isOverloaded reports whether err looks like a persistent provider overload
// (HTTP 503 / "unable to process" after all retries) — capacity on the model
// side, where switching models helps and retrying the same one usually doesn't.
func isOverloaded(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "503") ||
		strings.Contains(s, "service unavailable") ||
		strings.Contains(s, "unable to process") ||
		strings.Contains(s, "overloaded")
}

// failoverOrigin remembers where to switch back to after an overload failover.
type failoverOrigin struct {
	provider, model string
}

// failoverTurns is how many turns run on the fallback model after a persistent
// overload before switching back to the original model.
const failoverTurns = 5

// failoverModelID is the known-good model used while the primary is overloaded.
const failoverModelID = "us.anthropic.claude-opus-4-8"

// startFailover switches the live provider to the fallback model for
// failoverTurns turns, remembering the origin. Returns false when failover is
// not applicable (already on the fallback, no constructor, or switch failed).
func (m *model) startFailover() bool {
	if m.newProvider == nil || m.modelID == failoverModelID || m.failoverFrom != nil {
		return false
	}
	prov := llm.ResolveProvider(m.provName, failoverModelID)
	np, err := m.newProvider(prov, failoverModelID)
	if err != nil {
		return false
	}
	m.failoverFrom = &failoverOrigin{provider: m.provName, model: m.modelID}
	m.failoverLeft = failoverTurns
	m.a.Provider = np
	m.a.Compactor = llm.NewCompactor(np)
	m.provName, m.modelID = prov, failoverModelID
	if w := llm.EffectiveContextWindow(failoverModelID); w > 0 {
		m.a.MaxContextTokens = contextBudgetFor(w)
	}
	return true
}

// endFailover switches back to the original model after the failover window.
// Best-effort: if the original cannot be constructed, stay on the fallback.
func (m *model) endFailover() {
	if m.failoverFrom == nil || m.newProvider == nil {
		return
	}
	orig := *m.failoverFrom
	np, err := m.newProvider(orig.provider, orig.model)
	if err != nil {
		m.note("failover: could not switch back to " + orig.model + " (" + err.Error() + ") — staying on " + m.modelID)
		m.failoverFrom = nil
		m.failoverLeft = 0
		return
	}
	m.a.Provider = np
	m.a.Compactor = llm.NewCompactor(np)
	m.provName, m.modelID = orig.provider, orig.model
	if w := llm.EffectiveContextWindow(orig.model); w > 0 {
		m.a.MaxContextTokens = contextBudgetFor(w)
	}
	m.failoverFrom = nil
	m.failoverLeft = 0
	m.note("overload window over — switched back to " + orig.model)
}

// contextBudgetFor mirrors main's auto budget: 85% of the window, capped so a
// huge (1M) window can't exceed per-minute token quotas.
func contextBudgetFor(window int) int {
	b := window * 85 / 100
	if b > 200_000 {
		b = 200_000
	}
	return b
}

// renderHistory pre-fills the transcript with resumed messages as blocks, so
// the user sees the conversation being continued — thinking and tool blocks
// start collapsed and can be expanded.
func renderHistory(m *model, history []llm.Message) {
	for _, msg := range history {
		switch msg.Role {
		case llm.RoleUser:
			if msg.Text != "" {
				m.text("user", msg.Text)
			}
		case llm.RoleAssistant:
			if msg.Reasoning != "" {
				m.push(&block{kind: blockThinking, title: "thinking", collapsed: true, body: sb(msg.Reasoning)})
			}
			if msg.Text != "" {
				m.text("assistant", msg.Text)
			}
			for _, tc := range msg.ToolCalls {
				m.push(&block{
					kind:      blockTool,
					toolName:  tc.Name,
					toolArgs:  tc.Arguments,
					title:     tc.Name + " " + compact(string(tc.Arguments)),
					collapsed: true,
					state:     toolDone,
				})
			}
		case llm.RoleTool:
			for i := len(m.blocks) - 1; i >= 0; i-- {
				if m.blocks[i].kind == blockTool && m.blocks[i].result == "" && m.blocks[i].state != toolFailed {
					m.blocks[i].result = msg.Text
					m.blocks[i].isErr = msg.ToolError
					if msg.ToolError {
						m.blocks[i].state = toolFailed
					}
					break
				}
			}
		}
	}
}

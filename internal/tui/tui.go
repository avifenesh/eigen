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
	styleInlineCode = lipgloss.NewStyle().Foreground(lipgloss.Color("80")) // teal
	styleQuote      = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	styleBullet     = lipgloss.NewStyle().Foreground(lipgloss.Color("141")) // violet
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
	mem            *memory.Store
	dreamOnIdle    bool
	idleMinutes    int
	maxTokens      int           // user context-budget ceiling (0 = auto from the model window)
	smallCompactor llm.Compactor // cheap-model summarizer chained on live switches

	// ping: attention signals (terminal bell + optional notifier command).
	notifyCmd   string    // external notifier (config notify_cmd / EIGEN_NOTIFY_CMD)
	turnStarted time.Time // when the running turn began (zero when idle)
	idleGen     int       // bumped on each turn; stale idle ticks are ignored

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

	// ctxNudged is set once usage crosses the proactive-compaction threshold so
	// the "context is filling up" hint fires only once per fill cycle (it resets
	// when usage falls back below the threshold, e.g. after /compact).
	ctxNudged bool

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
		meta.Goal = m.a.CurrentGoal()
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

// --- update ----------------------------------------------------------------

func (m *model) submit(task string) tea.Cmd {
	m.text("user", task)
	m.state = stRunning
	m.status = "thinking"
	m.turnStarted = time.Now()
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
	m.turnStarted = time.Now()
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
			m.vp.HalfPageUp()
			return m, nil
		case "pgdown":
			m.vp.HalfPageDown()
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
		m.ping("approval needed: " + msg.name)
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
		m.pingOnTurnDone(msg.err)
		m.turnStarted = time.Time{}
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

// --- commands --------------------------------------------------------------

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
	DreamOnIdle bool       // reflect into memory after the session goes idle
	IdleMinutes int        // idle delay before dreaming (default 5)
	MaxTokens   int        // user context-budget ceiling (0 = auto from the model window)
	// SmallCompactor, when set, summarizes compactions on a cheap small model;
	// live model switches chain it before the new main provider's compactor.
	SmallCompactor llm.Compactor
	// NotifyCmd is an external notifier command (e.g. "notify-send") run with
	// the ping message appended; empty = bell only (EIGEN_NOTIFY_CMD also works).
	NotifyCmd string
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
		a:              a,
		sp:             sp,
		ti:             ti,
		session:        session,
		ctx:            ctx,
		state:          stInput,
		initialTask:    initialTask,
		srcDir:         eigenSrcDir(),
		sessionPath:    defaultSessionPath(),
		store:          store,
		speaker:        speech.Detect(),
		clip:           clipboard.Detect(),
		provName:       o.Provider,
		modelID:        o.Model,
		newProvider:    llm.New,
		mem:            o.Memory,
		skills:         o.Skills,
		dreamOnIdle:    o.DreamOnIdle,
		idleMinutes:    o.IdleMinutes,
		maxTokens:      o.MaxTokens,
		smallCompactor: o.SmallCompactor,
		notifyCmd:      o.NotifyCmd,
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

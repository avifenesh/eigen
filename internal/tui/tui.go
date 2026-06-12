// Package tui renders an eigen session with Bubble Tea: a multi-turn REPL with
// a scrolling transcript of streamed model output, collapsible thinking and
// tool blocks, an input box, and inline gated approvals. It consumes the agent
// event sink.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/chat"
	"github.com/avifenesh/eigen/internal/clipboard"
	"github.com/avifenesh/eigen/internal/dream"
	"github.com/avifenesh/eigen/internal/hook"
	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/memory"
	"github.com/avifenesh/eigen/internal/session"
	"github.com/avifenesh/eigen/internal/skill"
	"github.com/avifenesh/eigen/internal/speech"
	"github.com/avifenesh/eigen/internal/transcript"
	"github.com/avifenesh/eigen/internal/voice"
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

// pendingApproval is a gated tool call awaiting the user's verdict, surfaced
// as an EventApproval through the backend's event stream and answered by id
// via backend.Answer (one path for local and daemon-hosted sessions).
type pendingApproval struct {
	id   string
	name string
	args string
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
	backend chat.Backend
	ctx     context.Context

	blocks  []*block
	sel     int // index of the selected block (-1 = none / following tail)
	state   uiState
	pending *pendingApproval
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

	// in-session config panel (bare /config) — live editable settings
	conf confPanel

	// session switcher (alt+s / /sessions): hop this window to another daemon
	// session, or back to the app. Quits the program with Result.SwitchTo /
	// Result.OpenApp set; main re-runs the TUI on the target — the window is a
	// VIEW, the sessions keep running in the daemon.
	switching     bool
	switchEntries []chat.SessionEntry
	switchIdx     int
	switchTo      string
	openApp       bool

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

	// voice conversation mode: STT for spoken input, TTS for spoken replies.
	stt         voice.STT
	tts         voice.TTS
	voiceOn     bool               // conversation mode active
	voiceCancel context.CancelFunc // cancels in-flight TTS (interrupt)

	// pendingImages are clipboard/paste-staged images attached to the next
	// message (vision models only).
	pendingImages []llm.Image

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
	router         Router        // opt-in auto-router (nil when unavailable)
	eventWrap      func(agent.EventSink) agent.EventSink
	hooks          *hook.Runner

	// ping: attention signals (terminal bell + optional notifier command).
	notifyCmd   string    // external notifier (config notify_cmd / EIGEN_NOTIFY_CMD)
	turnStarted time.Time // when the running turn began (zero when idle)

	// loop: a prompt re-submitted every loopEvery while idle, until cleared.
	loopPrompt string
	loopEvery  time.Duration
	loopRuns   int

	// throughput: streamed output chars this turn (text + reasoning deltas)
	// and the last completed turn's tokens-out + tok/s for the status bar.
	turnOutChars int
	lastOutToks  int
	lastInToks   int // provider-reported input tokens (last turn; 0 = unknown)
	turnInToks   int // provider-reported usage for the CURRENT turn (EventDone)
	turnOutToks  int
	lastTokRate  float64
	idleGen      int // bumped on each turn; stale idle ticks are ignored

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

	// ov is a lightweight bottom-line overlay (confirm / text prompt) used by
	// clickable chrome actions that must not fire silently (perm change,
	// compact, rename). Inactive when ov.active is false.
	ov overlay

	// Left session rail (Tier 9 Wave 3): railOn toggles the persistent column
	// of daemon sibling sessions down the left of the transcript; railEntries
	// is the last polled snapshot. Only shown for daemon-hosted backends on a
	// wide-enough terminal.
	railOn      bool
	railEntries []chat.SessionEntry

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
	// SwitchTo / OpenApp: in-window navigation. SwitchTo names a daemon
	// session to hop this window to; OpenApp returns to the app shell. The
	// session that was showing keeps running in the daemon (detached, not
	// interrupted).
	SwitchTo string
	OpenApp  bool
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
	// Seed + start the left session rail (daemon-hosted backends only; nil for
	// local chats, so it costs nothing there).
	m.refreshRail()
	if c := m.railTick(); c != nil {
		cmds = append(cmds, c)
	}
	if m.initialTask != "" {
		task := m.initialTask
		cmds = append(cmds, func() tea.Msg { return submitMsg{task} })
	}
	// A resumed session may carry a goal or loop: arm them from the start.
	if c := m.scheduleGoalNag(); c != nil {
		cmds = append(cmds, c)
	}
	if c := m.scheduleLoop(); c != nil {
		m.note(fmt.Sprintf("loop resumed: every %s → %s   (/loop clear to stop)", m.loopEvery, m.loopPrompt))
		cmds = append(cmds, c)
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

// sessionPathFor picks the local transcript path ("" = no local autosave).
func sessionPathFor(o Options) string {
	if o.NoSessionFile {
		return ""
	}
	return defaultSessionPath()
}

// autosave persists the current conversation to the session file. It is safe
// to call repeatedly and never panics: a save failure must not crash the UI.
// It also writes a sidecar meta file recording the live config (provider,
// model, perm, effort, search) so a plain restart/--resume continues exactly as
// the conversation was.
func (m *model) autosave() {
	if m == nil || m.sessionPath == "" || m.backend == nil {
		return
	}
	defer func() { _ = recover() }()
	_ = transcript.Save(m.sessionPath, m.backend.Messages())
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
	if wd, err := os.Getwd(); err == nil {
		meta.Dir = wd
	}
	// During an overload failover window, persist the ORIGINAL model: the
	// fallback is temporary, and a restart should resume on the user's choice
	// (failing over again if the model is still overloaded).
	if m.failoverFrom != nil {
		meta.Provider = m.failoverFrom.provider
		meta.Model = m.failoverFrom.model
	}
	if m.backend != nil {
		meta.Perm = string(m.backend.Perm())
		meta.Effort = m.backend.Effort()
		meta.Search = m.backend.SearchMode()
		meta.Goal = m.backend.Goal()
		meta.Title = m.backend.Title()
	}
	if m.loopPrompt != "" {
		meta.LoopPrompt = m.loopPrompt
		meta.LoopEvery = m.loopEvery.String()
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
	if m.mem == nil || m.backend == nil {
		return nil
	}
	prov := m.backend.Provider()
	if prov == nil {
		// Daemon session: the provider lives in the daemon. Dreaming is a
		// read-only distillation, so build a throwaway provider here.
		p, err := llm.New("", m.modelID)
		if err != nil {
			return nil
		}
		prov = p
	}
	convo := dream.RenderSession(m.backend.Messages())
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
// An explicit /compact should shrink MEANINGFULLY regardless of how far the
// conversation is from the budget (a 262k convo under a 500k ceiling should
// still compact), so it targets a fraction of the CURRENT size, not the budget.
func (m *model) compactCmd() tea.Cmd {
	beforeTok := m.backend.Tokens()
	target := beforeTok * 45 / 100 // aim to roughly halve the live context
	if target < 8000 {
		target = 8000 // don't try to shrink an already-tiny conversation
	}
	sess := m.backend
	return func() tea.Msg {
		before, after, err := sess.Compact(context.Background(), target)
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
	m.turnOutChars = 0
	m.comp = completion{kind: compNone}
	m.streamedText = false
	m.idleGen++ // invalidate any pending idle-dream timer

	// Routing is ORCHESTRATOR-DRIVEN, not static: the top-level turn always
	// stays on the user's chosen model (fable by default), which acts as the
	// orchestrator — it decides per delegation (task tool kind/difficulty)
	// what routes where. The ONE top-level exception is a capability need: an
	// image attached while the active model lacks vision forces a route to a
	// vision-capable model — the alternative is silently dropping the image.
	hasImageRef := referencesImage(task) || len(m.pendingImages) > 0
	needVision := hasImageRef && !llm.HasVision(m.modelID)
	if m.router != nil && needVision && m.failoverFrom == nil {
		if prov, model, label := m.router.Route(m.ctx, task, "", "", hasImageRef); prov != nil && model != m.modelID {
			m.backend.SetModel(prov, m.compactorFor(prov), m.contextBudgetFor(model))
			m.provName, m.modelID = prov.Name(), model
			m.note(label + " (vision needed)")
		}
	}

	// Vision: attach referenced image files when the active model supports it
	// (model-id capability check — works for daemon sessions too, where no
	// live provider handle exists in the view).
	var images []llm.Image
	if m.backend != nil && llm.HasVision(m.modelID) {
		imgs, notes := extractImages(task)
		images = imgs
		for _, n := range notes {
			m.note(n)
		}
		if len(images) > 0 {
			m.note(fmt.Sprintf("attached %d image(s)", len(images)))
		}
	}
	// Prepend any clipboard-staged images (independent of path references).
	if len(m.pendingImages) > 0 {
		images = append(append([]llm.Image(nil), m.pendingImages...), images...)
		m.pendingImages = nil
	}
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
		_, err := m.backend.Send(tctx, task, images)
		return turnDoneMsg{err: err}
	})
}

// resend re-drives the current turn (history already holds the user message)
// after a failover switched the provider — the turn resumes where it stopped.
func (m *model) resend() tea.Cmd {
	m.state = stRunning
	m.status = "retrying on " + m.modelID
	m.turnStarted = time.Now()
	m.turnOutChars = 0
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
		_, err := m.backend.Resend(tctx)
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
			m.conf = confPanel{}
			m.ov = overlay{}
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
		// Lightweight overlay (confirm / rename prompt) captures keys first.
		if m.ov.active {
			if cmd, handled := m.overlayKey(msg.String()); handled {
				return m, cmd
			}
		}
		// Session switcher captures keys while open.
		if m.switching {
			switch msg.String() {
			case "up", "ctrl+p", "alt+up", "shift+up", "k":
				if m.switchIdx > 0 {
					m.switchIdx--
				}
			case "down", "ctrl+n", "alt+down", "shift+down", "j":
				if m.switchIdx < len(m.switchEntries)-1 {
					m.switchIdx++
				}
			case "enter":
				sel := m.switchEntries[m.switchIdx]
				m.switching = false
				if sl, ok := m.backend.(chat.SessionLister); ok && sel.ID == sl.SessionID() {
					m.sync() // already here
					return m, nil
				}
				m.switchTo = sel.ID
				return m, tea.Quit
			case "h":
				m.switching = false
				m.openApp = true
				return m, tea.Quit
			case "esc", "q":
				m.switching = false
				m.sync()
			}
			return m, nil
		}
		// Session picker captures keys while open.
		if m.picking {
			switch msg.String() {
			case "up", "ctrl+p", "alt+up", "shift+up", "k":
				if m.pickIdx > 0 {
					m.pickIdx--
				}
			case "down", "ctrl+n", "alt+down", "shift+down", "j":
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
		// Config panel (bare /config) captures keys while open.
		if m.conf.active {
			m.confPanelKey(msg.String())
			return m, nil
		}
		// Model picker (bare /model) captures keys while open.
		if m.modelPicking {
			switch msg.String() {
			case "up", "ctrl+p", "alt+up", "shift+up", "k":
				if m.modelPickIdx > 0 {
					m.modelPickIdx--
				}
			case "down", "ctrl+n", "alt+down", "shift+down", "j":
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
				m.backend.Answer(m.pending.id, true)
				m.note("approved")
				m.pending = nil
				m.relayout()
			case "a":
				// Always allow this tool for the rest of the session.
				if m.approvedTools == nil {
					m.approvedTools = map[string]bool{}
				}
				m.approvedTools[m.pending.name] = true
				m.backend.Answer(m.pending.id, true)
				m.note("always allowing " + m.pending.name + " this session")
				m.pending = nil
				m.relayout()
			case "n", "esc":
				m.backend.Answer(m.pending.id, false)
				m.note("denied")
				m.pending = nil
				m.relayout()
			}
			return m, nil
		}
		// Autocomplete menu (slash commands / @file) captures nav + select keys.
		if m.comp.active() {
			switch msg.String() {
			case "up", "ctrl+p", "alt+up", "shift+up":
				if m.comp.idx > 0 {
					m.comp.idx--
				}
				return m, nil
			case "down", "ctrl+n", "alt+down", "shift+down":
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
		case "ctrl+p", "alt+up", "alt+k", "shift+up":
			m.moveSel(-1)
			return m, nil
		case "ctrl+n", "alt+down", "alt+j", "shift+down":
			m.moveSel(1)
			return m, nil
		case "tab", "shift+tab":
			m.toggleSel()
			return m, nil
		case "ctrl+y", "alt+y":
			m.copySelected()
			return m, nil
		case "alt+s":
			// In-window session switcher: hop this window to another daemon
			// session (or home to the app) without touching running turns.
			m.openSwitcher()
			return m, nil
		case "ctrl+v", "alt+v":
			// Explicit image paste: grab an image from the clipboard and stage
			// it for the next message (text paste is handled by the textarea).
			m.pasteImage()
			return m, nil
		case "ctrl+t", "alt+t":
			// Push-to-talk: record a spoken utterance and submit it.
			if m.voiceOn && m.state == stInput {
				return m, m.listen()
			}
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
			// Bracketed paste / drag-drop: when the payload is dropped file
			// path(s), normalize (strip file://, unquote, percent-decode) and
			// insert as clean path tokens the model reads like an @file mention.
			if msg.Paste && len(msg.Runes) > 0 {
				if norm := normalizeDropped(string(msg.Runes)); norm != string(msg.Runes) {
					m.ti.InsertString(norm)
					m.resizeInput()
					m.refreshCompletion()
					return m, nil
				}
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
			// Chrome (status bar / header) is clickable: dispatch the segment's
			// action through the same validated path as keys. Chrome rows are
			// not draggable, so acting on press is safe.
			if h := m.hitTest(msg.X, msg.Y); h.action != actNone {
				return m, m.dispatch(h.action)
			} else if h.region == regLeftRail {
				// A click on a session rail row hops the window there (same path
				// as the switcher's enter — Detach keeps the daemon turn alive).
				if idx := m.railRowAt(h.localY); idx >= 0 && idx < len(m.railEntries) {
					return m, m.hopToSession(m.railEntries[idx].ID)
				}
				return m, nil
			}
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
		if msg.e.Kind == agent.EventApproval {
			// Auto-approve tools the user marked "always allow" this session.
			if m.approvedTools[msg.e.ToolName] {
				m.backend.Answer(msg.e.Result, true)
				return m, nil
			}
			args := strings.TrimSpace(strings.TrimPrefix(msg.e.Text, msg.e.ToolName))
			m.pending = &pendingApproval{id: msg.e.Result, name: msg.e.ToolName, args: args}
			m.note(fmt.Sprintf("approve %s %s ? [y]es / [n]o / [a]lways", msg.e.ToolName, compact(args)))
			m.ping("approval needed: " + msg.e.ToolName)
			m.relayout()
			return m, nil
		}
		m.renderEvent(msg.e)
		return m, nil

	case voiceSpokenMsg:
		m.status = ""
		if msg.err != nil {
			m.note("voice: " + msg.err.Error())
			return m, nil
		}
		if msg.text == "" {
			m.note("voice: nothing heard")
			return m, nil
		}
		// Submit the transcript as a normal turn (spoken input → message).
		return m, m.submit(msg.text)

	case turnDoneMsg:
		if msg.err != nil {
			m.push(&block{kind: blockNote, isErr: true, body: sb("error: " + msg.err.Error())})
			switch {
			case isGPTRoutingError(m.modelID, msg.err) && m.ctx.Err() == nil:
				// GPT routing/availability failure: walk the failover chain
				// (fable default → gpt-5.5 → opus); never land on the failing
				// model itself.
				if fb := nextFailover(m.modelID); m.startFailover(fb) {
					m.cancel = nil
					m.autosave()
					m.note(fmt.Sprintf("%s routing error — falling back to %s", m.failoverFrom.model, fb))
					return m, m.resend()
				}
			case isOverloaded(msg.err) && m.ctx.Err() == nil:
				// Persistent overload (503 after all retries): redirect the next
				// turns to the next model in the failover chain and retry there.
				if fb := nextFailover(m.modelID); m.startFailover(fb) {
					m.cancel = nil
					m.autosave() // history is intact; the retry continues it
					m.note(fmt.Sprintf("model overloaded (Bedrock 503) — redirecting to %s for the next %d turns, then switching back", fb, failoverTurns))
					return m, m.resend()
				}
				m.note("model overloaded (Bedrock 503: capacity on the provider side) — try /model to switch, or retry shortly")
			case isRateLimit(msg.err):
				// Actionable hint for token-rate throttling (Bedrock 429).
				m.note("rate-limited (too many tokens/min). Try: /compact to shrink context, /effort low to reduce thinking tokens, or wait a moment and retry.")
			}
		}
		m.pingOnTurnDone(msg.err)
		m.finishTurnStats()
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
		return m, tea.Batch(textarea.Blink, m.scheduleIdleDream(), m.scheduleGoalNag(), m.scheduleLoop())

	case goalNagMsg:
		return m, m.handleGoalNag(msg)

	case loopMsg:
		return m, m.handleLoop(msg)

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
		} else if msg.afterTok >= msg.beforeTok {
			// Judge by TOKENS, not message count: compaction can shed tool-
			// result payloads (shrinking tokens) without removing messages, so
			// a count-based check wrongly reports "nothing to compact".
			m.note("nothing to compact (conversation already small)")
		} else {
			// Re-render the transcript from the compacted messages so the UI
			// matches what will be sent to the model.
			m.blocks = nil
			m.sel = -1
			renderHistory(m, m.backend.Messages())
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

	case railTickMsg:
		m.refreshRail()
		return m, m.railTick()
	}
	return m, nil
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
	DreamOnIdle bool       // reflect into memory after the session goes idle
	IdleMinutes int        // idle delay before dreaming (default 5)
	MaxTokens   int        // user context-budget ceiling (0 = auto from the model window)
	// SmallCompactor, when set, summarizes compactions on a cheap small model;
	// live model switches chain it before the new main provider's compactor.
	SmallCompactor llm.Compactor
	// NotifyCmd is an external notifier command (e.g. "notify-send") run with
	// the ping message appended; empty = bell only (EIGEN_NOTIFY_CMD also works).
	NotifyCmd string
	// LoopPrompt/LoopEvery restore a resumed session's idle loop (see /loop).
	LoopPrompt string
	LoopEvery  time.Duration
	// Router is the opt-in auto-router; /route toggles it and the top-level turn
	// routes through it. Nil disables the /route command.
	Router Router
	// EventWrap, if set, wraps the agent event sink (e.g. observability logging)
	// before the TUI's own handler. Identity when nil.
	EventWrap func(agent.EventSink) agent.EventSink
	// HookRunner fires session-lifecycle hooks (start/stop/resume). Nil = none.
	HookRunner *hook.Runner
	// NoSessionFile disables the local transcript autosave (daemon-hosted
	// sessions: the daemon owns persistence; a local copy would duplicate it).
	NoSessionFile bool
	// Title restores a resumed session's user-set name so a later saveMeta
	// doesn't blank it (local sessions); daemon sessions carry it in State.
	Title string
}

// Router is the auto-router surface the TUI needs: toggle, status, and routing
// a top-level prompt to a provider+model. Implemented by main's autoRouter.
type Router interface {
	Enabled() bool
	SetEnabled(bool)
	Providers() []string
	Route(ctx context.Context, prompt, kind, difficulty string, hasImage bool) (llm.Provider, string, string)
}

// Run drives the agent under a multi-turn Bubble Tea REPL.
func Run(backend chat.Backend, o Options) (Result, error) {
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

	if len(history) > 0 {
		backend.Reset(history)
	}
	// Restore a resumed session's user-set title (local backends keep it only
	// in the sidecar; daemon backends already carry it in their State, but
	// re-applying an empty string would be a harmless no-op there).
	if o.Title != "" {
		backend.SetTitle(o.Title)
	}

	m := &model{
		backend:        backend,
		sp:             sp,
		ti:             ti,
		ctx:            ctx,
		state:          stInput,
		initialTask:    initialTask,
		srcDir:         eigenSrcDir(),
		sessionPath:    sessionPathFor(o),
		store:          store,
		speaker:        speech.Detect(),
		clip:           clipboard.Detect(),
		stt:            voice.DetectSTT(),
		tts:            voice.DetectTTS(),
		provName:       o.Provider,
		modelID:        o.Model,
		newProvider:    llm.New,
		mem:            o.Memory,
		skills:         o.Skills,
		dreamOnIdle:    o.DreamOnIdle,
		idleMinutes:    o.IdleMinutes,
		maxTokens:      o.MaxTokens,
		smallCompactor: o.SmallCompactor,
		router:         o.Router,
		eventWrap:      o.EventWrap,
		hooks:          o.HookRunner,
		notifyCmd:      o.NotifyCmd,
		loopPrompt:     o.LoopPrompt,
		loopEvery:      o.LoopEvery,
		railOn:         true, // shown only for daemon-hosted backends on wide terminals
	}
	if m.idleMinutes <= 0 {
		m.idleMinutes = 5
	}
	if len(history) == 0 {
		// Attaching to a backend with existing history (a daemon session):
		// render what's already there.
		history = backend.Messages()
	}
	if len(history) > 0 {
		renderHistory(m, history)
		m.note(fmt.Sprintf("— resumed %d messages —", len(history)))
	}
	m.refreshCtx()

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	sink := func(e agent.Event) { p.Send(agentEvent{e}) }
	if m.eventWrap != nil {
		sink = m.eventWrap(sink)
	}
	// Continuous, race-free autosave: persist runs in the agent goroutine after
	// every message, so a crash or kill mid-turn still leaves a complete JSONL.
	// Approvals arrive through the SAME event stream (EventApproval) for local
	// and remote backends alike; the TUI answers via backend.Answer.
	backend.Wire(sink, func(msgs []llm.Message) {
		_ = transcript.Save(m.sessionPath, msgs)
		m.saveMeta()
	})

	m.hooks.Fire(hook.Payload{Event: hook.OnSessionStart})
	final, err := p.Run()
	m.hooks.Fire(hook.Payload{Event: hook.OnSessionStop})
	if err != nil {
		return Result{}, err
	}
	fm := final.(*model)
	// A daemon-backed view leaving must NOT interrupt the session's running
	// turn: detach first, so the deferred ctx cancel (which unblocks the turn
	// goroutine's Send) is just the view leaving — the daemon keeps working.
	// esc remains the way to interrupt; quitting/hopping never is.
	if d, ok := backend.(chat.Detacher); ok {
		d.Detach()
	}
	return Result{
		Rebuild:     fm.rebuild,
		SessionPath: fm.sessionPath,
		BinPath:     fm.rebuildBin,
		SwitchTo:    fm.switchTo,
		OpenApp:     fm.openApp,
		Provider:    fm.provName,
		Model:       fm.modelID,
		Perm:        string(fm.backend.Perm()),
		Effort:      fm.backend.Effort(),
		Search:      fm.backend.SearchMode(),
	}, nil
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
// isGPTRoutingError reports a GPT-family model failing for routing/availability
// reasons (not a normal task error): the model is a GPT and the error looks
// like a routing/availability/transport failure. Per the user's rule such
// failures fail over to opus.
func isGPTRoutingError(model string, err error) bool {
	if err == nil || !strings.HasPrefix(model, "openai.gpt") {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, cue := range []string{
		"no route", "routing", "not found", "does not exist", "unavailable",
		"bad gateway", "502", "503", "504", "connection", "timeout", "eof",
	} {
		if strings.Contains(s, cue) {
			return true
		}
	}
	return false
}

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

// failoverChain is the ordered fallback ladder (the user's rule): fable-5 is
// the default, gpt-5.5 is the first failover, opus third. nextFailover picks
// the first entry that isn't the failing model so a failover never lands on
// the model that just failed.
var failoverChain = []string{
	"openai.gpt-5.5",
	"us.anthropic.claude-opus-4-8",
}

// nextFailover returns the first chain model different from the failing one,
// or "" when the chain is exhausted (the failing model IS the last resort).
func nextFailover(failing string) string {
	for _, id := range failoverChain {
		if id != failing {
			return id
		}
	}
	return ""
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

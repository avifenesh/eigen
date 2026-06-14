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
	"github.com/avifenesh/eigen/internal/theme"
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
	// Palette — sourced from internal/theme (the single source of truth shared
	// with the app shell). Calm desaturated truecolor; roles, not hues:
	//   user = title cyan, assistant prose = default fg, thinking = dim slate,
	//   tool = purple, ok = green, warn/active = amber, error = red,
	//   accent (borders/rules/caret) = frost blue.
	styleUser   = theme.STitle.Bold(true)
	styleText   = theme.SText // assistant prose — crisp, explicit (not terminal default)
	styleTool   = theme.STool
	styleErr    = theme.SErr
	styleReason = theme.SDim
	styleGhost  = theme.SGhost
	styleStatus = theme.SOk
	styleAsk    = theme.SWarn.Bold(true)
	styleCode   = theme.SCode

	// Code on the Surface tint: the fg paired with the code-block surface bg so
	// fenced code reads as a distinct framed element (not teal prose). The lang
	// chip is bright brand on the Overlay tint.
	styleCodeOnSurface = theme.SCode.Background(theme.Surface)
	styleSurfaceBrand  = lipgloss.NewStyle().Foreground(theme.Title).Background(theme.Overlay).Bold(true)

	// accent is the calm structural color for borders, rules, and the prompt
	// caret — present but not loud.
	accent       = theme.Accent
	styleAccent  = theme.SAccent
	styleWorking = theme.SWorking.Bold(true)
	styleFocus   = theme.SFocus // the active session (this pane) — non-brand
	styleSel     = theme.SSel   // selected row / cursor in a list/picker — non-brand

	// Markdown prose styles for assistant answers.
	styleHeading    = theme.SHeading.Bold(true)
	styleTableHead  = theme.STitle.Bold(true) // markdown table column headers
	styleBold       = lipgloss.NewStyle().Bold(true)
	styleItalic     = lipgloss.NewStyle().Italic(true)
	styleInlineCode = theme.SCode
	styleQuote      = theme.SDim.Italic(true)
	styleBullet     = theme.SAccent
	styleLink       = theme.SLink.Underline(true)
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

	blocks []*block
	sel    int // index of the selected block (-1 = none / following tail)
	state  uiState
	// attachedRunning: we attached to a session whose turn another view (or no
	// view) started, so this view is WATCHING a turn it didn't launch. A
	// terminal event from that turn flips us back to idle even though we have
	// no local Send/turnDoneMsg in flight.
	attachedRunning bool
	pending         *pendingApproval
	status          string

	// flash is a transient banner (e.g. "copied 250 chars") shown bottom-right
	// for a beat then cleared by flashClearMsg — eye-catching, no transcript
	// clutter. flashGen guards against an earlier flash's timer clearing a
	// newer one.
	flash     string
	flashGen  int
	flashTone flashKind
	pingFlash tea.Cmd // deferred turn-done flash, batched after relayout

	// approvedTools are tool names the user chose to always allow this session.
	approvedTools map[string]bool

	initialTask string
	width       int
	height      int
	ready       bool

	rebuild      bool
	rebuildBin   string
	rebuildArmed bool // /rebuild on the default instance asked for confirmation
	srcDir       string
	sessionPath  string

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
	switchEntries []chat.SessionEntry // all daemon sessions (unfiltered)
	switchQuery   string              // incremental type-to-search
	switchIdx     int
	switchTo      string
	openApp       bool

	// notifications/approvals tray (alt+t / actTray): an at-a-glance "what
	// needs me" surface — sibling daemon sessions blocked on an approval or
	// errored, plus a ring of recent notes. trayItems is rebuilt when opened;
	// notif is the rolling notification history (newest last, capped).
	tray      bool
	trayIdx   int
	trayItems []trayItem
	notif     []string

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
	voiceMic    voiceState         // what the mic is doing (sidebar glyph)
	voiceStop   context.CancelFunc // cancels the in-flight recording
	voiceGen    int                // epoch guard: stale recordings/timers die
	voiceMuted  bool               // conv mode: replies speak, mic doesn't record
	speech      *speechQueue       // streamed sentence-by-sentence speech
	speechBuf   string             // incomplete sentence tail awaiting boundary

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

	// pal is the fuzzy command palette (ctrl+k) over the action registry +
	// chrome toggles + common slash commands. Inactive when pal.active false.
	pal palette

	// Left session rail (Tier 9 Wave 3): railOn toggles the persistent column
	// of daemon sibling sessions down the left of the transcript; railEntries
	// is the last polled snapshot. Only shown for daemon-hosted backends on a
	// wide-enough terminal. Sessions group under collapsible project headers
	// (Tier 11 Wave 4); railCollapsed is UI-local state keyed by project dir,
	// railSpin animates the working-session glyph.
	railOn        bool
	railEntries   []chat.SessionEntry
	railCollapsed map[string]bool
	railSpin      int
	brandTick     int    // advances while working → animates the λ mark + loader
	lastTitle     string // last terminal-title string written (throttle: skip no-op rewrites)

	// Right changes panel (Tier 9 Wave 4): changesOn toggles the column of
	// files touched in the last edit-producing run (click a file = jump to its
	// tool block). Hidden when the last run made no edits or the terminal is
	// too narrow (degrades before the rail).
	changesOn       bool
	rightTab        rightPanelTab // selected right panel tab (changes/git/terminal)
	term            termState     // embedded PTY terminal tab state
	tasks           tasksState    // background-tasks tab state (Tier 12)
	tasksDir        string        // background-task store dir (tests isolate it)
	changesCache    []fileChange  // memoized last-run change index
	changesCacheSig string        // transcript signature the cache was built for
	changesVw       changesView   // memoized inline-diff view (lines + file map)
	changesScroll   int           // changes tab scroll offset (wheel)

	// Panel resizing (drag the rail's separator / the right panel's left
	// edge). railW/rightW are the user-set widths (0 = the defaults);
	// resizing names the panel currently being dragged (regNone = none).
	railW    int
	rightW   int
	resizing region

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
	// Seed the background-tasks state (sidebar badge) up front so renders
	// never lazily hit the disk store mid-View.
	if !m.tasks.loaded {
		m.refreshTasks()
	}
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

func (m *model) note(s string) {
	m.push(&block{kind: blockNote, body: sb(s)})
	// Keep a rolling history for the notifications tray (newest last, capped).
	m.notif = append(m.notif, s)
	if len(m.notif) > maxNotif {
		m.notif = m.notif[len(m.notif)-maxNotif:]
	}
}

// maxNotif caps the notifications ring shown in the tray.
const maxNotif = 50

// flashClearMsg clears a transient banner if it's still the current one.
type flashClearMsg struct{ gen int }

// flashKind tones the transient banner: ok (green), warn (amber), err (red).
type flashKind int

const (
	flashOk flashKind = iota
	flashWarn
	flashBad
)

// showFlash sets a transient bottom banner (ok tone) and returns a command
// that clears it after a short beat. Use for fast confirmations (copy,
// toggles) that shouldn't leave a permanent transcript line.
func (m *model) showFlash(s string) tea.Cmd { return m.showFlashTone(s, flashOk) }

// showFlashTone is showFlash with an explicit tone.
func (m *model) showFlashTone(s string, tone flashKind) tea.Cmd {
	m.flash = s
	m.flashTone = tone
	m.flashGen++
	gen := m.flashGen
	return tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
		return flashClearMsg{gen: gen}
	})
}
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
// hasRunningTool reports whether any block is a tool call still in flight — the
// signal to re-render the transcript on the spinner tick so its glyph animates.
func (m *model) hasRunningTool() bool {
	for _, b := range m.blocks {
		if b.kind == blockTool && b.state == toolRunning {
			return true
		}
	}
	return false
}

func (m *model) sync() {
	if !m.ready {
		return
	}
	w := m.vp.Width
	// Empty transcript: show the welcome wordmark instead of a blank void.
	if len(m.blocks) == 0 {
		m.blockStart = append(m.blockStart[:0], 0)
		m.plainLines = m.plainLines[:0]
		welcome := m.welcomeView(w, m.vp.Height)
		for _, l := range strings.Split(welcome, "\n") {
			m.plainLines = append(m.plainLines, ansi.Strip(l))
		}
		m.vp.SetContent(welcome)
		m.vp.GotoTop()
		return
	}
	var out strings.Builder
	m.blockStart = m.blockStart[:0]
	m.plainLines = m.plainLines[:0]
	line := 0
	for i, b := range m.blocks {
		// Composed vertical rhythm: a blank separator before every block (so
		// messages / thoughts / tool actions don't run together), and an EXTRA
		// blank before a user message — a new turn reads as a fresh section
		// with real air around it. Tracked in plainLines + line count so
		// click-mapping and drag-selection stay aligned with the viewport.
		if i > 0 {
			out.WriteString("\n")
			m.plainLines = append(m.plainLines, "")
			line++
			if b.kind == blockText && b.role == "user" {
				out.WriteString("\n")
				m.plainLines = append(m.plainLines, "")
				line++
			}
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

// isWatchedTurnEnd reports whether an event ends a turn this view is merely
// watching (didn't start): a normal EventDone, or the daemon's terminal
// EventNote for an interrupt/error (finishTurn emits "interrupted" / "error:…"
// with no EventDone).
func isWatchedTurnEnd(e agent.Event) bool {
	if e.Kind == agent.EventDone {
		return true
	}
	if e.Kind == agent.EventNote {
		t := strings.TrimSpace(e.Text)
		return t == "interrupted" || strings.HasPrefix(t, "error: ")
	}
	return false
}

func (m *model) submit(task string) tea.Cmd {
	m.text("user", task)
	m.state = stRunning
	m.status = "thinking"
	m.brandTick = 0
	m.setTitleThrottled(titleWorking(0)) // tab flips to "working" immediately
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
	// Routing fails CLOSED: only hop models when the catalog POSITIVELY says
	// the active model is blind — unknown ids stay on the user's choice.
	visionHas, visionKnown := llm.Vision(m.modelID)
	needVision := hasImageRef && visionKnown && !visionHas
	if m.router != nil && needVision && m.failoverFrom == nil {
		if prov, model, label := m.router.Route(m.ctx, task, "", "", hasImageRef); prov != nil && model != m.modelID {
			m.backend.SetModel(prov, m.compactorFor(prov), m.contextBudgetFor(model))
			m.provName, m.modelID = prov.Name(), model
			m.note(label + " (vision needed)")
		}
	}

	// Vision: attach referenced image files unless the catalog POSITIVELY says
	// the model is blind (fail open for unknown ids — attempting the attach and
	// surfacing the backend's error beats silently dropping the image).
	var images []llm.Image
	if has, known := llm.Vision(m.modelID); m.backend != nil && (has || !known) {
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
			m.pal = palette{}
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
		// Reshape the embedded terminal to the new panel height (Update-owned).
		if m.term.started && !m.term.exited {
			m.ensureTermSize(m.termRows())
		}
		return m, nil

	case tea.KeyMsg:
		// When the embedded terminal owns input, it gets keys FIRST — including
		// ctrl+c (interrupt the shell's foreground job, not quit eigen). ctrl+g
		// is the one chord that releases focus back to the TUI.
		if m.termFocused() {
			if cmd, handled := m.termKey(msg.String(), msg); handled {
				return m, cmd
			}
		}
		// A live recording owns esc: discard it (bump the epoch so the
		// in-flight transcript is dropped as stale). The button (⏺/◉) is the
		// "done talking" stop; esc is the "never mind".
		if m.voiceMic != voiceIdle && msg.String() == "esc" {
			m.voiceGen++ // anything in flight is now stale
			m.stopListening("")
			m.voiceMic = voiceIdle
			if m.voiceOn {
				m.exitVoiceMode("conversation mode off")
			} else {
				m.note("dictation cancelled")
			}
			return m, nil
		}
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
		// Command palette captures keys while open.
		if m.pal.active {
			if cmd, handled := m.paletteKey(msg.String()); handled {
				return m, cmd
			}
		}
		// Session switcher captures keys while open.
		if m.switching {
			entries := m.switchFiltered()
			if m.switchIdx >= len(entries) {
				m.switchIdx = len(entries) - 1
			}
			if m.switchIdx < 0 {
				m.switchIdx = 0
			}
			switch msg.String() {
			case "up", "ctrl+p", "alt+up", "shift+up":
				if m.switchIdx > 0 {
					m.switchIdx--
				}
			case "down", "ctrl+n", "alt+down", "shift+down":
				if m.switchIdx < len(entries)-1 {
					m.switchIdx++
				}
			case "enter":
				if len(entries) == 0 {
					return m, nil
				}
				sel := entries[m.switchIdx]
				m.switching = false
				if sl, ok := m.backend.(chat.SessionLister); ok && sel.ID == sl.SessionID() {
					m.sync() // already here
					return m, nil
				}
				m.switchTo = sel.ID
				return m, tea.Quit
			case "ctrl+h":
				m.switching = false
				m.openApp = true
				return m, tea.Quit
			case "esc":
				m.switching = false
				m.sync()
			case "backspace":
				if m.switchQuery != "" {
					m.switchQuery = m.switchQuery[:len(m.switchQuery)-1]
					m.switchIdx = 0
				} else {
					m.switching = false
					m.sync()
				}
			default:
				// Type to search; "space" arrives as a named key.
				k := msg.String()
				if k == "space" {
					k = " "
				}
				if len(k) == 1 {
					m.switchQuery += k
					m.switchIdx = 0
				}
			}
			return m, nil
		}
		// Notifications/approvals tray captures keys while open.
		if m.tray {
			switch msg.String() {
			case "up", "ctrl+p", "alt+up", "shift+up", "k":
				if m.trayIdx > 0 {
					m.trayIdx--
				}
			case "down", "ctrl+n", "alt+down", "shift+down", "j":
				if m.trayIdx < len(m.trayItems)-1 {
					m.trayIdx++
				}
			case "enter":
				if _, quit := m.trayActivate(); quit {
					return m, tea.Quit
				}
			case "esc", "q":
				m.tray = false
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
			return m, m.copySelected()
		case "alt+s":
			// In-window session switcher: hop this window to another daemon
			// session (or home to the app) without touching running turns.
			m.openSwitcher()
			return m, nil
		case "alt+n":
			// Notifications/approvals tray: what needs me across sessions.
			m.openTray()
			return m, nil
		case "ctrl+b", "alt+b":
			// Toggle the left session rail (b = bar/sidebar).
			return m, m.toggleRail()
		case "ctrl+g", "alt+g":
			// Toggle the right panel.
			return m, m.toggleChanges()
		case "alt+tab", "ctrl+r":
			// Cycle right panel tabs (changes/git/term).
			return m, m.nextRightTab()
		case "ctrl+k":
			// Command palette: fuzzy launcher over every action + chrome toggle.
			m.openPalette()
			return m, nil
		case "ctrl+v", "alt+v":
			// Explicit image paste: grab an image from the clipboard and stage
			// it for the next message (text paste is handled by the textarea).
			m.pasteImage()
			return m, nil
		case "ctrl+t", "alt+t":
			// Dictate once (secondary path — the sidebar ⏺ button is primary;
			// ctrl+t is dead under zellij). In voice mode it (re)starts a listen.
			if m.voiceOn && m.state == stInput {
				return m, m.startListening(true)
			}
			return m, m.dictateOnce()
		case "ctrl+a", "alt+a":
			// Quick toggle of the permission posture (gated ↔ auto) without
			// typing /perm. "a" = auto/approval mode.
			return m, m.togglePerm()
		case "ctrl+e", "alt+r":
			// Quick cycle of the reasoning effort (wraps) without typing /effort.
			// alt+r ("reasoning") is the multiplexer-safe alternative.
			return m, m.cycleEffort()
		case "ctrl+o", "alt+m":
			// Quick cycle to the next model in the catalog (wraps), without
			// typing /model. "o" for mOdel (ctrl+m is Enter in terminals);
			// alt+m is the multiplexer-safe alternative (ctrl+o is a zellij key).
			return m, m.cycleModel()
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
			case "ctrl+z":
				// Move the turn you're waiting on to the background: the daemon
				// keeps running it; this window returns to the dashboard. (esc
				// interrupts — ctrl+z does NOT; it just stops watching.)
				if m.canBackgroundTurn() {
					m.note("moved to background — the daemon keeps running it; reattach from the dashboard to collect")
					return m, m.backgroundTurn()
				}
				return m, nil
			case "esc":
				if m.cancel != nil {
					m.cancel()
					m.dropSpeech() // don't keep narrating an interrupted turn
					m.status = "interrupting…"
				} else if m.attachedRunning {
					// Watching a turn another view started: interrupt it on the
					// daemon (the terminal event arrives over the live stream).
					if it, ok := m.backend.(chat.Interrupter); ok {
						_ = it.Interrupt()
						m.dropSpeech()
						m.status = "interrupting…"
					}
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
		// Full-screen config panel has a visible clickable back affordance.
		if m.conf.active {
			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.Y == 0 && msg.X >= 0 && msg.X < len("‹ back") {
				m.conf = confPanel{}
				m.sync()
				return m, nil
			}
			return m, nil
		}
		switch {
		case msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress:
			// Right-click pastes the clipboard into the input.
			m.pasteIntoInput()
			return m, nil
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress:
			// Panel edges are grabbable: press on the rail's separator column or
			// the right panel's gutter column starts a resize drag (checked
			// before chrome actions so the edge always wins its one column).
			if reg, ok := m.resizeEdgeAt(msg.X, msg.Y); ok {
				m.resizing = reg
				return m, nil
			}
			// Chrome (status bar / header) is clickable: dispatch the segment's
			// action through the same validated path as keys. Chrome rows are
			// not draggable, so acting on press is safe.
			if h := m.hitTest(msg.X, msg.Y); h.action != actNone {
				return m, m.dispatch(h.action)
			} else if h.region == regLeftRail {
				if m.sidebarVisible() {
					// Sidebar rows: nav actions resolved in hitTest (handled
					// above); here handle the embedded rail rows — project
					// headers collapse, session rows hop.
					if r, ok := m.sidebarRowAt(h.localY); ok {
						switch {
						case r.kind == sbSessionsHeader:
							// Collapse-all / expand-all toggle on the header.
							m.toggleRailProjects()
							return m, nil
						case r.kind == sbRail && r.rail.header:
							m.toggleRailProject(r.rail.dir)
							return m, nil
						case r.kind == sbRail:
							if r.rail.entry >= 0 && r.rail.entry < len(m.railEntries) {
								return m, m.hopToSession(m.railEntries[r.rail.entry].ID)
							}
						}
					}
					return m, nil
				}
				// A click on a project header toggles its collapse; a click on
				// a session row hops the window there (same path as the
				// switcher's enter — Detach keeps the daemon turn alive).
				if r, ok := m.railRowAt(h.localY); ok {
					if r.header {
						m.toggleRailProject(r.dir)
						return m, nil
					}
					if r.entry >= 0 && r.entry < len(m.railEntries) {
						return m, m.hopToSession(m.railEntries[r.entry].ID)
					}
				}
				return m, nil
			} else if h.region == regRightPanel {
				// Header tab click (after the leading gutter) switches the tab
				// (and may start the embedded terminal). A click on the term
				// grid focuses it so keystrokes go to the shell.
				if h.localY == 0 {
					if cmd, hit := m.rightPanelTabAt(h.localX-2, h.localY, m.rightPanelWidth()-2); hit {
						return m, cmd
					}
				}
				if m.rightTab == rightTabTerminal {
					m.term.focused = true
					if !m.term.started {
						return m, m.startTerm(m.termRows())
					}
					return m, nil
				}
				// A click on a changes-panel file jumps to its tool block.
				if m.rightTab == rightTabChanges {
					if idx := m.changesRowAt(h.localY); idx >= 0 {
						return m, m.jumpToChange(idx)
					}
				}
				// A click on a task row expands it; [cancel] confirms a cancel.
				if m.rightTab == rightTabTasks {
					return m, m.tasksClick(h.localY)
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
		case msg.Action == tea.MouseActionMotion && m.resizing != regNone:
			// Dragging a panel edge resizes the panel live.
			m.applyResizeDrag(msg.X)
			return m, nil
		case msg.Action == tea.MouseActionRelease && m.resizing != regNone:
			m.applyResizeDrag(msg.X)
			m.resizing = regNone
			m.persistPanelWidths() // remember the dragged width across sessions
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
						err := m.clip.Copy(text)
						if err == nil {
							cmd := m.showFlash(fmt.Sprintf("copied %d chars", len([]rune(text))))
							m.dragMoved = false
							return m, cmd
						}
						m.push(&block{kind: blockNote, isErr: true, body: sb("copy failed: " + err.Error())})
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
			// Wheel over the changes tab scrolls the inline diff view.
			if h := m.hitTest(msg.X, msg.Y); h.region == regRightPanel && m.rightTab == rightTabChanges {
				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.changesScroll -= 3
				case tea.MouseButtonWheelDown:
					m.changesScroll += 3
				}
				if m.changesScroll < 0 {
					m.changesScroll = 0
				}
				return m, nil
			}
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
		// When this view is WATCHING a turn it didn't start (attached to an
		// already-running session), the daemon's terminal event is what
		// returns us to idle — there's no local turnDoneMsg. The turn ends on
		// EventDone (normal) or a terminal EventNote ("interrupted"/"error:…",
		// emitted by the daemon's finishTurn). Mirror the essential turn-end
		// transitions; the originating view handles its own turnDoneMsg.
		if m.attachedRunning && m.cancel == nil && isWatchedTurnEnd(msg.e) {
			m.attachedRunning = false
			m.state = stInput
			m.status = ""
			m.turnStarted = time.Time{}
			m.ti.Focus()
			m.setTitleThrottled(titleReady())
			m.refreshCtx()
			m.autosave()
			// Drain a queued steer message now that the watched turn ended.
			if len(m.queued) > 0 {
				next := m.queued[0]
				m.queued = m.queued[1:]
				return m, func() tea.Msg { return submitMsg{next} }
			}
		}
		return m, nil

	case voiceSpokenMsg:
		m.status = ""
		return m.handleSpoken(msg)

	case voiceSpeechDoneMsg:
		// The spoken reply finished (or was cut). Same epoch + still in voice
		// mode → back to the mic; anything else is stale (mode exited, a new
		// utterance already started).
		if msg.gen != m.voiceGen || !m.voiceOn {
			return m, nil
		}
		m.voiceCancel = nil
		m.voiceMic = voiceIdle
		return m, m.startListening(true)

	case turnDoneMsg:
		if msg.err != nil {
			// Defensive: a "session busy" error means another view's turn was
			// still running when this Send raced in (the attachedRunning queue
			// path normally prevents it). Don't surface a scary error — the
			// turn is fine; tell the user to wait/queue.
			if strings.Contains(msg.err.Error(), "session busy") {
				m.state = stRunning
				m.attachedRunning = true
				m.status = "working"
				m.note("still working on the previous turn — your next message will queue (Enter), or press esc to interrupt")
				m.cancel = nil
				return m, nil
			}
			m.dropSpeech() // a failed/interrupted turn must not keep talking
			m.push(&block{kind: blockNote, isErr: true, body: sb("error: " + msg.err.Error())})
			switch {
			case isGPTRoutingError(m.modelID, msg.err) && m.ctx.Err() == nil:
				// GPT routing/availability failure: walk the failover chain
				// (opus default → gpt-5.5 → sonnet-4-6); never land on the
				// failing model itself.
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
		m.pingFlash = m.pingOnTurnDone(msg.err)
		m.finishTurnStats()
		m.turnStarted = time.Time{}
		m.cancel = nil
		m.state = stInput
		m.status = ""
		m.ti.Focus()
		m.setTitleThrottled(titleReady()) // tab shows "ready"; bell (below) chimes for long turns
		m.refreshCtx()                    // safe: the turn's goroutine has returned
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
		// Read the answer aloud if the persistent /read toggle is on (voice
		// mode handles its own speech + relisten below). When streamed speech
		// already spoke the answer mid-turn, just flush its tail — re-speaking
		// the whole thing would repeat it.
		if msg.err == nil && !m.voiceOn && m.readAloud {
			if q := m.flushSpeech(); q == nil && m.speaker != nil {
				if ans := m.lastAssistantText(); ans != "" {
					m.speaker.Speak(ans)
				}
			}
		}
		// Drain a queued message (steer/queue): send the next one immediately.
		if len(m.queued) > 0 {
			next := m.queued[0]
			m.queued = m.queued[1:]
			return m, m.submit(next)
		}
		m.relayout()
		pf := m.pingFlash
		m.pingFlash = nil
		// Conversation mode: speak the answer, then listen for the next turn.
		if vc := m.voiceTurnDone(msg.err); vc != nil {
			return m, tea.Batch(pf, vc, textarea.Blink, m.scheduleIdleDream(), m.scheduleGoalNag(), m.scheduleLoop())
		}
		return m, tea.Batch(pf, textarea.Blink, m.scheduleIdleDream(), m.scheduleGoalNag(), m.scheduleLoop())

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

	case termExitedMsg:
		// The shell process exited (reaped by the waiter). Mark it so the panel
		// shows the restart hint; gen guard drops a stale exit from an old shell.
		if msg.gen == m.term.gen {
			m.term.exited = true
			m.term.ticking = false
		}
		return m, nil

	case termTickMsg:
		// Paced repaint while the terminal tab is visible. Stop the loop (don't
		// re-arm) when the tab is hidden, the shell is gone, or this is a stale
		// generation. Resize happens HERE (Update), never in View.
		if msg.gen != m.term.gen || !m.term.started || m.term.exited ||
			m.rightTab != rightTabTerminal || !m.changesOn {
			m.term.ticking = false
			return m, nil
		}
		m.ensureTermSize(m.termRows())
		return m, m.termTick()

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
		if m.state == stRunning {
			m.brandTick++           // animate the λ mark + loader (fast, in-app)
			animFrame = m.brandTick // shared frame for the running-tool spinner
			// Re-render the transcript so a running tool block's spinner glyph
			// animates (running blocks bypass the render cache). Cheap: only
			// while a turn is in flight and only if a tool is actually running.
			if m.hasRunningTool() {
				m.sync()
			}
			// Tab title breathes on WALL-CLOCK seconds, not the fast tick, and
			// only rewrites when the string changes — so the window tab doesn't
			// flicker like a bug. (secs since the turn started.)
			secs := int(time.Since(m.turnStarted).Seconds())
			m.setTitleThrottled(titleWorking(secs))
		}
		return m, cmd

	case growDoneMsg:
		switch {
		case msg.unsupported:
			m.note(fmt.Sprintf("can't stretch this terminal from inside — widen the window to ≥%d cols", msg.want))
		case msg.ok:
			// The WindowSizeMsg that follows the resize relayouts and shows the
			// panel; nothing more to do.
		case msg.triedWindow:
			// We asked the terminal window itself (XTWINOPS) and it refused or
			// fell short — common policy in some terminals (e.g. ghostty
			// ignores resize escapes by default; tiling WMs fix the size).
			m.note(fmt.Sprintf("asked the terminal to widen to %d cols but it stayed at %d — this terminal ignores resize requests; widen the window (or run inside zellij/tmux and I can stretch the pane)", msg.want, msg.got))
		default:
			m.note(fmt.Sprintf("pane stretched to %d cols but the panel needs ≥%d — the zellij pane may already fill the window: enlarge the terminal window itself (ghostty ignores resize requests from apps), or close/shrink a neighbor pane", msg.got, msg.want))
		}
		return m, nil

	case railTickMsg:
		m.refreshRail()
		return m, m.railTick()

	case flashClearMsg:
		if msg.gen == m.flashGen {
			m.flash = ""
		}
		return m, nil

	case tasksTickMsg:
		// Stale generations (tab hidden and reshown) die silently.
		if msg.gen != m.tasks.gen {
			return m, nil
		}
		m.refreshTasks()
		return m, m.tasksTick()
	}
	return m, nil
}

// buildCmd rebuilds eigen to a staging binary, smoke-tests it, and only on
// success atomically swaps it into place — so a broken build never replaces the
// working binary or kills the session. Failures are reported back via buildDoneMsg.
// findGo resolves the go toolchain: PATH first, then the usual install
// locations — windows spawned from minimal environments (systemd, daemons,
// app launchers) often lack the login shell's PATH additions.
func findGo() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, c := range []string{
		filepath.Join(home, ".local", "bin", "go"),
		filepath.Join(home, ".local", "go", "bin", "go"),
		filepath.Join(home, "go", "bin", "go"),
		"/usr/local/go/bin/go",
		"/usr/lib/go/bin/go",
	} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func (m *model) buildCmd() tea.Cmd {
	src := m.srcDir
	return func() tea.Msg {
		gobin := findGo()
		if gobin == "" {
			return buildDoneMsg{err: fmt.Errorf("go toolchain not found (PATH=%s)", os.Getenv("PATH"))}
		}
		bin := filepath.Join(src, "bin", "eigen")
		staging := bin + ".new"

		build := exec.Command(gobin, "build", "-o", staging, ".")
		build.Dir = src
		if out, err := build.CombinedOutput(); err != nil {
			return buildDoneMsg{err: fmt.Errorf("build failed: %v", err), out: string(out)}
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
	sp.Spinner = spinner.MiniDot // smooth single-cell braille — calm, lively
	sp.Style = styleAccent

	ti := textarea.New()
	ti.Placeholder = "type a task…  (enter send · ctrl+j newline · / commands · ↑↓ history · ctrl+c quit)"
	ti.Prompt = "❯ "
	ti.ShowLineNumbers = false
	ti.CharLimit = 0
	ti.MaxHeight = inputMaxRows
	ti.SetHeight(1)
	styleInputBox(&ti, accent)
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

	spk := speech.Detect()
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
		speaker:        spk,
		clip:           clipboard.Detect(),
		stt:            voice.DetectSTT(),
		tts:            voiceTTS(spk),
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
		changesOn:      true, // right panel (changes/git/terminal) visibility
		rightTab:       rightTabChanges,
	}
	// Restore window-layout prefs (panel widths the user dragged to last time).
	if pr := loadUIPrefs(); pr.RailW > 0 || pr.RightW > 0 {
		m.railW = pr.RailW
		m.rightW = pr.RightW
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
	// Attaching to a session whose turn is ALREADY running (started by another
	// view, or still going after this view detached): start in the running
	// state so the "working" indicator shows and typed input queues instead of
	// erroring "session busy". A terminal event (EventDone/note) from that
	// in-flight turn flips us back to idle (see the agentEvent handler).
	if backend.Running() {
		m.state = stRunning
		m.attachedRunning = true
		m.turnStarted = time.Now()
		m.status = "working"
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
	m.stopTerm()     // kill the embedded terminal's shell process group, if any
	setTermTitle("") // restore the terminal's own title on exit
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

// styleInputBox pins every textarea sub-style (and the cursor) to the eigen
// Base background. The bubbles textarea otherwise paints its content/cursor
// with ANSI black ([40m) + reverse video, which on a terminal with a non-black
// background (e.g. ghostty `background = #1b1c20` + opacity) leaks through as an
// "exposed" patch around the input box. Shared by Run and tests so both render
// the same. A rounded border frames the box: accent when focused, faint when
// blurred; the caret/placeholder follow the palette.
func styleInputBox(ti *textarea.Model, accent lipgloss.TerminalColor) {
	base := lipgloss.NewStyle().Background(theme.Base)
	for _, st := range []*textarea.Style{&ti.FocusedStyle, &ti.BlurredStyle} {
		st.CursorLine = base // flat: no cursor-line highlight, on Base
		st.CursorLineNumber = base
		st.EndOfBuffer = base
		st.LineNumber = base
		st.Text = st.Text.Background(theme.Base)
	}
	ti.FocusedStyle.Base = lipgloss.NewStyle().
		Background(theme.Base).Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).BorderBackground(theme.Base)
	ti.BlurredStyle.Base = lipgloss.NewStyle().
		Background(theme.Base).Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Faint).BorderBackground(theme.Base)
	ti.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(theme.Accent).Background(theme.Base)
	ti.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(theme.Faint).Background(theme.Base)
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(theme.Dim).Background(theme.Base)
	ti.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(theme.Faint).Background(theme.Base)
	// The cursor bubble defaults to reverse video on ANSI black; put it on Base
	// (the block cursor still inverts the glyph cell, but on the right canvas).
	ti.Cursor.Style = lipgloss.NewStyle().Background(theme.Base)
	ti.Cursor.TextStyle = lipgloss.NewStyle().Background(theme.Base)
}

func compact(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		s = s[:80] + "⋯"
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
		s = s[:100] + "⋯"
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

// failoverChain is the ordered fallback ladder used when the active model is
// persistently overloaded (Bedrock 503). The default is opus-4-8, so the chain
// is the OTHER models to try: gpt-5.5 first, then sonnet-4-6. nextFailover
// picks the first entry that isn't the failing model so a failover never lands
// on the model that just failed (and never on opus when opus is failing).
var failoverChain = []string{
	"openai.gpt-5.5",
	"us.anthropic.claude-sonnet-4-6",
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

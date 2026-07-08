// Package daemon is the long-lived session host: the REAL eigen app. It owns
// agent sessions (each a whole chat rooted at its own directory), keeps them
// running whether or not any window is attached, and serves views over a Unix
// socket. Terminal windows are thin clients that attach, mirror events, and
// send input; a session's lifetime is independent of any view.
//
// This package is transport + lifecycle only. The actual agent for a session
// is built by the caller (package main's buildSession) and handed in via
// NewSession, so daemon need not know how tools/providers are wired.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
)

// Status is a session's live state, shown in the app rail.
type Status string

const (
	StatusIdle     Status = "idle"
	StatusWorking  Status = "working"
	StatusApproval Status = "approval" // blocked awaiting an approval answer
	StatusError    Status = "error"
)

// SessionInfo is the metadata a view needs to list/choose sessions.
type SessionInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Dir     string `json:"dir"`
	Model   string `json:"model"`
	Status  Status `json:"status"`
	Turns   int    `json:"turns"`
	Views   int    `json:"views"`   // attached views (windows) right now
	Updated int64  `json:"updated"` // unix nano
}

// Session is one hosted chat: an agent session plus the bookkeeping the daemon
// needs to multiplex views onto it (event fan-out, status, replay buffer).
type Session struct {
	ID    string
	Dir   string
	Model string

	mu        sync.Mutex
	loadMu    sync.Mutex // serializes unload/rehydrate with operations that need a live agent
	persistMu sync.Mutex // serializes one session's transcript/meta writes so older persistence snapshots cannot clobber newer state
	agent     *agent.Agent
	sess      *agent.Session
	status    Status
	title     string
	updated   time.Time

	// Cold-session metadata kept after an inactive session unloads its agent and
	// transcript from memory. The daemon can still list the row cheaply and later
	// rehydrate from disk when a view/input needs it again.
	turns         int
	fallbackTitle string
	coldPerm      string
	coldGoal      string
	coldRoots     []string

	// lastAttached: when a view last attached — "last used by ME" for list
	// ordering (transcript mtime lies; the titler touches files).
	lastAttached time.Time
	onAttach     func() // host hook: persist meta when a view attaches
	onTokens     func() // host hook: persist meta when cumulative tokens change (turn done)
	onClear      func() // host hook: purge transcript backups after /clear (so recovery can't resurrect)

	// notify, when set, fires a desktop notification when a turn finishes with
	// NO views attached — i.e. the user backgrounded the turn (left the window
	// while it ran). Set by the host from the configured notifier.
	notify      func(title, body string)
	turnStarted time.Time

	// events is the append-only log of this session's events, so a view that
	// attaches mid-run can replay history and then follow live.
	events  []agent.Event
	subs    map[int]chan agent.Event // attached views
	nextSub int

	// Cumulative token usage over this session's lifetime (summed from each
	// turn's EventDone). cumCacheRead vs cumIn is the prompt-cache hit rate,
	// surfaced in the daemon stats for token-efficiency visibility.
	cumIn, cumOut, cumCacheRead, cumCacheWrite int64

	cancel     context.CancelFunc // cancels the in-flight turn (interrupt)
	running    bool
	titling    bool   // a title request is already in flight
	onClose    func() // releases the session's external resources (MCP/LSP/observe)
	onInactive func() // host hook: unload cold resources once idle + detached

	// goalWakes counts CONSECUTIVE goal auto-continuations (wakeForGoalContinue)
	// with no user input in between. A goal the judge never confirms would
	// otherwise re-wake the session after every turn forever — an unbounded
	// billing loop on an unattended session. Reset by user activity or a goal
	// change; capped at maxGoalWakes.
	goalWakes int

	// gated-permission approvals awaiting a view's verdict
	approvals   map[string]*pendingApproval
	approvalSeq int
}

// newSession wraps a built agent as a hosted session.
func newSession(id, dir, model string, a *agent.Agent) *Session {
	// When no explicit model was requested, report the provider's actual model
	// id so the status bar isn't blank and a persisted+restored session
	// reconstructs the same model.
	if model == "" && a != nil && a.Provider != nil {
		model = a.Provider.ModelID()
	}
	s := newColdSession(id, dir, model)
	s.bindAgent(a, nil)
	return s
}

func newColdSession(id, dir, model string) *Session {
	return &Session{
		ID:      id,
		Dir:     dir,
		Model:   model,
		status:  StatusIdle,
		updated: time.Now(),
		subs:    map[int]chan agent.Event{},
	}
}

// bindAgent installs a freshly-built agent into this hosted session and wires
// daemon event fan-out + approvals. sess may be a resumed transcript; nil means
// start a new empty agent session.
func (s *Session) bindAgent(a *agent.Agent, sess *agent.Session) {
	s.agent = a
	if sess != nil {
		s.sess = sess
	} else {
		s.sess = a.NewSession()
	}
	// Fan out agent events to all attached views + record for replay, composing
	// the agent's host wrap (observability + hooks) so those run in the daemon —
	// sessions are observable with zero or many views.
	if a.EventWrap != nil {
		a.OnEvent = a.EventWrap(s.dispatch)
	} else {
		a.OnEvent = s.dispatch
	}
	s.installApprover()
}

// dispatch records an event and fans it out to attached views.
func (s *Session) dispatch(e agent.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	switch e.Kind {
	case agent.EventToolStart, agent.EventTextDelta, agent.EventReasoningDelta:
		s.status = StatusWorking
	case agent.EventDone:
		s.status = StatusIdle
		s.cumIn += int64(e.InTokens)
		s.cumOut += int64(e.OutTokens)
		s.cumCacheRead += int64(e.CacheReadTokens)
		s.cumCacheWrite += int64(e.CacheWriteTokens)
	}
	// Persist the updated lifetime token tallies after a turn finishes so the
	// cache-hit ratio in stats survives a restart (fired outside the lock —
	// saveSessionMeta re-enters s.mu).
	var onTokens func()
	if e.Kind == agent.EventDone {
		onTokens = s.onTokens
	}
	wakeID := ""
	if e.Kind == agent.EventBgDone && !s.running {
		// A background task this session spawned finished while the orchestrator
		// is idle → wake it to collect the result (started after we unlock).
		wakeID = e.Result
	}
	// Bound the replay buffer: a long-lived daemon session would otherwise
	// accumulate every delta forever (a slow memory leak). The buffer only
	// exists so a view attaching MID-TURN sees in-progress events; once a turn
	// ends those are dead weight (chat.Remote discards replayed events and
	// renders history from Messages()). Keep only a recent tail.
	if len(s.events) > maxReplayEvents {
		drop := len(s.events) - maxReplayEvents
		s.events = append([]agent.Event(nil), s.events[drop:]...)
	}
	s.updated = time.Now()
	for _, ch := range s.subs {
		select {
		case ch <- e:
		default: // a slow view must not stall the agent loop; it can resync
		}
	}
	s.mu.Unlock()
	if onTokens != nil {
		onTokens()
	}
	if wakeID != "" {
		s.wakeForBg(wakeID)
	}
}

// wakeForBg starts a fresh turn on an IDLE session to consume a finished
// background task — the orchestrator "wakes up" when its promoted/background
// subtask reports done. The result rides in as the turn's user message so the
// orchestrator acts on it without the user having to nudge. No-op if a turn
// raced into running first (send() returns false → the running turn will see
// the bg note in its own stream).
func (s *Session) wakeForBg(id string) {
	result := s.agent.BgResult(id)
	if strings.TrimSpace(result) == "" {
		return // canceled / no collectable output: just leave the note
	}
	msg := "Your background task " + id + " finished. Here is its result:\n\n" + result +
		"\n\nContinue: incorporate this and proceed, or report back if it completes your work."
	s.send(msg, nil, nil)
}

func (s *Session) goalJudgeAvailable() bool {
	if s == nil || s.agent == nil || s.agent.Tools == nil {
		return false
	}
	for _, d := range s.agent.Tools.Definitions() {
		if d.Name == "goal_achieved" {
			return true
		}
	}
	return false
}

func (s *Session) wakeForGoalStart() {
	if !s.goalJudgeAvailable() || strings.TrimSpace(s.agent.CurrentGoal()) == "" {
		return
	}
	s.send(agent.GoalStartInstruction, nil, nil)
}

// maxGoalWakes bounds consecutive goal auto-continuations with no user input in
// between. A goal the judge never confirms would otherwise keep the session hot
// (and billing) forever with nobody watching. Generous enough for a long
// multi-turn goal; the user resets the counter just by talking to the session.
const maxGoalWakes = 25

func (s *Session) wakeForGoalContinue() {
	if !s.goalJudgeAvailable() || strings.TrimSpace(s.agent.CurrentGoal()) == "" {
		return
	}
	s.mu.Lock()
	s.goalWakes++
	wakes := s.goalWakes
	s.mu.Unlock()
	if wakes > maxGoalWakes {
		s.dispatch(agent.Event{Kind: agent.EventNote, Text: fmt.Sprintf(
			"goal auto-continue paused after %d consecutive turns without user input — the goal is still set; send any message to resume working toward it", maxGoalWakes)})
		return
	}
	s.send(agent.GoalContinueInstruction, nil, nil)
}

// resetGoalWakes clears the consecutive auto-wake counter — called on genuine
// user activity (input/steer/resend) and on goal changes, so the cap only ever
// stops UNATTENDED loops.
func (s *Session) resetGoalWakes() {
	s.mu.Lock()
	s.goalWakes = 0
	s.mu.Unlock()
}

// maxReplayEvents bounds the per-session replay buffer — large enough to cover
// any single in-progress turn's deltas (all a mid-turn attach needs), small
// enough that a multi-day session can't leak unbounded memory.
const maxReplayEvents = 4096

// attach registers a view: returns a replay of events so far plus a live
// channel and an unsubscribe func.
func (s *Session) attach() (replay []agent.Event, live <-chan agent.Event, detach func()) {
	s.mu.Lock()
	replay = append(replay, s.events...)
	ch := make(chan agent.Event, 256)
	id := s.nextSub
	s.nextSub++
	s.subs[id] = ch
	s.lastAttached = time.Now()
	hook := s.onAttach
	s.mu.Unlock()
	if hook != nil {
		hook() // persist LastAttached (outside the lock — it re-enters)
	}
	return replay, ch, func() {
		var inactive func()
		s.mu.Lock()
		if c, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(c)
		}
		if len(s.subs) == 0 && !s.running {
			inactive = s.onInactive
		}
		s.mu.Unlock()
		if inactive != nil {
			inactive()
		}
	}
}

// send runs a turn on the session (one at a time). It returns immediately;
// progress arrives via events. A turn already running is rejected.
func (s *Session) send(task string, images []llm.Image, allowTools []string) bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.status = StatusWorking
	s.turnStarted = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	sess := s.sess
	// Per-turn allowed-tools (a slash command's allowed-tools): the agent
	// consumes and clears it for exactly this turn.
	sess.SetTurnTools(allowTools)
	go s.runTurn(ctx, func() (string, error) { return sess.SendWith(ctx, task, images) })
	return true
}

// steer injects a message into the RUNNING turn (between tool-call rounds).
// Returns false when no turn is running (the caller should send() instead).
func (s *Session) steer(text string, images []llm.Image) bool {
	s.mu.Lock()
	running := s.running
	sess := s.sess
	s.mu.Unlock()
	if !running || sess == nil {
		return false
	}
	sess.Steer(text, images)
	return true
}

// runTurn executes a turn body then finishes the turn, converting any panic
// into a turn error so a bug in one session never crashes the daemon (which
// would take down every other hosted session). Shared by send and resend.
func (s *Session) runTurn(ctx context.Context, body func() (string, error)) {
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("internal panic: %v", r)
			}
		}()
		_, err = body()
	}()
	s.finishTurn(ctx, err)
}

// finishTurn clears running state and emits a terminal event so attached views
// leave the "working" state — the agent loop emits EventDone on a normal
// finish, but an interrupt or error returns without one.
func (s *Session) finishTurn(ctx context.Context, err error) {
	s.mu.Lock()
	s.running = false
	s.cancel = nil
	interrupted := ctx.Err() != nil
	if err != nil && !interrupted {
		s.status = StatusError
	} else {
		s.status = StatusIdle
	}
	// If the turn finished with NO views attached, the user backgrounded it
	// (left the window while it ran). There's no TUI to ping them, so the
	// daemon notifies. Skip interrupts (they left deliberately) and trivially
	// short turns (not worth a popup). Snapshot under the lock.
	noViews := len(s.subs) == 0
	dur := time.Since(s.turnStarted)
	notify := s.notify
	title := s.title
	if title == "" {
		title = s.ID
	}
	s.mu.Unlock()
	switch {
	case interrupted:
		s.dispatch(agent.Event{Kind: agent.EventNote, Text: "interrupted"})
	case err != nil:
		s.dispatch(agent.Event{Kind: agent.EventNote, Text: "error: " + err.Error()})
	}
	if notify != nil && noViews && !interrupted && dur >= backgroundedNotifyMin {
		label := "done"
		if err != nil {
			label = "failed"
		}
		notify("eigen: "+title, "background turn "+label+" after "+dur.Round(time.Second).String()+" — reattach to collect")
	}
	// The turn is over: drop the replay buffer. A view attaching now
	// reconstructs the conversation from Messages() (replayed events are
	// discarded by chat.Remote), so retaining them only grows memory.
	s.mu.Lock()
	s.events = nil
	s.mu.Unlock()
	if !interrupted && err == nil {
		// A goal is not permission to go idle. The goal_achieved tool clears it
		// only after judge confirmation; until then the daemon keeps the hosted
		// agent moving even if no TUI is attached.
		s.wakeForGoalContinue()
	}
	// If the turn is now idle with no attached views, let the host unload the
	// heavyweight agent/tool resources. The hook re-checks running/views because
	// goal continuation or a racing attach may have made the session active again.
	s.mu.Lock()
	inactive := len(s.subs) == 0 && !s.running
	onInactive := s.onInactive
	s.mu.Unlock()
	if inactive && onInactive != nil {
		onInactive()
	}
}

// backgroundedNotifyMin is the minimum turn length worth a desktop notification
// when no view is attached — a short backgrounded turn isn't worth a popup. A
// var so tests can shrink it.
var backgroundedNotifyMin = 10 * time.Second

// interrupt cancels the in-flight turn, if any. It returns true only when a
// turn was actually running (s.cancel != nil) and got cancelled, so a caller
// can distinguish "interrupted a running turn" from "nothing to interrupt".
func (s *Session) interrupt() bool {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// waitUntilIdle waits briefly for an interrupted in-flight turn to unwind.
// It is bounded because a provider/tool bug must not hang daemon shutdown
// forever. A final flush after this wait captures messages appended while the
// cancellation was being handled.
func (s *Session) waitUntilIdle(timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if !running {
			return true
		}
		select {
		case <-deadline.C:
			return false
		case <-tick.C:
		}
	}
}

// flush persists the session's in-memory transcript to disk. Called by
// Host.Shutdown so a clean stop (daemon stop / SIGTERM / restart) NEVER loses
// unflushed work — the agent loop's persist() only fires at its own save
// points, so a turn in flight (or any state added since the last save, e.g. a
// /model switch that swaps the provider in memory) would be dropped on a kill.
// This makes shutdown lossless. Best-effort: a flush error must not block
// shutdown (the last good on-disk file still stands).
func (s *Session) flush() {
	s.mu.Lock()
	sess := s.sess
	var persist func([]llm.Message)
	if s.agent != nil {
		persist = s.agent.Persist
	}
	s.mu.Unlock()
	if sess == nil || persist == nil {
		return
	}
	// Make pending mid-turn user input durable too. Without this, a restart
	// before the agent loop reaches its next steer-drain boundary can lose the
	// user's follow-up even though the main transcript was flushed.
	sess.FlushSteer()
	persist(sess.Messages())
}

// info snapshots the session for listing.
func (s *Session) info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	title := s.title
	if title == "" {
		// Display fallback while the model title hasn't landed (or failed):
		// a snippet of the first user message beats "(untitled)". Cold sessions
		// keep that snippet in fallbackTitle after unloading their transcript.
		title = s.fallbackTitle
		if title == "" && s.sess != nil {
			for _, m := range s.sess.Messages() {
				if m.Role == llm.RoleUser && strings.TrimSpace(m.Text) != "" {
					title = snippet(m.Text, 48)
					break
				}
			}
		}
	}
	turns := s.turns
	if s.sess != nil {
		turns = len(s.sess.Messages())
	}
	return SessionInfo{
		ID:      s.ID,
		Title:   title,
		Dir:     s.Dir,
		Model:   s.Model,
		Status:  s.status,
		Turns:   turns,
		Views:   len(s.subs),
		Updated: s.updated.UnixNano(),
	}
}

// SetTitle updates the session's display title.
func (s *Session) SetTitle(t string) {
	s.mu.Lock()
	s.title = t
	s.mu.Unlock()
}

// --- gated-permission approvals over the socket ---
//
// When the daemon hosts a gated session, a mutating tool call blocks in
// a.Approve until an attached view answers (any view may answer — approvals
// broadcast like every other event). With no answer within approvalTimeout
// the call is DENIED (fail closed): a session with no window attached cannot
// mutate anything silently.

const approvalTimeout = 10 * time.Minute

// pendingApproval is one blocked tool call awaiting a verdict.
type pendingApproval struct {
	ID   string `json:"id"`
	Tool string `json:"tool"`
	Args string `json:"args"`
	ch   chan bool
}

// installApprover wires the session's agent to broadcast approval requests to
// views and block until answered (or timeout).
func (s *Session) installApprover() {
	s.agent.Approve = func(ctx context.Context, name string, args json.RawMessage) (bool, error) {
		s.mu.Lock()
		s.approvalSeq++
		id := fmt.Sprintf("a%d", s.approvalSeq)
		p := &pendingApproval{ID: id, Tool: name, Args: string(args), ch: make(chan bool, 1)}
		if s.approvals == nil {
			s.approvals = map[string]*pendingApproval{}
		}
		s.approvals[id] = p
		s.status = StatusApproval
		s.mu.Unlock()

		// Broadcast as an event so every attached view can prompt.
		s.dispatch(agent.Event{Kind: agent.EventApproval, Text: name + " " + p.Args, ToolName: name, Result: id})

		defer func() {
			s.mu.Lock()
			delete(s.approvals, id)
			if s.status == StatusApproval {
				s.status = StatusWorking
			}
			s.mu.Unlock()
		}()
		select {
		case ok := <-p.ch:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(approvalTimeout):
			return false, fmt.Errorf("approval timed out (no attached view answered)")
		}
	}
}

// answer resolves a pending approval by id. Returns false if no such pending.
func (s *Session) answer(approvalID string, ok bool) bool {
	s.mu.Lock()
	p := s.approvals[approvalID]
	s.mu.Unlock()
	if p == nil {
		return false
	}
	select {
	case p.ch <- ok:
	default:
	}
	return true
}

// pendingList snapshots outstanding approvals (for views attaching mid-wait).
func (s *Session) pendingList() []pendingApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]pendingApproval, 0, len(s.approvals))
	for _, p := range s.approvals {
		out = append(out, pendingApproval{ID: p.ID, Tool: p.Tool, Args: p.Args})
	}
	return out
}

// unixMilli renders a time as unix-millis for the wire, mapping the zero time
// to 0 (rather than a large negative epoch offset) so "unknown" reads as 0.
func unixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// state snapshots everything a remote chat UI needs (history + status).
func (s *Session) state() *SessionState {
	s.mu.Lock()
	a := s.agent
	sess := s.sess
	model := s.Model
	title := s.title
	running := s.running
	coldPerm := s.coldPerm
	coldGoal := s.coldGoal
	s.mu.Unlock()
	if a == nil || sess == nil {
		return &SessionState{Title: title, Model: model, Perm: coldPerm, Goal: coldGoal, Running: running}
	}
	st := &SessionState{
		Messages:  sess.Messages(),
		Tokens:    sess.Tokens(),
		Title:     title,
		Model:     model,
		MaxTokens: a.CurrentMaxContextTokens(),
		Perm:      string(a.CurrentPerm()),
		Goal:      a.CurrentGoal(),
		Running:   running,
	}
	// Read the provider once under the agent lock (a /model switch from another
	// window swaps it via SetLive — a direct field read would race).
	if prov := a.CurrentProvider(); prov != nil {
		st.Provider = prov.Name()
		if es, ok := prov.(llm.EffortSetter); ok {
			st.Effort = es.Effort()
		}
		if sr, ok := prov.(llm.Searcher); ok {
			st.Search = sr.SearchMode()
		}
		if fm, ok := prov.(llm.FastModer); ok {
			st.Fast, st.FastOK = fm.FastMode(), true
		}
	}
	if a.Tools != nil {
		for _, d := range a.Tools.Definitions() {
			st.Tools = append(st.Tools, ToolInfo{Name: d.Name, ReadOnly: d.ReadOnly})
		}
	}
	st.Roots = a.Roots()
	if a.Shells != nil {
		for _, sh := range a.Shells.Infos() {
			st.Shells = append(st.Shells, ShellInfo{
				ID: sh.ID, Command: sh.Command, Status: sh.Status, ExitCode: sh.ExitCode,
				StartedMs: unixMilli(sh.Started), FinishedMs: unixMilli(sh.Finished),
				LastLine: sh.LastLine,
			})
		}
	}
	for _, p := range s.pendingList() {
		st.Pending = append(st.Pending, ApprovalInfo{ID: p.ID, Tool: p.Tool, Args: p.Args})
	}
	return st
}

// setPerm/setGoal mutate session state (the agent's setters are mutex-guarded).
func (s *Session) setPerm(p string) { s.agent.SetPerm(agent.Permission(p)) }
func (s *Session) setGoal(g string) {
	s.agent.SetGoal(g)
	s.resetGoalWakes() // a fresh/changed goal earns a fresh auto-continue budget
	if strings.TrimSpace(g) != "" {
		s.wakeForGoalStart()
	}
}

// addDir extends the session's tool sandbox (user-invoked /add-dir grant).
// Returns the normalized root added; the agent's Policy guards its concurrency.
func (s *Session) addDir(path string) (string, error) { return s.agent.AddDir(path) }

// killShell stops a backgrounded bash shell by id (the shells panel's kill).
func (s *Session) killShell(id string) bool {
	if s.agent.Shells == nil {
		return false
	}
	return s.agent.Shells.KillByID(id)
}

// detachBash backgrounds the foreground bash command running in this session's
// turn (the user's "background this step" key).
func (s *Session) detachBash() bool { return s.agent.DetachBash() }

// setEffort/setSearch forward to the provider's optional capability; false =
// the model has no such setting or rejected the value.
func (s *Session) setEffort(level string) bool {
	if es, ok := s.agent.CurrentProvider().(llm.EffortSetter); ok {
		return es.SetEffort(level)
	}
	return false
}

func (s *Session) setSearch(mode string) bool {
	if sr, ok := s.agent.CurrentProvider().(llm.Searcher); ok {
		return sr.SetSearch(mode)
	}
	return false
}

// setFast toggles the fast/priority service tier; false = unsupported.
func (s *Session) setFast(on bool) bool {
	if fm, ok := s.agent.CurrentProvider().(llm.FastModer); ok {
		return fm.SetFast(on)
	}
	return false
}

// compact summarizes toward target tokens (0 = the agent's default policy).
func (s *Session) compact(ctx context.Context, target int) (int, int, error) {
	return s.sess.Compact(ctx, target)
}

// resume replaces the session's conversation with imported history (the
// --resume path: the view imports a transcript and hands it to the daemon).
func (s *Session) resume(history []llm.Message) {
	s.mu.Lock()
	s.sess = s.agent.Resume(history)
	s.turns = len(history)
	s.fallbackTitle = ""
	for _, m := range history {
		if m.Role == llm.RoleUser && strings.TrimSpace(m.Text) != "" {
			s.fallbackTitle = snippet(m.Text, 48)
			break
		}
	}
	s.mu.Unlock()
	// Persist immediately so the resumed history survives a restart even
	// before the first turn runs.
	if s.agent.Persist != nil {
		s.agent.Persist(history)
	}
}

// clear resets the conversation to empty (the /clear command).
func (s *Session) clear() {
	s.mu.Lock()
	s.sess = s.agent.NewSession()
	s.events = nil // a fresh attach replays nothing
	s.turns = 0
	s.fallbackTitle = ""
	s.mu.Unlock()
	// /clear is user-visible state, not just in-memory UI state. onClear force-
	// writes the empty transcript immediately (a plain autosave of [] is refused
	// as an accidental truncation) so a daemon restart cannot resurrect the old
	// conversation, then purges the rotated backups, or transcript.Load's
	// corruption-recovery would resurrect the cleared conversation from a .bak.
	if s.onClear != nil {
		s.onClear()
	}
}

// resend retries the last user turn (the /resend command) — runs like send.
func (s *Session) resend() bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.status = StatusWorking
	s.turnStarted = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	sess := s.sess
	s.mu.Unlock()

	go s.runTurn(ctx, func() (string, error) { return sess.Resend(ctx) })
	return true
}

// setModel performs a live provider switch for the session. The caller passes
// the rebuilt provider + compactor + budget (package main owns provider
// construction). modelID updates the session's listed model.
func (s *Session) setModel(modelID string, p llm.Provider, c llm.Compactor, budget int) {
	s.agent.SetLive(p, c, budget)
	s.mu.Lock()
	s.Model = modelID
	s.mu.Unlock()
}

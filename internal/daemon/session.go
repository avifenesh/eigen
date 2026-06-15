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

	mu      sync.Mutex
	agent   *agent.Agent
	sess    *agent.Session
	status  Status
	title   string
	updated time.Time

	// lastAttached: when a view last attached — "last used by ME" for list
	// ordering (transcript mtime lies; the titler touches files).
	lastAttached time.Time
	onAttach     func() // host hook: persist meta when a view attaches

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

	cancel  context.CancelFunc // cancels the in-flight turn (interrupt)
	running bool
	titling bool   // a title request is already in flight
	onClose func() // releases the session's external resources (MCP/LSP/observe)

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
	s := &Session{
		ID:      id,
		Dir:     dir,
		Model:   model,
		agent:   a,
		sess:    a.NewSession(),
		status:  StatusIdle,
		updated: time.Now(),
		subs:    map[int]chan agent.Event{},
	}
	// Fan out agent events to all attached views + record for replay,
	// composing the agent's host wrap (observability + hooks) so those run
	// in the daemon — sessions are observable with zero or many views.
	if a.EventWrap != nil {
		a.OnEvent = a.EventWrap(s.dispatch)
	} else {
		a.OnEvent = s.dispatch
	}
	s.installApprover()
	return s
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
	subs := make([]chan agent.Event, 0, len(s.subs))
	for _, ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default: // a slow view must not stall the agent loop; it can resync
		}
	}
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
		s.mu.Lock()
		if c, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(c)
		}
		s.mu.Unlock()
	}
}

// send runs a turn on the session (one at a time). It returns immediately;
// progress arrives via events. A turn already running is rejected.
func (s *Session) send(task string, images []llm.Image) bool {
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
}

// backgroundedNotifyMin is the minimum turn length worth a desktop notification
// when no view is attached — a short backgrounded turn isn't worth a popup. A
// var so tests can shrink it.
var backgroundedNotifyMin = 10 * time.Second

// interrupt cancels the in-flight turn, if any.
func (s *Session) interrupt() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
}

// info snapshots the session for listing.
func (s *Session) info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	title := s.title
	if title == "" {
		// Display fallback while the model title hasn't landed (or failed):
		// a snippet of the first user message beats "(untitled)".
		for _, m := range s.sess.Messages() {
			if m.Role == llm.RoleUser && strings.TrimSpace(m.Text) != "" {
				title = snippet(m.Text, 48)
				break
			}
		}
	}
	return SessionInfo{
		ID:      s.ID,
		Title:   title,
		Dir:     s.Dir,
		Model:   s.Model,
		Status:  s.status,
		Turns:   len(s.sess.Messages()),
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

var _ = llm.RoleUser // keep llm imported for future message-typed protocol

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

// state snapshots everything a remote chat UI needs (history + status).
func (s *Session) state() *SessionState {
	s.mu.Lock()
	a := s.agent
	sess := s.sess
	model := s.Model
	title := s.title
	running := s.running
	s.mu.Unlock()
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
	}
	if a.Tools != nil {
		for _, d := range a.Tools.Definitions() {
			st.Tools = append(st.Tools, ToolInfo{Name: d.Name, ReadOnly: d.ReadOnly})
		}
	}
	st.Roots = a.Roots()
	return st
}

// setPerm/setGoal mutate session state (the agent's setters are mutex-guarded).
func (s *Session) setPerm(p string) { s.agent.SetPerm(agent.Permission(p)) }
func (s *Session) setGoal(g string) { s.agent.SetGoal(g) }

// addDir extends the session's tool sandbox (user-invoked /add-dir grant).
// Returns the normalized root added; the agent's Policy guards its concurrency.
func (s *Session) addDir(path string) (string, error) { return s.agent.AddDir(path) }

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

// compact summarizes toward target tokens (0 = the agent's default policy).
func (s *Session) compact(ctx context.Context, target int) (int, int, error) {
	return s.sess.Compact(ctx, target)
}

// resume replaces the session's conversation with imported history (the
// --resume path: the view imports a transcript and hands it to the daemon).
func (s *Session) resume(history []llm.Message) {
	s.mu.Lock()
	s.sess = s.agent.Resume(history)
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
	s.mu.Unlock()
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

package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/daemon"
	"github.com/avifenesh/eigen/internal/llm"
)

// Remote runs the conversation in the eigen daemon: the agent loop, tools,
// memory, and approvals all live there. This backend forwards input/commands
// over the socket and feeds the daemon's event stream into the TUI sink —
// the SAME rich chat UI drives local and daemon-hosted sessions.
type Remote struct {
	c  *daemon.Client
	id string // daemon session id

	mu    sync.Mutex
	state *daemon.SessionState // last-synced snapshot (refreshed around turns)
	sink  agent.EventSink

	// turn signalling: Send blocks until the daemon reports the turn ended
	// (done event, or a terminal note for interrupt/error).
	turnDone chan struct{}
	lastText string
	lastErr  error // daemon-side turn error (from the terminal note)

	// detached: the view left (session hop / window close). Blocked Sends
	// return, events are dropped, and ctx cancellation must NOT interrupt the
	// daemon-side turn (it keeps running without us).
	detached bool
}

// NewRemote attaches to a daemon session as a chat backend. The returned
// backend is not usable until Wire is called (which subscribes to events).
func NewRemote(c *daemon.Client, sessionID string) (*Remote, error) {
	st, err := c.State(sessionID)
	if err != nil {
		return nil, fmt.Errorf("session state: %w", err)
	}
	return &Remote{c: c, id: sessionID, state: st}, nil
}

// Wire subscribes to the daemon's event stream. persist is ignored — the
// daemon owns persistence. Replayed events are NOT forwarded to the sink (the
// TUI renders history from Messages()); live events stream through.
func (r *Remote) Wire(sink agent.EventSink, _ func([]llm.Message)) {
	r.mu.Lock()
	r.sink = sink
	r.mu.Unlock()
	_ = r.c.Attach(r.id, func(e daemon.WireEvent, replay bool) {
		if replay {
			return
		}
		ev := wireToEvent(e)
		// Track turn completion for the blocking Send. An error terminal note
		// carries the daemon-side turn error — surface it as Send's error so
		// the TUI's failover/rate-limit handling works on daemon sessions.
		if e.Kind == "done" || (e.Kind == "note" && isTerminalNote(e.Text)) {
			r.mu.Lock()
			r.lastText = e.Text
			r.lastErr = nil
			if e.Kind == "note" && strings.HasPrefix(e.Text, "error: ") {
				r.lastErr = errors.New(strings.TrimPrefix(e.Text, "error: "))
			}
			ch := r.turnDone
			r.turnDone = nil
			r.mu.Unlock()
			if ch != nil {
				close(ch)
			}
		}
		r.mu.Lock()
		s := r.sink
		r.mu.Unlock()
		if s != nil {
			s(ev)
		}
	})
}

// isTerminalNote reports whether a note event ends a turn (the daemon emits
// these for interrupts and errors, which return without an EventDone).
func isTerminalNote(text string) bool {
	return text == "interrupted" || strings.HasPrefix(text, "error: ")
}

// wireToEvent maps the socket event shape back to an agent.Event.
func wireToEvent(e daemon.WireEvent) agent.Event {
	var k agent.EventKind
	switch e.Kind {
	case "text":
		k = agent.EventTextDelta
	case "reasoning":
		k = agent.EventReasoningDelta
	case "tool_start":
		k = agent.EventToolStart
	case "tool_result":
		k = agent.EventToolResult
	case "done":
		k = agent.EventDone
	case "approval":
		k = agent.EventApproval
	case "bg_done":
		k = agent.EventBgDone // wake signal; the sibling note renders the text
	default:
		k = agent.EventNote
	}
	return agent.Event{Kind: k, Step: e.Step, Text: e.Text, ToolName: e.ToolName, ToolID: e.ToolID, ToolArgs: e.ToolArgs, Result: e.Result, IsError: e.IsError, InTokens: e.InTokens, OutTokens: e.OutTokens}
}

// Send forwards the task and blocks until the daemon reports the turn ended,
// mirroring the local backend's contract (progress streams via events). Images
// are carried over the socket via the input op.
func (r *Remote) Send(ctx context.Context, task string, images []llm.Image) (string, error) {
	ch := make(chan struct{})
	r.mu.Lock()
	r.turnDone = ch
	r.mu.Unlock()
	if err := r.c.Input(r.id, task, images); err != nil {
		r.mu.Lock()
		r.turnDone = nil
		r.mu.Unlock()
		return "", err
	}
	select {
	case <-ch:
	case <-ctx.Done():
		// A detached view's context cancel is just the view leaving — the
		// daemon keeps running the turn. Only a live view's esc interrupts.
		if !r.isDetached() {
			_ = r.c.Interrupt(r.id)
		}
		<-ch // the daemon emits a terminal note after the interrupt lands
	}
	r.refresh()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastText, r.lastErr
}

// Resend asks the daemon to retry the last turn, blocking until it ends.
func (r *Remote) Resend(ctx context.Context) (string, error) {
	ch := make(chan struct{})
	r.mu.Lock()
	r.turnDone = ch
	r.mu.Unlock()
	if err := r.c.Resend(r.id); err != nil {
		r.mu.Lock()
		r.turnDone = nil
		r.mu.Unlock()
		return "", err
	}
	select {
	case <-ch:
	case <-ctx.Done():
		if !r.isDetached() {
			_ = r.c.Interrupt(r.id)
		}
		<-ch
	}
	r.refresh()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastText, r.lastErr
}

func (r *Remote) isDetached() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.detached
}

// refresh re-syncs the cached state snapshot (called after turns/mutations).
func (r *Remote) refresh() {
	if st, err := r.c.State(r.id); err == nil {
		r.mu.Lock()
		r.state = st
		r.mu.Unlock()
	}
}

func (r *Remote) snap() *daemon.SessionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *Remote) Messages() []llm.Message { return r.snap().Messages }
func (r *Remote) Tokens() int             { return r.snap().Tokens }
func (r *Remote) Running() bool           { return r.snap().Running }

func (r *Remote) Compact(ctx context.Context, target int) (int, int, error) {
	before, after, err := r.c.Compact(r.id, target)
	r.refresh()
	return before, after, err
}

func (r *Remote) ModelID() string      { return r.snap().Model }
func (r *Remote) ProviderName() string { return r.snap().Provider }

// SetModel switches the daemon session's model. The provider cannot cross the
// socket, so its ModelID() (the raw, resolvable id — NOT Name(), whose
// "(bedrock converse)" suffix would break llm.New) is sent and the daemon
// rebuilds the provider server-side; compactor/budget are derived there too.
func (r *Remote) SetModel(p llm.Provider, c llm.Compactor, maxTokens int) {
	if p == nil {
		return
	}
	_ = r.c.SetModel(r.id, p.ModelID())
	r.refresh()
}

func (r *Remote) MaxContextTokens() int { return r.snap().MaxTokens }

func (r *Remote) Perm() agent.Permission { return agent.Permission(r.snap().Perm) }
func (r *Remote) SetPerm(p agent.Permission) {
	_ = r.c.SetPerm(r.id, string(p))
	r.refresh()
}

func (r *Remote) Goal() string { return r.snap().Goal }
func (r *Remote) SetGoal(g string) {
	_ = r.c.SetGoal(r.id, g)
	r.refresh()
}

func (r *Remote) Title() string { return r.snap().Title }
func (r *Remote) SetTitle(t string) {
	_ = r.c.SetTitle(r.id, t)
	r.refresh()
}

func (r *Remote) Tools() []ToolInfo {
	st := r.snap()
	out := make([]ToolInfo, 0, len(st.Tools))
	for _, t := range st.Tools {
		out = append(out, ToolInfo{Name: t.Name, ReadOnly: t.ReadOnly})
	}
	return out
}

// Shells lists the daemon session's backgrounded bash shells (from the snapshot).
func (r *Remote) Shells() []ShellInfo {
	st := r.snap()
	out := make([]ShellInfo, 0, len(st.Shells))
	for _, s := range st.Shells {
		out = append(out, ShellInfo{ID: s.ID, Command: s.Command, Status: s.Status, ExitCode: s.ExitCode, LastLine: s.LastLine})
	}
	return out
}

// KillShell stops a backgrounded shell by id over the socket, then refreshes.
func (r *Remote) KillShell(id string) bool {
	killed, err := r.c.KillShell(r.id, id)
	if err != nil {
		return false
	}
	r.refresh()
	return killed
}

// DetachBash backgrounds the foreground bash command running in the daemon
// session's turn (over the socket), then refreshes so the shells panel updates.
func (r *Remote) DetachBash() bool {
	detached, err := r.c.DetachBash(r.id)
	if err != nil {
		return false
	}
	if detached {
		r.refresh()
	}
	return detached
}

// AddDir extends the daemon session's tool sandbox (user /add-dir grant) and
// refreshes the snapshot so Roots reflects it.
func (r *Remote) AddDir(path string) (string, error) {
	root, err := r.c.AddDir(r.id, path)
	if err != nil {
		return "", err
	}
	r.refresh()
	return root, nil
}

// Roots lists the daemon session's tool-sandbox allowed dirs (primary first).
func (r *Remote) Roots() []string { return r.snap().Roots }

// Steer injects a message into the daemon session's running turn (mid-turn,
// between tool-call rounds). Returns true when a turn was running and the
// message was steered; false when idle (caller should Send). Fire-and-forget —
// never blocks on the turn (unlike Send).
func (r *Remote) Steer(text string, images []llm.Image) bool {
	steered, err := r.c.SteerInput(r.id, text, images)
	if err != nil {
		return false
	}
	r.refresh()
	return steered
}

// Provider is nil for remote backends: capability checks that need the live
// provider (vision, effort, search) degrade gracefully in the TUI.
func (r *Remote) Provider() llm.Provider { return nil }

// Reset replaces the daemon session's conversation: empty history is /clear,
// non-empty is /resume (the daemon imports the transcript and persists it).
func (r *Remote) Reset(history []llm.Message) {
	if len(history) == 0 {
		_ = r.c.Clear(r.id)
	} else {
		_ = r.c.ResetTo(r.id, history)
	}
	r.refresh()
}

// Answer resolves a pending approval on the daemon session.
func (r *Remote) Answer(approvalID string, allow bool) {
	_ = r.c.Approve(r.id, approvalID, allow)
}

// Effort returns the daemon session's reasoning-effort level ("" = none).
func (r *Remote) Effort() string { return r.snap().Effort }

// SetEffort switches reasoning effort on the daemon's provider.
func (r *Remote) SetEffort(level string) bool {
	if err := r.c.SetEffort(r.id, level); err != nil {
		return false
	}
	r.refresh()
	return true
}

// SearchMode returns the daemon session's live-search mode ("" = none).
func (r *Remote) SearchMode() string { return r.snap().Search }

// SetSearch switches live search on the daemon's provider.
func (r *Remote) SetSearch(mode string) bool {
	if err := r.c.SetSearch(r.id, mode); err != nil {
		return false
	}
	r.refresh()
	return true
}

// FastSupported reports whether the daemon session's model has a fast tier.
func (r *Remote) FastSupported() bool { return r.snap().FastOK }

// FastMode reports whether the fast/priority service tier is active.
func (r *Remote) FastMode() bool { return r.snap().Fast }

// SetFast toggles the fast/priority service tier on the daemon's provider.
func (r *Remote) SetFast(on bool) bool {
	if err := r.c.SetFast(r.id, on); err != nil {
		return false
	}
	r.refresh()
	return true
}

// SessionID returns the daemon session id this backend drives.
func (r *Remote) SessionID() string { return r.id }

// Sessions lists the daemon's sessions for the in-window switcher.
func (r *Remote) Sessions() []SessionEntry {
	infos, err := r.c.List()
	if err != nil {
		return nil
	}
	out := make([]SessionEntry, 0, len(infos))
	for _, in := range infos {
		out = append(out, SessionEntry{
			ID:      in.ID,
			Title:   in.Title,
			Dir:     in.Dir,
			Model:   in.Model,
			Status:  string(in.Status),
			Turns:   in.Turns,
			Views:   in.Views,
			Updated: in.Updated,
		})
	}
	return out
}

// Detach releases the view from the session WITHOUT touching the running
// turn: a blocked Send returns immediately (the daemon keeps working), and
// later events are ignored. The TUI calls this before hopping to another
// session or back to the app.
func (r *Remote) Detach() {
	r.mu.Lock()
	r.detached = true
	r.sink = nil
	ch := r.turnDone
	r.turnDone = nil
	r.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// Interrupt cancels the session's in-flight turn on the daemon — used when a
// view that did NOT start the turn (attached to an already-running session)
// presses esc. The terminal event then arrives over the live stream.
func (r *Remote) Interrupt() error { return r.c.Interrupt(r.id) }

package chat

import (
	"context"
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
		// Track turn completion for the blocking Send.
		if e.Kind == "done" || (e.Kind == "note" && isTerminalNote(e.Text)) {
			r.mu.Lock()
			r.lastText = e.Text
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
	default:
		k = agent.EventNote
	}
	return agent.Event{Kind: k, Step: e.Step, Text: e.Text, ToolName: e.ToolName, Result: e.Result, IsError: e.IsError}
}

// Send forwards the task and blocks until the daemon reports the turn ended,
// mirroring the local backend's contract (progress streams via events).
// Images are not yet carried over the socket.
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
		_ = r.c.Interrupt(r.id)
		<-ch // the daemon emits a terminal note after the interrupt lands
	}
	r.refresh()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastText, nil
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
		_ = r.c.Interrupt(r.id)
		<-ch
	}
	r.refresh()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastText, nil
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

func (r *Remote) Compact(ctx context.Context, target int) (int, int, error) {
	before, after, err := r.c.Compact(r.id, target)
	r.refresh()
	return before, after, err
}

func (r *Remote) ModelID() string      { return r.snap().Model }
func (r *Remote) ProviderName() string { return r.snap().Provider }

// SetModel switches the daemon session's model. The provider cannot cross the
// socket, so its Name() (the model id) is sent and the daemon rebuilds the
// provider server-side; compactor/budget are derived there too.
func (r *Remote) SetModel(p llm.Provider, c llm.Compactor, maxTokens int) {
	if p == nil {
		return
	}
	_ = r.c.SetModel(r.id, p.Name())
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

func (r *Remote) Tools() []ToolInfo {
	st := r.snap()
	out := make([]ToolInfo, 0, len(st.Tools))
	for _, t := range st.Tools {
		out = append(out, ToolInfo{Name: t.Name, ReadOnly: t.ReadOnly})
	}
	return out
}

// Provider is nil for remote backends: capability checks that need the live
// provider (vision, effort, search) degrade gracefully in the TUI.
func (r *Remote) Provider() llm.Provider { return nil }

// Reset clears the daemon session's conversation (the /clear command). A
// non-empty history (the /resume path) is not supported remotely — the daemon
// owns history — so only the clear case is honored.
func (r *Remote) Reset(history []llm.Message) {
	if len(history) == 0 {
		_ = r.c.Clear(r.id)
		r.refresh()
	}
}

// Answer resolves a pending approval on the daemon session.
func (r *Remote) Answer(approvalID string, allow bool) {
	_ = r.c.Approve(r.id, approvalID, allow)
}

package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
)

// Local runs the conversation on an in-process agent — today's standalone
// chat, unchanged in behavior. It is a thin adapter: every method delegates to
// the agent/session the TUI used to touch directly.
type Local struct {
	a *agent.Agent
	s *agent.Session

	mu      sync.Mutex
	modelID string // current model id (live switches update it)

	// pending gated-tool approvals (surfaced as EventApproval, answered by id)
	approvals   map[string]chan bool
	approvalSeq int
}

// NewLocal wraps an agent and an optional resumed history into a Backend.
func NewLocal(a *agent.Agent, history []llm.Message, modelID string) *Local {
	var s *agent.Session
	if len(history) > 0 {
		s = a.Resume(history)
	} else {
		s = a.NewSession()
	}
	return &Local{a: a, s: s, modelID: modelID}
}

func (l *Local) Send(ctx context.Context, task string, images []llm.Image) (string, error) {
	return l.s.SendWith(ctx, task, images)
}

func (l *Local) Resend(ctx context.Context) (string, error) { return l.s.Resend(ctx) }

func (l *Local) Messages() []llm.Message { return l.s.Messages() }
func (l *Local) Tokens() int             { return l.s.Tokens() }

func (l *Local) Compact(ctx context.Context, targetTokens int) (int, int, error) {
	return l.s.Compact(ctx, targetTokens)
}

func (l *Local) ModelID() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.modelID
}

func (l *Local) ProviderName() string {
	if l.a.Provider == nil {
		return ""
	}
	return l.a.Provider.Name()
}

// SetModel performs a live provider switch (the /model command). modelID is
// tracked here so the status bar follows.
func (l *Local) SetModel(p llm.Provider, c llm.Compactor, maxTokens int) {
	l.a.SetLive(p, c, maxTokens)
	l.mu.Lock()
	l.modelID = p.Name()
	l.mu.Unlock()
}

func (l *Local) MaxContextTokens() int      { return l.a.MaxContextTokens }
func (l *Local) Perm() agent.Permission     { return l.a.Perm }
func (l *Local) SetPerm(p agent.Permission) { l.a.SetPerm(p) }
func (l *Local) Goal() string               { return l.a.CurrentGoal() }
func (l *Local) SetGoal(g string)           { l.a.SetGoal(g) }

func (l *Local) Tools() []ToolInfo {
	if l.a.Tools == nil {
		return nil
	}
	defs := l.a.Tools.Definitions()
	out := make([]ToolInfo, 0, len(defs))
	for _, d := range defs {
		out = append(out, ToolInfo{Name: d.Name, ReadOnly: d.ReadOnly})
	}
	return out
}

func (l *Local) Provider() llm.Provider { return l.a.Provider }

// Agent exposes the underlying agent for the few main-side wiring needs
// (EventWrap composition, subtask hookup) that predate the seam. The TUI must
// NOT use this — it talks to the Backend interface only.
func (l *Local) Agent() *agent.Agent { return l.a }

// Reset replaces the conversation (the /resume and /clear commands).
func (l *Local) Reset(history []llm.Message) {
	if len(history) > 0 {
		l.s = l.a.Resume(history)
	} else {
		l.s = l.a.NewSession()
	}
}

// Wire connects the agent's callbacks to the UI: events flow to sink, every
// appended message triggers persist, and gated tool calls surface as
// EventApproval events answered via Answer — the same shape a remote daemon
// session uses, so the TUI has ONE approval path.
func (l *Local) Wire(sink agent.EventSink, persist func([]llm.Message)) {
	l.a.OnEvent = sink
	l.a.Persist = persist
	l.a.Approve = func(ctx context.Context, name string, args json.RawMessage) (bool, error) {
		l.mu.Lock()
		l.approvalSeq++
		id := fmt.Sprintf("a%d", l.approvalSeq)
		ch := make(chan bool, 1)
		if l.approvals == nil {
			l.approvals = map[string]chan bool{}
		}
		l.approvals[id] = ch
		l.mu.Unlock()
		defer func() {
			l.mu.Lock()
			delete(l.approvals, id)
			l.mu.Unlock()
		}()
		sink(agent.Event{Kind: agent.EventApproval, Text: name + " " + string(args), ToolName: name, Result: id})
		select {
		case ok := <-ch:
			return ok, nil
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// Answer resolves a pending approval by id (no-op for unknown ids).
func (l *Local) Answer(approvalID string, allow bool) {
	l.mu.Lock()
	ch := l.approvals[approvalID]
	l.mu.Unlock()
	if ch != nil {
		select {
		case ch <- allow:
		default:
		}
	}
}

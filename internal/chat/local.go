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
	title   string // user-set session title (/rename); "" = derived
	running bool   // a turn is in flight (so Steer can inject vs Send)

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
	l.mu.Lock()
	l.running = true
	l.mu.Unlock()
	out, err := l.s.SendWith(ctx, task, images)
	l.mu.Lock()
	l.running = false
	l.mu.Unlock()
	return out, err
}

// Steer injects a message mid-turn when a turn is running (drained between
// tool-call rounds); returns false when idle so the caller starts a new turn.
func (l *Local) Steer(text string, images []llm.Image) bool {
	l.mu.Lock()
	running := l.running
	l.mu.Unlock()
	if !running {
		return false
	}
	l.s.Steer(text, images)
	return true
}

func (l *Local) Resend(ctx context.Context) (string, error) { return l.s.Resend(ctx) }

func (l *Local) Messages() []llm.Message { return l.s.Messages() }
func (l *Local) Tokens() int             { return l.s.Tokens() }

// Running is always false for a local backend: the TUI itself drives Send, so
// no turn is ever in flight before the UI starts (unlike a remote daemon
// session another view may already be running).
func (l *Local) Running() bool { return false }

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

// SetModel performs a live provider switch (the /model command). The raw
// ModelID() (NOT Name(), whose "(zhipu glm)" suffix would diverge from the
// daemon-attached Remote, which tracks ModelID()) is recorded so the status bar
// shows the same id for a local chat and an attached session on the same model.
func (l *Local) SetModel(p llm.Provider, c llm.Compactor, maxTokens int) {
	l.a.SetLive(p, c, maxTokens)
	l.mu.Lock()
	l.modelID = p.ModelID()
	l.mu.Unlock()
}

func (l *Local) MaxContextTokens() int      { return l.a.MaxContextTokens }
func (l *Local) Perm() agent.Permission     { return l.a.Perm }
func (l *Local) SetPerm(p agent.Permission) { l.a.SetPerm(p) }
func (l *Local) Goal() string               { return l.a.CurrentGoal() }
func (l *Local) SetGoal(g string)           { l.a.SetGoal(g) }

func (l *Local) Title() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.title
}
func (l *Local) SetTitle(t string) {
	l.mu.Lock()
	l.title = t
	l.mu.Unlock()
}

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

// SetTurnTools restricts the next turn to the given tool names (slash command
// allowed-tools); the agent consumes and clears it per turn.
func (l *Local) SetTurnTools(names []string) { l.s.SetTurnTools(names) }

// Shells lists the agent's backgrounded bash shells.
func (l *Local) Shells() []ShellInfo {
	if l.a.Shells == nil {
		return nil
	}
	infos := l.a.Shells.Infos()
	out := make([]ShellInfo, 0, len(infos))
	for _, s := range infos {
		out = append(out, ShellInfo{ID: s.ID, Command: s.Command, Status: s.Status, ExitCode: s.ExitCode, Started: s.Started, Finished: s.Finished, LastLine: s.LastLine})
	}
	return out
}

// KillShell stops a backgrounded shell by id.
func (l *Local) KillShell(id string) bool {
	if l.a.Shells == nil {
		return false
	}
	return l.a.Shells.KillByID(id)
}

// DetachBash backgrounds the bash command running in the current turn.
func (l *Local) DetachBash() bool { return l.a.DetachBash() }

// AddDir extends the tool sandbox (user-invoked /add-dir grant).
func (l *Local) AddDir(path string) (string, error) { return l.a.AddDir(path) }

// Roots lists the tool sandbox's allowed directories (primary first).
func (l *Local) Roots() []string { return l.a.Roots() }

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

// Effort returns the provider's reasoning-effort level ("" = unsupported).
func (l *Local) Effort() string {
	if es, ok := l.a.Provider.(llm.EffortSetter); ok {
		return es.Effort()
	}
	return ""
}

// SetEffort sets the reasoning effort; false = unsupported or unknown level.
func (l *Local) SetEffort(level string) bool {
	if es, ok := l.a.Provider.(llm.EffortSetter); ok {
		return es.SetEffort(level)
	}
	return false
}

// SearchMode returns the provider's live-search mode ("" = unsupported).
func (l *Local) SearchMode() string {
	if sr, ok := l.a.Provider.(llm.Searcher); ok {
		return sr.SearchMode()
	}
	return ""
}

// SetSearch sets the live-search mode; false = unsupported or unknown mode.
func (l *Local) SetSearch(mode string) bool {
	if sr, ok := l.a.Provider.(llm.Searcher); ok {
		return sr.SetSearch(mode)
	}
	return false
}

// FastSupported reports whether the active model has a fast/low-latency tier.
func (l *Local) FastSupported() bool {
	_, ok := l.a.Provider.(llm.FastModer)
	return ok
}

// FastMode reports whether the fast (priority) service tier is active.
func (l *Local) FastMode() bool {
	if fm, ok := l.a.Provider.(llm.FastModer); ok {
		return fm.FastMode()
	}
	return false
}

// SetFast toggles the fast/priority service tier; false = unsupported.
func (l *Local) SetFast(on bool) bool {
	if fm, ok := l.a.Provider.(llm.FastModer); ok {
		return fm.SetFast(on)
	}
	return false
}

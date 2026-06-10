// Package agent implements eigen's tool-use loop: drive a provider, execute the
// tool calls it returns, feed results back, and repeat until the model is done.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
	"sync"
)

// Permission is the loop's autonomy posture.
type Permission string

const (
	// PermGated auto-runs read-only tools and asks before mutating ones.
	PermGated Permission = "gated"
	// PermAuto runs every tool without prompting.
	PermAuto Permission = "auto"
)

const systemPrompt = `You are eigen, a coding agent that works directly in the user's project.

Working method:
- Explore before editing: use grep, glob, symbols, and tree to locate relevant code.
- For multi-step work, use the todo tool to lay out and update a short plan; keep one task in_progress.
- Make focused edits with edit/multiedit/apply_patch; create files with write.
- After changing code, use diff to review your changes before reporting.
- When a task matches an available skill (listed below), load it with the skill tool first.
- Record durable, project-specific facts (build/test commands, conventions, gotchas) with the memory tool.
- For a large, separable chunk of work, delegate it with the task tool (a fresh, isolated subtask).

Call tools as needed; when the task is complete, reply with a short, specific summary of what you did.`

// Approver decides whether a mutating tool call may run in gated mode. It is
// context-aware so a UI can cancel a pending prompt, and returns an error
// distinct from a plain "no".
type Approver func(ctx context.Context, name string, args json.RawMessage) (bool, error)

// EventKind classifies an agent event.
type EventKind int

const (
	EventTextDelta      EventKind = iota // streamed assistant text
	EventReasoningDelta                  // streamed reasoning summary
	EventToolStart                       // a tool call is about to run
	EventToolResult                      // a tool finished
	EventDone                            // the loop produced its final answer
	EventNote                            // an out-of-band notice for the user (e.g. compaction stalled)
)

// Event is a structured observation emitted during a run. A CLI prints it; a
// TUI renders it. It is the single seam between the loop and any front-end.
type Event struct {
	Kind     EventKind
	Step     int
	Text     string          // delta text, or final answer for EventDone
	ToolName string          // EventToolStart / EventToolResult
	ToolID   string          // EventToolStart / EventToolResult
	ToolArgs json.RawMessage // EventToolStart
	Result   string          // EventToolResult
	IsError  bool            // EventToolResult
}

// EventSink receives agent events. It must not block for long.
type EventSink func(Event)

// Agent drives a provider through the tool-use loop.
//
// Concurrency: the TUI intentionally allows live config switches while a turn
// is running (/model, /perm, ctrl+a/e/o, overload failover), which write
// Provider/Perm/Compactor/MaxContextTokens from the UI goroutine while the
// agent goroutine reads them. Those four fields are therefore guarded by mu:
// the loop reads them via the private accessors and the TUI writes them via
// SetLive/SetPerm/SetMaxContextTokens. The remaining fields are configured
// before the first turn and never mutated live.
type Agent struct {
	mu sync.RWMutex

	Provider llm.Provider
	Tools    *tool.Registry
	Perm     Permission
	// MaxSteps optionally caps tool-use iterations as a runaway guard. The
	// default (0) means unlimited: the loop runs until the model produces a
	// final answer, and is interrupted only by canceling the context (esc in
	// the TUI). Set a positive value to bound a single task.
	MaxSteps int
	Approve  Approver

	// MaxContextTokens, if > 0, bounds the conversation sent to the model: at
	// the start of each turn the transcript is compacted to fit. This is the
	// single compaction mechanism for both live growth and resuming a large
	// session.
	MaxContextTokens int

	// Compactor, if set, summarizes older history when MaxContextTokens is
	// exceeded (model-generated structured summary). If nil, compaction falls
	// back to the deterministic recency window.
	Compactor llm.Compactor

	// OnEvent, if set, receives the structured event stream (deltas, tool
	// lifecycle, final answer). Streaming deltas only appear if the provider
	// implements llm.Streamer.
	OnEvent EventSink

	// ExtraSystem is appended to the base system prompt (e.g. the skills
	// catalog), so the model knows about capabilities discovered at runtime.
	ExtraSystem string

	// Memory is durable per-project notes appended to the system prompt, so the
	// agent recalls prior learnings across sessions.
	Memory string

	// Goal, when set, is a persistent north-star injected into the system
	// prompt every step. Unlike a user message it can never be paraphrased away
	// by compaction or buried under tool output — the agent re-reads it each
	// turn. Guarded by mu (live-settable via /goal).
	Goal string

	// Persist, if set, is called with the full conversation after every message
	// is appended (from the same goroutine that owns the session), so a session
	// can be autosaved continuously and race-free.
	Persist func([]llm.Message)
}

// maxToolOutput caps a single tool result fed back to the model, so a runaway
// tool (huge file, verbose command) can't blow up memory or the next request.
const maxToolOutput = 100_000

// maxEmptyTurns bounds how many times we nudge the model after it returns a
// turn with neither tool calls nor text (e.g. a reasoning-only response),
// preventing both a premature empty exit and an infinite spin.
const maxEmptyTurns = 2

// Session holds a running conversation so the agent can be driven turn by turn
// (e.g. a REPL/TUI), preserving history across user inputs.
type Session struct {
	a    *Agent
	msgs []llm.Message

	// Circuit-breaker state for auto-compaction. compactStall is set when a
	// compaction left too little headroom under budget to be worth repeating;
	// while set, drive() skips auto-compaction (the user is nudged to /clear or
	// refocus) until the context drops back under budget on its own.
	lastCompactBefore int // estimated tokens before the last auto-compaction
	lastCompactAfter  int // estimated tokens after it
	compactStall      bool
}

// NewSession starts an empty conversation.
func (a *Agent) NewSession() *Session { return &Session{a: a} }

// --- live-switchable config (guarded by mu) ---------------------------------
// The TUI switches these mid-turn (live /model, ctrl+a perm toggle, overload
// failover); the loop goroutine reads them per step via the accessors below.

// SetLive atomically swaps the provider and its paired compactor, and (when
// budget > 0) the context budget — everything a live model switch changes.
func (a *Agent) SetLive(p llm.Provider, c llm.Compactor, budget int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Provider = p
	a.Compactor = c
	if budget > 0 {
		a.MaxContextTokens = budget
	}
}

// SetPerm switches the permission posture live.
func (a *Agent) SetPerm(p Permission) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Perm = p
}

// SetGoal sets or clears (empty string) the persistent goal live.
func (a *Agent) SetGoal(g string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Goal = g
}

// CurrentGoal returns the goal (live-safe read).
func (a *Agent) CurrentGoal() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Goal
}

// SetMaxContextTokens adjusts the context budget live.
func (a *Agent) SetMaxContextTokens(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.MaxContextTokens = n
}

func (a *Agent) provider() llm.Provider {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Provider
}

func (a *Agent) compactor() llm.Compactor {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Compactor
}

func (a *Agent) perm() Permission {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Perm
}

func (a *Agent) maxContextTokens() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.MaxContextTokens
}

// Resume starts a session pre-seeded with prior messages (e.g. an imported or
// saved transcript), so the next Send continues that conversation.
func (a *Agent) Resume(msgs []llm.Message) *Session {
	return &Session{a: a, msgs: msgs}
}

// subtaskDepthKey bounds Subtask recursion via the context.
type subtaskDepthKey struct{}

// maxSubtaskDepth caps how deeply subtasks may nest.
const maxSubtaskDepth = 2

// Subtask runs task on a fresh session of (a copy of) this agent, with event
// emission suppressed so a delegated subtask does not clutter the caller's
// transcript. Recursion is bounded so subtasks cannot spawn unboundedly.
func (a *Agent) Subtask(ctx context.Context, task string) (string, error) {
	depth, _ := ctx.Value(subtaskDepthKey{}).(int)
	if depth >= maxSubtaskDepth {
		return "", fmt.Errorf("subtask depth limit (%d) reached", maxSubtaskDepth)
	}
	// Construct the sub-agent explicitly (not by struct copy: Agent embeds a
	// mutex). It inherits the live provider/perm/budget snapshot but stays
	// silent (no OnEvent) and does not persist (its session is ephemeral).
	sub := &Agent{
		Provider:         a.provider(),
		Tools:            a.Tools,
		Perm:             a.perm(),
		MaxSteps:         a.MaxSteps,
		Approve:          a.Approve,
		MaxContextTokens: a.maxContextTokens(),
		Compactor:        a.compactor(),
		ExtraSystem:      a.ExtraSystem,
		Memory:           a.Memory,
	}
	ctx = context.WithValue(ctx, subtaskDepthKey{}, depth+1)
	return sub.NewSession().Send(ctx, task)
}

// Messages returns the conversation so far (for saving / live-replace handoff).
// It returns the session's live slice, NOT a copy: callers must only read it,
// and only from the goroutine that owns the session (the TUI calls it at safe
// points — turn finished or not started). Use the Persist hook for continuous
// race-free saving during a turn.
func (s *Session) Messages() []llm.Message { return s.msgs }

// Compact summarizes the conversation now, on demand (e.g. the /compact
// command), regardless of the token budget. It replaces older history with a
// model-generated summary, keeping recent turns verbatim. targetTokens bounds
// the result; when <= 0 it uses the agent's MaxContextTokens, or a conservative
// default. Returns the message counts before/after so the caller can report it.
func (s *Session) Compact(ctx context.Context, targetTokens int) (before, after int, err error) {
	a := s.a
	before = len(s.msgs)
	if before == 0 {
		return 0, 0, nil
	}
	budget := targetTokens
	if budget <= 0 {
		budget = a.maxContextTokens()
	}
	if budget <= 0 {
		budget = 120_000
	}
	// Force a summarization even if we're under budget: aim the compactor at a
	// fraction of the budget so /compact meaningfully shrinks the context.
	target := budget / 2
	compacted, cerr := llm.CompactWith(ctx, a.compactor(), s.msgs, target)
	if cerr != nil {
		return before, before, cerr
	}
	s.msgs = compacted
	s.persist()
	return before, len(s.msgs), nil
}

// Tokens returns the current estimated context size of the conversation.
func (s *Session) Tokens() int { return llm.EstimateTokens(s.msgs) }

// compactStallHeadroomFrac is the minimum headroom (as a fraction of the
// budget) a compaction must leave to be considered worthwhile. If compaction
// lands within this margin of the budget, it will just re-trip next turn, so
// the circuit breaker trips instead of summarizing every turn for no gain.
const compactStallHeadroomFrac = 0.15

// maybeCompact runs start-of-turn auto-compaction with a circuit breaker. It
// only acts when the context is over budget. After a compaction it records how
// much headroom remained; if a subsequent over-budget turn follows one that
// left too little headroom, it stops auto-compacting and emits a one-time note
// suggesting the user refocus or /clear, rather than spinning a summary call
// every turn for negligible gain.
func (s *Session) maybeCompact(ctx context.Context) {
	a := s.a
	before := llm.EstimateTokens(s.msgs)
	budget := a.maxContextTokens()
	if before <= budget {
		// Under budget: reset the breaker so it can act again later.
		s.compactStall = false
		return
	}
	if s.compactStall {
		// Already tripped and still over budget: don't keep summarizing.
		return
	}
	// If the previous compaction left too little headroom and we're over budget
	// again, trip the breaker instead of compacting once more — it can't help.
	if s.lastCompactAfter > 0 {
		headroom := budget - s.lastCompactAfter
		if float64(headroom) < compactStallHeadroomFrac*float64(budget) {
			s.compactStall = true
			a.emit(Event{Kind: EventNote, Text: "Context keeps refilling and compaction is no longer freeing much space. Consider /clear for a fresh thread or a more focused task."})
			return
		}
	}
	compacted, err := llm.CompactWith(ctx, a.compactor(), s.msgs, budget)
	if err != nil {
		return
	}
	s.msgs = compacted
	s.lastCompactBefore = before
	s.lastCompactAfter = llm.EstimateTokens(s.msgs)
}

// forceCompactOnOverflow shrinks the conversation after the provider rejected
// it as too large, regardless of MaxContextTokens (the model's hard window is
// smaller than we estimated, or no budget was configured). It aims at a
// fraction of the budget — or a conservative default when none is set — and
// reports whether the history actually got smaller (so the caller only retries
// when there is a real chance of success).
func (s *Session) forceCompactOnOverflow(ctx context.Context) bool {
	a := s.a
	before := len(s.msgs)
	if before <= 1 {
		return false // nothing left to fold; a single turn over the window can't be helped here
	}
	budget := a.maxContextTokens()
	if budget <= 0 {
		budget = 120_000
	}
	// Aim well under the budget so the retry has headroom; the provider just
	// told us our token estimate was optimistic, so also aim under HALF the
	// current estimated size, guaranteeing the history actually folds even when
	// our estimate already "fit" the configured budget.
	target := budget / 2
	if cur := llm.EstimateTokens(s.msgs); cur/2 < target {
		target = cur / 2
	}
	compacted, err := llm.CompactWith(ctx, a.compactor(), s.msgs, target)
	if err != nil {
		return false
	}
	if len(compacted) >= before && llm.EstimateTokens(compacted) >= llm.EstimateTokens(s.msgs) {
		return false // compaction couldn't shrink it; don't spin
	}
	s.msgs = compacted
	s.persist()
	return true
}

// Run executes a single task to completion (a one-shot Session.Send).
func (a *Agent) Run(ctx context.Context, task string) (string, error) {
	if a.provider() == nil {
		return "", fmt.Errorf("agent: nil provider")
	}
	if a.Tools == nil {
		return "", fmt.Errorf("agent: nil tools")
	}
	return a.NewSession().Send(ctx, task)
}

// Send appends a user message and drives the loop until the model produces a
// final answer, keeping the conversation in the Session. The loop is unbounded
// by default (it ends when the model stops calling tools); a positive
// a.MaxSteps imposes an optional runaway cap, and canceling ctx stops it.
func (s *Session) Send(ctx context.Context, task string) (string, error) {
	a := s.a
	if a.provider() == nil {
		return "", fmt.Errorf("agent: nil provider")
	}
	if a.Tools == nil {
		return "", fmt.Errorf("agent: nil tools")
	}
	s.msgs = append(s.msgs, llm.Message{Role: llm.RoleUser, Text: task})
	return s.drive(ctx)
}

// Resend re-drives the loop on the existing history without appending a new
// user message — used to retry a turn that failed mid-flight (e.g. after an
// overload failover switched the provider). The history already contains the
// user message (and any tool results that completed), so the loop simply
// continues from where it stopped.
func (s *Session) Resend(ctx context.Context) (string, error) {
	if len(s.msgs) == 0 {
		return "", fmt.Errorf("agent: nothing to resend")
	}
	return s.drive(ctx)
}

// drive runs the tool-use loop over the session's current history.
func (s *Session) drive(ctx context.Context) (string, error) {
	a := s.a
	if a.provider() == nil {
		return "", fmt.Errorf("agent: nil provider")
	}
	if a.Tools == nil {
		return "", fmt.Errorf("agent: nil tools")
	}
	if a.maxContextTokens() > 0 {
		s.maybeCompact(ctx)
	}
	s.persist()
	specs := a.Tools.Specs()
	emptyTurns := 0
	overflowRetried := false // guard: force-compact-and-retry at most once per step

	system := systemPrompt
	if a.ExtraSystem != "" {
		system += "\n\n" + a.ExtraSystem
	}
	if a.Memory != "" {
		system += "\n\n" + a.Memory
	}

	for step := 0; ; step++ {
		// Optional runaway guard: only when MaxSteps is explicitly positive.
		if a.MaxSteps > 0 && step >= a.MaxSteps {
			return "", fmt.Errorf("reached MaxSteps (%d) without a final answer", a.MaxSteps)
		}
		// Respect cancellation between steps (esc in the TUI cancels the turn).
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// The goal is read per step (live-settable via /goal) and appended
		// last so it stays the freshest instruction in the system prompt.
		sys := system
		if g := a.CurrentGoal(); g != "" {
			sys += "\n\nCURRENT GOAL (persistent; the user set this as the north star — keep every action aligned with it until it changes):\n" + g + "\nWhen you believe the goal is FULLY achieved, call the goal_achieved tool with concrete evidence; an independent judge verifies and clears it."
		}
		req := llm.Request{
			System:   sys,
			Messages: s.msgs,
			Tools:    specs,
		}
		var resp *llm.Response
		var err error
		prov := a.provider()
		if sm, ok := prov.(llm.Streamer); ok && a.OnEvent != nil {
			sink := func(c llm.StreamChunk) {
				kind := EventTextDelta
				if c.Kind == llm.ChunkReasoning {
					kind = EventReasoningDelta
				}
				a.emit(Event{Kind: kind, Step: step, Text: c.Text})
			}
			resp, err = sm.Stream(ctx, req, sink)
		} else {
			resp, err = prov.Complete(ctx, req)
		}
		if err != nil {
			// Error-driven compaction: if the provider rejected the request as
			// too large for its context window, shrink the conversation and
			// retry this step once. This is the context-size sibling of the
			// TUI's overload→failover path, and works for every provider.
			if llm.IsContextOverflow(err) && !overflowRetried {
				overflowRetried = true
				if s.forceCompactOnOverflow(ctx) {
					a.emit(Event{Kind: EventNote, Text: "Prompt exceeded the model's context window — compacted the conversation and retrying."})
					// Retry the same step with the shrunk history. Decrementing
					// the loop variable (re-incremented by the for post) keeps
					// MaxSteps semantics: a retry does not consume a step.
					step--
					continue
				}
			}
			return "", err
		}
		overflowRetried = false
		if len(resp.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Text) != "" {
				s.msgs = append(s.msgs, llm.Message{Role: llm.RoleAssistant, Text: resp.Text})
				s.persist()
				a.emit(Event{Kind: EventDone, Step: step, Text: resp.Text})
				return resp.Text, nil // final answer
			}
			// Empty turn (e.g. reasoning-only): nudge to act, bounded.
			emptyTurns++
			if emptyTurns > maxEmptyTurns {
				return "", fmt.Errorf("model returned no actionable output after %d empty turns", emptyTurns)
			}
			s.msgs = append(s.msgs, llm.Message{
				Role: llm.RoleUser,
				Text: "Continue: use a tool to make progress, or give your final answer.",
			})
			continue
		}
		emptyTurns = 0

		s.msgs = append(s.msgs, llm.Message{
			Role:        llm.RoleAssistant,
			Text:        resp.Text,
			Reasoning:   resp.Reasoning,
			ReasoningID: resp.ReasoningID,
			ToolCalls:   resp.ToolCalls,
		})
		// Tool calls are dispatched strictly in order, one at a time. This
		// in-order, non-concurrent execution is what makes write/edit (atomic
		// rename) and bash safe without per-path locking; add per-path mutexes
		// before ever parallelizing this loop.
		for _, tc := range resp.ToolCalls {
			a.emit(Event{Kind: EventToolStart, Step: step, ToolName: tc.Name, ToolID: tc.ID, ToolArgs: tc.Arguments})
			result, isErr := a.dispatch(ctx, tc)
			a.emit(Event{Kind: EventToolResult, Step: step, ToolName: tc.Name, ToolID: tc.ID, Result: result, IsError: isErr})
			s.msgs = append(s.msgs, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Text:       result,
				ToolError:  isErr,
			})
			// Dedupe: if this exact output already appears earlier (same tool,
			// e.g. an unchanged file re-read), stub the older copies — the
			// newest occurrence is the one the model will use.
			llm.DedupeToolResults(s.msgs, len(s.msgs)-1)
		}
		s.persist()
	}
}

// persist autosaves the conversation via the agent's Persist hook (called from
// the goroutine that owns the session, so reading s.msgs here never races).
func (s *Session) persist() {
	if s.a.Persist != nil {
		s.a.Persist(s.msgs)
	}
}

// emit delivers an event to the sink if one is set.
func (a *Agent) emit(e Event) {
	if a.OnEvent != nil {
		a.OnEvent(e)
	}
}

// dispatch runs one tool call, enforcing the permission posture, and returns the
// result (or an error string) to feed back to the model plus whether it failed.
func (a *Agent) dispatch(ctx context.Context, tc llm.ToolCall) (string, bool) {
	def, ok := a.Tools.Get(tc.Name)
	if !ok {
		return fmt.Sprintf("Error: unknown tool %q", tc.Name), true
	}
	if !def.ReadOnly {
		// Fail closed: a mutating tool runs only under an explicitly recognized
		// posture. Any unknown posture denies.
		switch a.perm() {
		case PermAuto:
			// allowed
		case PermGated:
			if a.Approve == nil {
				return fmt.Sprintf("Denied: tool %q was not approved by the user.", tc.Name), true
			}
			ok, err := a.Approve(ctx, tc.Name, tc.Arguments)
			if err != nil {
				return fmt.Sprintf("Denied: approval failed for %q: %v", tc.Name, err), true
			}
			if !ok {
				return fmt.Sprintf("Denied: tool %q was not approved by the user.", tc.Name), true
			}
		default:
			return fmt.Sprintf("Denied: tool %q blocked under unknown permission posture %q.", tc.Name, a.perm()), true
		}
	}
	out, err := a.runTool(ctx, def, tc.Arguments)
	if err != nil {
		return "Error: " + err.Error(), true
	}
	if len(out) > maxToolOutput {
		out = tool.TruncateUTF8(out, maxToolOutput) + "\n[output truncated]"
	}
	return out, false
}

// runTool executes a tool's Run, recovering any panic into an error so a buggy
// tool (including a plugin or MCP tool) becomes a recoverable tool failure
// rather than crashing the agent — in every entry path (TUI and headless).
func (a *Agent) runTool(ctx context.Context, def tool.Definition, args json.RawMessage) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("tool %q panicked: %v", def.Name, r)
		}
	}()
	return def.Run(ctx, args)
}

// Package agent implements eigen's tool-use loop: drive a provider, execute the
// tool calls it returns, feed results back, and repeat until the model is done.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
	"github.com/avifenesh/eigen/internal/tool"
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
- Background slow or independent work instead of blocking: task(background=true) returns immediately with an id and keeps running; poll/collect with task_status. Prefer it for anything that may run long or stall (a vision read of a large image, a long build/scan, web research) so a slow subtask never wedges your turn — a foreground subtask self-aborts after a timeout, but backgrounding is the right call when you don't need the result this instant.
- You are the orchestrator: when delegating, state the subtask's kind and difficulty so it routes to the best-fit model (trivial edits → fast cheap model; search/vision → a capable one). Keep only the work that needs you.
- To investigate or review SEVERAL things at once, use the task_group tool: it runs multiple READ-ONLY sub-agents in parallel (roles: researcher, reviewer, summarizer) and returns one combined report. Use it to fan out across files/angles; for changes that edit files, use the task tool one at a time.
- To make SEVERAL INDEPENDENT code changes at once, use task_group_mutating: each implementer works in an isolated copy of the repo and their diffs are merged back behind one approval (needs a git repo, session at the repo root, and a clean working tree). Keep each subtask's edits scoped so they don't overlap.

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
	EventApproval                        // a gated tool call awaits a user verdict (daemon mode)
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

	// InTokens/OutTokens: provider-reported usage summed over the turn
	// (EventDone only; zero when the provider reports none).
	InTokens  int
	OutTokens int
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

	// EventWrap, if set, wraps any sink installed as OnEvent by a session
	// host (observability logging, hooks). The daemon applies it when wiring
	// its dispatch so obs/hooks run daemon-side regardless of views.
	EventWrap func(EventSink) EventSink

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

	// Router, if set, picks the provider+model for a subtask given its prompt,
	// orchestrator-stated kind/difficulty, and whether it has images. It returns
	// a ready Provider + the chosen model id + a human label (for notes), or
	// nil to keep the current model. Injected by main (the agent package does
	// not build providers). Used by Subtask when auto-routing is enabled.
	Router func(ctx context.Context, prompt, kind, difficulty string, hasImage bool) (llm.Provider, string, string)

	// ModelProvider, if set, builds a Provider for an explicit model id/ref —
	// the orchestrator's per-delegation override ("run this on grok-4"), and
	// the escalation hook for background tasks. Injected by main.
	ModelProvider func(model string) (llm.Provider, error)

	// Bg, if set, enables background subtasks (task tool background:true):
	// detached delegations persisted under ~/.eigen/tasks. Injected by main.
	Bg *BgRegistry

	// SessionDir is the project root this agent is rooted at (the git repo for
	// mutating fan-out). Injected by main/buildSession.
	SessionDir string

	// WorktreeTools, if set, builds the implementer toolset rooted at a child
	// worktree dir for mutating parallel fan-out (read/search/write/edit/move —
	// NO bash/git/network). Injected by main (the agent package does not build
	// tool policies). nil disables mutating fan-out.
	WorktreeTools func(dir string) *tool.Registry

	// Policy is the filesystem sandbox the tools enforce. Held here so the
	// user-invoked /add-dir grant (AddDir) can extend it at runtime; the tools
	// capture this same *Policy, so an added root takes effect immediately.
	// nil when tools were built without a policy (rare). The agent NEVER calls
	// AddDir itself — only the user, via the command/flag.
	Policy *tool.Policy
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
	mu   sync.RWMutex // guards msgs — the daemon reads state() while drive() writes
	msgs []llm.Message

	// Circuit-breaker state for auto-compaction. compactStall is set when a
	// compaction left too little headroom under budget to be worth repeating;
	// while set, drive() skips auto-compaction (the user is nudged to /clear or
	// refocus) until the context drops back under budget on its own.
	lastCompactBefore int // estimated tokens before the last auto-compaction
	lastCompactAfter  int // estimated tokens after it
	compactStall      bool

	// steerMu guards steer: messages injected by the user (or orchestrator)
	// WHILE a turn is running. drive() drains them at the top of each step, so
	// a steer appears BETWEEN tool-call rounds (mid-turn), not deferred to the
	// next turn. This is the "steer, don't queue-to-end" contract.
	steerMu sync.Mutex
	steer   []llm.Message
}

// Steer injects a message into a RUNNING turn: it's appended to the
// conversation at the next step boundary (between tool-call rounds), so the
// model course-corrects mid-turn instead of at end-of-turn. Safe to call from
// another goroutine than the one driving the turn. No-op semantics if the turn
// ends before the next step — the message stays pending and a later turn drains
// it (callers that need end-of-turn delivery should append a user message
// instead).
func (s *Session) Steer(text string, images []llm.Image) {
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return
	}
	s.steerMu.Lock()
	s.steer = append(s.steer, llm.Message{Role: llm.RoleUser, Text: text, Images: images})
	s.steerMu.Unlock()
}

// drainSteer returns and clears any pending steer messages (drive calls it at
// each step boundary).
func (s *Session) drainSteer() []llm.Message {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	if len(s.steer) == 0 {
		return nil
	}
	out := s.steer
	s.steer = nil
	return out
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

// AddDir extends the tool sandbox with an additional allowed directory — the
// user-invoked /add-dir grant. Returns the normalized root that was added (the
// Policy guards its own concurrency). A no-op error when no policy is wired.
func (a *Agent) AddDir(path string) (string, error) {
	if a.Policy == nil {
		return "", fmt.Errorf("no sandbox policy on this session")
	}
	return a.Policy.AddRoot(path)
}

// Roots returns the tool sandbox's allowed directories (primary first), or nil.
func (a *Agent) Roots() []string {
	if a.Policy == nil {
		return nil
	}
	return a.Policy.Roots()
}

// CurrentPerm returns the permission posture (live-safe read).
func (a *Agent) CurrentPerm() Permission {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Perm
}

// CurrentMaxContextTokens returns the budget (live-safe read).
func (a *Agent) CurrentMaxContextTokens() int { return a.maxContextTokens() }

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

// CurrentProvider returns the live provider (race-safe read for callers like
// the daemon's state snapshot, which runs on a different goroutine than a
// /model SetLive swap).
func (a *Agent) CurrentProvider() llm.Provider {
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

// SubtaskOpts shapes one delegation: routing hints (kind/difficulty), an
// explicit Model override (the orchestrator names a specific model — beats
// routing entirely), and ModelProvider, the injected constructor that turns a
// model id into a ready Provider (set by main; the agent package does not
// build providers).
type SubtaskOpts struct {
	Kind       string
	Difficulty string
	Model      string // explicit model id/ref — overrides routing when set
	Role       string // named role (researcher/reviewer/summarizer) — read-only specialization
}

// Subtask runs task on a fresh session of (a copy of) this agent, with event
// emission suppressed so a delegated subtask does not clutter the caller's
// transcript — except a single EventNote reflecting WHERE the subtask ran
// (routed model or explicit override), so every delegation is auditable.
// Recursion is bounded so subtasks cannot spawn unboundedly.
func (a *Agent) Subtask(ctx context.Context, task, kind, difficulty string) (string, error) {
	return a.SubtaskWith(ctx, task, SubtaskOpts{Kind: kind, Difficulty: difficulty})
}

// SubtaskWith is Subtask with full options (explicit model override). It runs
// the subtask in the FOREGROUND for up to frontWindow; if the subtask is still
// active past that, it's PROMOTED to the background (the orchestrator gets a
// task id and continues, and can task_status / cancel it). A subtask that goes
// idle (no tool call for stallIdle) is cancelled as hung. The parent's ctx
// cancel (interrupt) still aborts it sooner.
func (a *Agent) SubtaskWith(ctx context.Context, task string, opts SubtaskOpts) (string, error) {
	depth, _ := ctx.Value(subtaskDepthKey{}).(int)
	if depth >= maxSubtaskDepth {
		return "", fmt.Errorf("subtask depth limit (%d) reached", maxSubtaskDepth)
	}
	sub, where := a.subAgent(ctx, task, opts)
	if where != "" {
		a.emit(Event{Kind: EventNote, Text: "task → " + where})
	}
	r := a.runChild(ctx, childRun{task: task, sub: sub, where: where, opts: opts, depth: depth})
	if r.promoted != "" {
		return "subtask still working past " + frontWindow.String() + " → moved to background as " + r.promoted +
			". Keep working on other things; check progress with task_status " + r.promoted +
			", and collect the result when its finish note arrives.", nil
	}
	return r.out, r.err
}

// childRun describes one foreground child to runChild.
type childRun struct {
	task  string
	sub   *Agent
	where string
	opts  SubtaskOpts
	depth int
}

// childResultFG is the outcome of a foreground child: a result, an error, or a
// promotion (the child outran the front window and is now a background task).
type childResultFG struct {
	out      string
	err      error
	promoted string // bg task id when promoted (out/err empty)
}

// childDone is a foreground child's terminal result, sent on the run channel.
type childDone struct {
	out string
	err error
}

// runChild runs a child subtask in the foreground with idle-stall detection and
// front-window→background promotion. Shared by Subtask and the group fan-out.
func (a *Agent) runChild(ctx context.Context, c childRun) childResultFG {
	// Snapshot the tunables once at run start so a later config change can't
	// race this run's watchdog.
	idle, front := stallIdle, frontWindow
	cctx := context.WithValue(ctx, subtaskDepthKey{}, c.depth+1)
	cctx, cancel := context.WithCancel(cctx)

	hb := newHeartbeat()
	// Install a settable relay ONCE so promotion can re-point the sinks without
	// racing the run goroutine (which reads OnEvent/Persist mid-drive). The
	// child's original OnEvent (e.g. group-report capture) chains through it.
	rl := &relay{onEvent: c.sub.OnEvent}
	c.sub.OnEvent = activitySink(hb, rl.emit)
	c.sub.Persist = rl.save
	stalled := watchStall(cctx, hb, cancel, idle, heartbeatGrace)

	ch := make(chan childDone, 1)
	sess := c.sub.NewSession()
	go func() {
		out, err := sess.Send(cctx, c.task)
		ch <- childDone{out, err}
	}()

	select {
	case d := <-ch:
		cancel()
		if d.err != nil && stalled() {
			return childResultFG{err: fmt.Errorf("subtask stalled (no tool activity for %s) and was stopped; try a smaller scope or background it", idle)}
		}
		if d.err != nil && cctx.Err() == context.Canceled && ctx.Err() != nil {
			return childResultFG{err: ctx.Err()} // parent interrupted
		}
		return childResultFG{out: d.out, err: d.err}
	case <-time.After(front):
		// Still working past the front window → promote to background. The
		// child keeps running on cctx (NOT cancelled); the bg registry adopts
		// the in-flight goroutine so the orchestrator can task_status/cancel.
		id := a.promoteRunning(cctx, cancel, c, rl, ch, stalled, idle, front)
		if id == "" {
			// No bg registry: fall back to blocking (idle-stall still applies).
			d := <-ch
			cancel()
			if d.err != nil && stalled() {
				return childResultFG{err: fmt.Errorf("subtask stalled (no tool activity for %s) and was stopped", idle)}
			}
			return childResultFG{out: d.out, err: d.err}
		}
		return childResultFG{promoted: id}
	}
}

// subAgent constructs the sub-agent for one delegation and reports where it
// runs ("" = inherited the caller's model, nothing noteworthy). Order of
// precedence: explicit opts.Model (the orchestrator named a model — beats
// routing), then the Router's choice, then the caller's live provider.
func (a *Agent) subAgent(ctx context.Context, task string, opts SubtaskOpts) (*Agent, string) {
	prov := a.provider()
	compactor := a.compactor()
	where := ""

	// Role: a named read-only specialization (Tier 16). Filter the toolset to
	// the role's allowlist, prepend its system framing, and default the routing
	// difficulty. An unknown role is ignored (the caller — task_group — fails
	// closed before reaching here; a bare Subtask just runs unspecialized).
	tools := a.Tools
	extraSystem := a.ExtraSystem
	if opts.Role != "" {
		if role, ok := LookupRole(opts.Role); ok {
			tools = a.Tools.Subset(role.Tools...)
			if role.System != "" {
				if extraSystem != "" {
					extraSystem = role.System + "\n\n" + extraSystem
				} else {
					extraSystem = role.System
				}
			}
			if opts.Difficulty == "" {
				opts.Difficulty = role.Difficulty
			}
			where = "role " + role.Name
		}
	}

	switch {
	case opts.Model != "" && a.ModelProvider != nil:
		p, err := a.ModelProvider(opts.Model)
		if err != nil {
			// The named model can't be built (bad id, missing creds): fall
			// back to routing/inherit, but SAY so — silent fallback would
			// defeat the override's purpose.
			where = joinWhere(where, "explicit model "+opts.Model+" unavailable ("+err.Error()+") — falling back")
		} else {
			prov = p
			compactor = llm.NewCompactor(p)
			where = joinWhere(where, "running on "+opts.Model+" (explicit)")
		}
	}
	if prov == a.provider() && a.Router != nil { // no explicit override took effect
		if rp, _, label := a.Router(ctx, task, opts.Kind, opts.Difficulty, false); rp != nil {
			prov = rp
			compactor = llm.NewCompactor(rp)
			where = joinWhere(where, label)
		}
	}
	// Construct the sub-agent explicitly (not by struct copy: Agent embeds a
	// mutex). It inherits the perm/budget snapshot but stays silent (no
	// OnEvent) and does not persist (its session is ephemeral). The Router and
	// ModelProvider are carried so nested subtasks route/override too.
	sub := &Agent{
		Provider:         prov,
		Tools:            tools,
		Perm:             a.perm(),
		MaxSteps:         a.MaxSteps,
		Approve:          a.Approve,
		MaxContextTokens: a.maxContextTokens(),
		Compactor:        compactor,
		ExtraSystem:      extraSystem,
		Memory:           a.Memory,
		Router:           a.Router,
		ModelProvider:    a.ModelProvider,
		Bg:               a.Bg,
		SessionDir:       a.SessionDir,
		WorktreeTools:    a.WorktreeTools,
	}
	return sub, where
}

// joinWhere combines two non-empty "where ran" fragments with "; ".
func joinWhere(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "; " + b
}

// Messages returns a COPY of the conversation so far (for saving / live-replace
// handoff). A copy — not the live slice — because the daemon serves state
// snapshots on a different goroutine than the running turn; returning the live
// backing array would race with drive()'s appends.
func (s *Session) Messages() []llm.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]llm.Message, len(s.msgs))
	copy(out, s.msgs)
	return out
}

// appendMsg appends under the write lock (drive loop / SendWith).
func (s *Session) appendMsg(m ...llm.Message) {
	s.mu.Lock()
	s.msgs = append(s.msgs, m...)
	s.mu.Unlock()
}

// setMsgs replaces history under the write lock (compaction).
func (s *Session) setMsgs(m []llm.Message) {
	s.mu.Lock()
	s.msgs = m
	s.mu.Unlock()
}

// snapshot returns a copy of msgs under the read lock (for requests that send
// the history to a provider mid-build).
func (s *Session) snapshot() []llm.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]llm.Message, len(s.msgs))
	copy(out, s.msgs)
	return out
}

// msgLen returns the message count under the read lock.
func (s *Session) msgLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.msgs)
}

// Compact summarizes the conversation now, on demand (e.g. the /compact
// command), regardless of the token budget. It replaces older history with a
// model-generated summary, keeping recent turns verbatim. targetTokens bounds
// the result; when <= 0 it uses the agent's MaxContextTokens, or a conservative
// default. Returns the message counts before/after so the caller can report it.
func (s *Session) Compact(ctx context.Context, targetTokens int) (before, after int, err error) {
	a := s.a
	msgs := s.snapshot()
	before = len(msgs)
	if before == 0 {
		return 0, 0, nil
	}
	var target int
	if targetTokens > 0 {
		// Explicit target (e.g. manual /compact): use it directly.
		target = targetTokens
	} else {
		// Auto: aim at a fraction of the budget so compaction meaningfully
		// shrinks the context even when under the limit.
		budget := a.maxContextTokens()
		if budget <= 0 {
			budget = 120_000
		}
		target = budget / 2
	}
	compacted, cerr := llm.CompactWith(ctx, a.compactor(), msgs, target)
	if cerr != nil {
		return before, before, cerr
	}
	s.setMsgs(compacted)
	s.persist()
	return before, len(compacted), nil
}

// Tokens returns the current estimated context size of the conversation.
func (s *Session) Tokens() int { return llm.EstimateTokens(s.snapshot()) }

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
	msgs := s.snapshot()
	before := llm.EstimateTokens(msgs)
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
	compacted, err := llm.CompactWith(ctx, a.compactor(), msgs, budget)
	if err != nil {
		return
	}
	s.setMsgs(compacted)
	s.lastCompactBefore = before
	s.lastCompactAfter = llm.EstimateTokens(compacted)
}

// forceCompactOnOverflow shrinks the conversation after the provider rejected
// it as too large, regardless of MaxContextTokens (the model's hard window is
// smaller than we estimated, or no budget was configured). It aims at a
// fraction of the budget — or a conservative default when none is set — and
// reports whether the history actually got smaller (so the caller only retries
// when there is a real chance of success).
func (s *Session) forceCompactOnOverflow(ctx context.Context) bool {
	a := s.a
	msgs := s.snapshot()
	before := len(msgs)
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
	curTokens := llm.EstimateTokens(msgs)
	if curTokens/2 < target {
		target = curTokens / 2
	}
	compacted, err := llm.CompactWith(ctx, a.compactor(), msgs, target)
	if err != nil {
		return false
	}
	if len(compacted) >= before && llm.EstimateTokens(compacted) >= curTokens {
		return false // compaction couldn't shrink it; don't spin
	}
	s.setMsgs(compacted)
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
	return s.SendWith(ctx, task, nil)
}

// SendWith is Send with attached images (vision models). Images are dropped
// from the message when the active model has no vision support, so a non-vision
// model never receives a block it can't handle.
func (s *Session) SendWith(ctx context.Context, task string, images []llm.Image) (string, error) {
	a := s.a
	if a.provider() == nil {
		return "", fmt.Errorf("agent: nil provider")
	}
	if a.Tools == nil {
		return "", fmt.Errorf("agent: nil tools")
	}
	s.appendMsg(llm.Message{Role: llm.RoleUser, Text: task, Images: images})
	return s.drive(ctx)
}

// Resend re-drives the loop on the existing history without appending a new
// user message — used to retry a turn that failed mid-flight (e.g. after an
// overload failover switched the provider). The history already contains the
// user message (and any tool results that completed), so the loop simply
// continues from where it stopped.
func (s *Session) Resend(ctx context.Context) (string, error) {
	if s.msgLen() == 0 {
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
	var usedIn, usedOut int  // provider-reported usage, summed over the turn

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
		// Steer: drain any messages the user/orchestrator injected mid-turn and
		// append them now, so they land BETWEEN tool-call rounds — the model
		// course-corrects on the next request instead of at end-of-turn.
		if steers := s.drainSteer(); len(steers) > 0 {
			for _, m := range steers {
				s.appendMsg(m)
				a.emit(Event{Kind: EventNote, Step: step, Text: "↪ steer: " + truncateForNote(m.Text)})
			}
			s.persist()
		}
		// The goal is read per step (live-settable via /goal) and appended
		// last so it stays the freshest instruction in the system prompt.
		sys := system
		if g := a.CurrentGoal(); g != "" {
			sys += "\n\nCURRENT GOAL (persistent; the user set this as the north star — keep every action aligned with it until it changes):\n" + g + "\nWhen you believe the goal is FULLY achieved, call the goal_achieved tool with concrete evidence; an independent judge verifies and clears it."
		}
		req := llm.Request{
			System: sys,
			// Bound screenshot bytes on the wire: keep only the most recent
			// tool-result images, drop older ones (a browser/computer-use
			// session would otherwise resend every screenshot every turn).
			Messages: llm.ShedToolImages(s.snapshot()),
			Tools:    specs,
		}
		var resp *llm.Response
		var err error
		streamed := false
		prov := a.provider()
		if sm, ok := prov.(llm.Streamer); ok && a.OnEvent != nil {
			streamed = true
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
		usedIn += resp.Usage.InputTokens
		usedOut += resp.Usage.OutputTokens
		if len(resp.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Text) != "" {
				s.appendMsg(llm.Message{Role: llm.RoleAssistant, Text: resp.Text})
				s.persist()
				a.emit(Event{Kind: EventDone, Step: step, Text: resp.Text, InTokens: usedIn, OutTokens: usedOut})
				return resp.Text, nil // final answer
			}
			// Empty turn (e.g. reasoning-only): nudge to act, bounded.
			emptyTurns++
			if emptyTurns > maxEmptyTurns {
				return "", fmt.Errorf("model returned no actionable output after %d empty turns", emptyTurns)
			}
			s.appendMsg(llm.Message{
				Role: llm.RoleUser,
				Text: "Continue: use a tool to make progress, or give your final answer.",
			})
			continue
		}
		emptyTurns = 0

		s.appendMsg(llm.Message{
			Role:        llm.RoleAssistant,
			Text:        resp.Text,
			Reasoning:   resp.Reasoning,
			ReasoningID: resp.ReasoningID,
			ToolCalls:   resp.ToolCalls,
		})
		// Non-streaming providers deliver the in-between commentary (reasoning
		// + text accompanying tool calls) only in the final response — emit it
		// now so the live view matches what resume renders from history.
		// Streaming providers already delivered these as deltas.
		if !streamed {
			if resp.Reasoning != "" {
				a.emit(Event{Kind: EventReasoningDelta, Step: step, Text: resp.Reasoning})
			}
			if strings.TrimSpace(resp.Text) != "" {
				a.emit(Event{Kind: EventTextDelta, Step: step, Text: resp.Text})
			}
		}
		// Tool calls are dispatched strictly in order, one at a time. This
		// in-order, non-concurrent execution is what makes write/edit (atomic
		// rename) and bash safe without per-path locking; add per-path mutexes
		// before ever parallelizing this loop.
		for _, tc := range resp.ToolCalls {
			a.emit(Event{Kind: EventToolStart, Step: step, ToolName: tc.Name, ToolID: tc.ID, ToolArgs: tc.Arguments})
			result, isErr := a.dispatch(ctx, tc)
			a.emit(Event{Kind: EventToolResult, Step: step, ToolName: tc.Name, ToolID: tc.ID, Result: result.Text, IsError: isErr})
			// Append the tool result and dedupe older identical outputs in one
			// locked step (DedupeToolResults mutates earlier entries in place).
			// Images (screenshots from browser / computer-use) ride on the
			// RoleTool message; each provider serializes them per capability.
			s.appendToolResult(llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: tc.ID,
				ToolName:   tc.Name,
				Text:       result.Text,
				Images:     result.Images,
				ToolError:  isErr,
			})
		}
		s.persist()
	}
}

// appendToolResult appends a tool message then dedupes older identical outputs,
// all under the write lock (DedupeToolResults rewrites earlier entries — the
// daemon must never read mid-rewrite).
func (s *Session) appendToolResult(m llm.Message) {
	s.mu.Lock()
	s.msgs = append(s.msgs, m)
	// Dedupe: if this exact output already appears earlier (same tool, e.g. an
	// unchanged file re-read), stub the older copies — the newest occurrence is
	// the one the model will use.
	llm.DedupeToolResults(s.msgs, len(s.msgs)-1)
	// Bound stored screenshot bytes too (not just the wire request): drop
	// images from all but the most recent few tool results so a long
	// browser/computer-use session can't grow daemon memory without limit.
	if len(m.Images) > 0 {
		s.msgs = llm.ShedToolImages(s.msgs)
	}
	s.mu.Unlock()
}

// persist autosaves the conversation via the agent's Persist hook. It snapshots
// under the read lock so a concurrent state() read never sees a half-written
// slice.
func (s *Session) persist() {
	if s.a.Persist != nil {
		s.a.Persist(s.snapshot())
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
func (a *Agent) dispatch(ctx context.Context, tc llm.ToolCall) (tool.Result, bool) {
	errResult := func(s string) (tool.Result, bool) { return tool.Result{Text: s}, true }
	def, ok := a.Tools.Get(tc.Name)
	if !ok {
		return errResult(fmt.Sprintf("Error: unknown tool %q", tc.Name))
	}
	if !def.ReadOnly {
		// Fail closed: a mutating tool runs only under an explicitly recognized
		// posture. Any unknown posture denies.
		switch a.perm() {
		case PermAuto:
			// allowed
		case PermGated:
			if a.Approve == nil {
				return errResult(fmt.Sprintf("Denied: tool %q was not approved by the user.", tc.Name))
			}
			ok, err := a.Approve(ctx, tc.Name, tc.Arguments)
			if err != nil {
				return errResult(fmt.Sprintf("Denied: approval failed for %q: %v", tc.Name, err))
			}
			if !ok {
				return errResult(fmt.Sprintf("Denied: tool %q was not approved by the user.", tc.Name))
			}
		default:
			return errResult(fmt.Sprintf("Denied: tool %q blocked under unknown permission posture %q.", tc.Name, a.perm()))
		}
	}
	res, err := a.runTool(ctx, def, tc.Arguments)
	if err != nil {
		return errResult("Error: " + err.Error())
	}
	if len(res.Text) > maxToolOutput {
		res.Text = tool.TruncateUTF8(res.Text, maxToolOutput) + "\n[output truncated]"
	}
	return res, false
}

// runTool executes a tool's Run/RunRich, recovering any panic into an error so
// a buggy tool (including a plugin or MCP tool) becomes a recoverable tool
// failure rather than crashing the agent — in every entry path (TUI and
// headless).
func (a *Agent) runTool(ctx context.Context, def tool.Definition, args json.RawMessage) (out tool.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = tool.Result{}, fmt.Errorf("tool %q panicked: %v", def.Name, r)
		}
	}()
	return def.Invoke(ctx, args)
}

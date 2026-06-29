// Package agent implements eigen's tool-use loop: drive a provider, execute the
// tool calls it returns, feed results back, and repeat until the model is done.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
- Make focused edits with edit or multiedit (read first, exact old_string); use apply_patch only for multi-file or large diffs after reading current file content; create files with write.
- After changing code, use diff to review your changes before reporting.
- When a task matches an available skill (listed below), load it with the skill tool first.
- Record durable, project-specific facts (build/test commands, conventions, gotchas) with the memory tool.
- Use subagents proactively for self-contained work: independent investigation, cross-checks, long scans, and review should usually go to task/task_group instead of being done serially in the main turn.
- For a large, separable chunk of work, delegate it with the task tool (a fresh, isolated subtask).
- Background slow or independent work instead of blocking: task(background=true) returns immediately with an id and keeps running; poll/collect with task_status. Prefer it for anything that may run long or stall (a vision read of a large image, a long build/scan, web research) so a slow subtask never wedges your turn — a foreground subtask self-aborts after a timeout, but backgrounding is the right call when you don't need the result this instant.
- You are the orchestrator: when delegating, state the subtask's kind and difficulty so it routes to the best-fit model (trivial edits → fast cheap model; search/vision → a capable one). Keep only the work that needs you.
- To investigate or review SEVERAL things at once, use the task_group tool: it runs multiple READ-ONLY sub-agents in parallel (roles: researcher, reviewer, summarizer, plus installed plugin-agent roles marked read-only) and returns one combined report. Use it to fan out across files/angles; for changes that edit files, use the task tool one at a time.
- To make SEVERAL INDEPENDENT code changes at once, use task_group_mutating: each implementer works in an isolated copy of the repo and their diffs are merged back behind one approval (needs a git repo, session at the repo root, and a clean working tree). Keep each subtask's edits scoped so they don't overlap.

Tools worth reaching for (beyond read/edit/grep):
- bash runs shell commands. The default timeout is **30 seconds** (max 600). For a command you expect to take longer than ~30s but that you still want to wait on (a build, a test run, an install, a long grep), set timeout_seconds high enough (e.g. 180) so it isn't killed mid-run. For anything genuinely long-lived or open-ended — a dev server, a file watcher, an interactive process — use background=true instead: it returns a shell id immediately and keeps running so you DON'T block the turn. Poll it with bash_output (incremental: shows new output + status), and stop it with kill_shell. Running shells are listed back to you each step, so start them freely and check in when it matters.
- orientation is built into Eigen's harness for history/provenance on unfamiliar code. When judging or planning around code you did not write in this session, run commands like: eigen orientation provenance "$PWD" <file>; eigen orientation related "$PWD" <file>. Do this with bash before calling code dead/stale or adding near it; this replaces the old get-oriented skill.
- retrieve does SEMANTIC search over the project (find code by meaning, not just text). Use it for "where/how is X handled" when you don't know the exact string; use grep for a known literal.
- websearch finds current/external information (it works out of the box, no key needed); fetch GETs a specific URL. Reach for them when the answer depends on something outside the codebase (a library's current API, an error message, docs).
- generate_image creates images from a text prompt (diagrams, mockups, assets) saved into the project.
- review gets an independent cross-vendor critique of a plan/diff before you commit to it.

You have a CORE set of tools always available (above). Many more — browser & desktop automation, project-specific integrations, language-server queries — are not loaded by default to save context; they're listed as capability groups under "MORE TOOLS" when present. When a task needs a capability the core tools don't cover, call search_tools with the group name to see capability categories (for example accessibility/windows/screen/input), then search_tools with a category or specific keyword/tool name to open only the needed schema(s). Don't assume a capability is missing without searching.

Call tools as needed; when the task is complete, reply with a short, specific summary of what you did.`

const GoalStartInstruction = "A goal was just set (see CURRENT GOAL in your instructions). Start working toward it now: assess the current state, plan briefly, then take the first concrete actions. When it is fully achieved, call goal_achieved with evidence."

const GoalContinueInstruction = "The CURRENT GOAL is still active and not judge-confirmed yet. Do not idle. Continue working toward it now: assess what remains, take the next concrete actions, and only stop once you can call goal_achieved with concrete evidence and the judge confirms it."

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
	EventBgDone                          // a background task this session spawned finished (Result=id, Text=note); wakes an idle orchestrator
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
	Provider string          // EventDone: provider that produced the final answer
	Model    string          // EventDone: model id that produced the final answer

	// InTokens/OutTokens: provider-reported usage summed over the turn
	// (EventDone only; zero when the provider reports none).
	InTokens  int
	OutTokens int
	// CacheReadTokens/CacheWriteTokens: prompt-cache hits/writes summed over the
	// turn (EventDone only). CacheReadTokens vs InTokens is the cache hit rate —
	// the headline token-efficiency signal for an always-on agent.
	CacheReadTokens  int
	CacheWriteTokens int
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

	// onModelCall, if set, is invoked at the START of every model call — the
	// subtask stall watchdog's heartbeat hook. It bypasses the user-facing
	// event stream entirely (a long non-streaming Complete() emits no events
	// but must not look "idle"). Set by runChild for subtasks; nil for the
	// top-level agent, which isn't stall-watched.
	onModelCall func()

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

	// JudgeModel, if set (config judge_model / EIGEN_JUDGE_MODEL), pins the model
	// the goal/claim judge uses — beats the type-ladder default. Injected by main.
	JudgeModel string

	// ChainProvider, if set, builds the per-ROLE fallback-chain provider for a
	// role ("explore"/"research"/"general"/"code"/"judge"/"dreamer"/"primary").
	// Injected by main from config RuleChains. When present it REPLACES the
	// built-in type ladders: a subagent of a given type runs that role's chain
	// (first reachable model, falling through on quota), so model selection is
	// fully user-configurable. Returns nil when the role has no usable chain.
	ChainProvider func(role string) llm.Provider

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

	// Shells is the per-session registry of backgrounded bash commands (the
	// bash background=true / detach feature). Held here so the on-demand detach
	// signal + a shells panel can reach it. nil = backgrounding disabled.
	Shells *tool.ShellRegistry

	// detachBash, when non-nil, returns the detach channel for the CURRENT
	// foreground bash call: a receive means "background this running command".
	// Set by the bash tool's detach func; signaled by DetachBash (the user's
	// background-the-running-command key). Guarded by mu.
	detachBash chan struct{}

	// unlockedTools is the set of niche tool names revealed via search_tools
	// (progressive disclosure). Guarded by mu. Sticky for the session: once the
	// model discovers a tool it stays callable (discovery is cumulative).
	unlockedTools map[string]bool
}

// maxToolOutput caps a single tool result fed back to the model, so a runaway
// tool (huge file, verbose command) can't blow up memory or the next request.
const maxToolOutput = 100_000

// maxEmptyTurns bounds how many times we nudge the model after it returns a
// turn with neither tool calls nor text (e.g. a reasoning-only response),
// preventing both a premature empty exit and an infinite spin.
const maxEmptyTurns = 2

// maxReasoningOnlyTurns bounds consecutive turns that produce ONLY reasoning
// (no text, no tool call) before we give up. Codex/gpt-5.x legitimately think
// across several Responses turns before acting, so this is generous — it exists
// only so a model that never acts can't loop forever.
const maxReasoningOnlyTurns = 20

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

	// allowTools, when non-empty, restricts the turn to this set of tool names
	// (a slash command's `allowed-tools` frontmatter). It is consumed at the
	// START of drive() and cleared at the end, so it scopes exactly one turn:
	// only these tools are offered to the model and only these may dispatch.
	// Guarded by allowMu (set from the UI goroutine before Send).
	allowMu    sync.Mutex
	allowTools []string
}

// SetTurnTools restricts the NEXT turn to the given tool names (a slash
// command's `allowed-tools`). Empty/nil clears the restriction. The set is
// consumed and cleared by drive() so it never leaks into a later turn.
func (s *Session) SetTurnTools(names []string) {
	s.allowMu.Lock()
	if len(names) == 0 {
		s.allowTools = nil
	} else {
		s.allowTools = append([]string(nil), names...)
	}
	s.allowMu.Unlock()
}

// turnTools returns the active per-turn allowlist (a copy), or nil.
func (s *Session) turnTools() []string {
	s.allowMu.Lock()
	defer s.allowMu.Unlock()
	if len(s.allowTools) == 0 {
		return nil
	}
	return append([]string(nil), s.allowTools...)
}

// clearTurnTools drops the per-turn allowlist (called at end of drive()).
func (s *Session) clearTurnTools() {
	s.allowMu.Lock()
	s.allowTools = nil
	s.allowMu.Unlock()
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

// hasSteer reports whether a steer is pending (peek without draining). Used at
// the final-answer boundary: a steer that landed during the last model call
// must be consumed by THIS turn (loop once more) rather than stranded until a
// future turn.
func (s *Session) hasSteer() bool {
	s.steerMu.Lock()
	defer s.steerMu.Unlock()
	return len(s.steer) > 0
}

// FlushSteer appends any pending steer messages to the conversation and
// persists them. It is used by the daemon during shutdown: a user's mid-turn
// follow-up may still be sitting in the steer queue (not yet consumed at a step
// boundary), and dropping it on restart would lose user input. Safe if the
// running turn races with shutdown: drainSteer serializes on steerMu, so either
// the turn consumes the steer or this method makes it durable.
func (s *Session) FlushSteer() bool {
	steers := s.drainSteer()
	if len(steers) == 0 {
		return false
	}
	s.appendMsg(steers...)
	s.persist()
	return true
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

// BashDetachCh hands the bash tool a fresh detach channel for one command and
// stores it so DetachBash can signal it. There is at most one foreground bash
// per session turn, so a single slot suffices; it's replaced when the next bash
// starts. UNBUFFERED: a DetachBash send succeeds only while runBash is actively
// selecting on it (a real running command) — between bash calls the send finds
// no receiver and DetachBash correctly reports "nothing to background". Returns
// nil when backgrounding isn't wired.
func (a *Agent) BashDetachCh() <-chan struct{} {
	if a.Shells == nil {
		return nil
	}
	ch := make(chan struct{})
	a.mu.Lock()
	a.detachBash = ch
	a.mu.Unlock()
	return ch
}

// DetachBash signals the currently-running foreground bash command to move to
// the background (the user's "background this running command" key). No-op when
// no bash is running. Returns true if a command was signaled.
func (a *Agent) DetachBash() bool {
	a.mu.Lock()
	ch := a.detachBash
	a.detachBash = nil
	a.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- struct{}{}:
		return true
	default:
		return false
	}
}

// Shelled reports whether this agent has a backgrounded-shell registry.
func (a *Agent) Shelled() bool { return a.Shells != nil }

// UnlockTools reveals niche tools (by name) for the rest of the turn — the
// search_tools meta-tool calls this so the discovered tools become callable.
func (a *Agent) UnlockTools(names []string) {
	a.mu.Lock()
	if a.unlockedTools == nil {
		a.unlockedTools = map[string]bool{}
	}
	for _, n := range names {
		a.unlockedTools[n] = true
	}
	a.mu.Unlock()
}

// unlockedSet returns a copy of the currently-unlocked niche tool names.
func (a *Agent) unlockedSet() map[string]bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.unlockedTools) == 0 {
		return nil
	}
	out := make(map[string]bool, len(a.unlockedTools))
	for k := range a.unlockedTools {
		out[k] = true
	}
	return out
}

// backgroundShellStatus renders a concise per-step block listing the agent's
// backgrounded shells, so it stays AWARE of them across steps. Empty when there
// are none. Injected into the system prompt each step, like the goal.
func (a *Agent) backgroundShellStatus() string {
	if a.Shells == nil {
		return ""
	}
	return a.Shells.StatusBlock()
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
	Role       string // named role (built-in or installed plugin agent)
	// Type is the subagent's PURPOSE (explore/research/general/code/judge) — it
	// drives a capability-aware effort + model policy (llm.SubagentEffort /
	// SubagentModel) distinct from Kind/Difficulty. Empty → derived from Role,
	// else "general". Effort, when set, is an explicit override that beats the
	// policy (the orchestrator pins a level for this one delegation).
	Type   string
	Effort string
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
	out      string
	err      error
	messages []llm.Message
}

// runChild runs a child subtask in the foreground with idle-stall detection and
// front-window→background promotion. Shared by Subtask and the group fan-out.
func (a *Agent) runChild(ctx context.Context, c childRun) childResultFG {
	// Snapshot the tunables once at run start so a later config change can't
	// race this run's watchdog.
	idle, modelWait, front := stallIdle, modelMaxWait, frontWindow
	// The child runs on a context DETACHED from the parent turn — rooted at
	// Background (not a descendant of ctx), depth-tagged, and capped at
	// bgMaxRuntime. This detachment is what lets a PROMOTED child outlive the
	// turn that spawned it: if cctx were derived from ctx, the parent turn
	// ending (or a parent group's `defer cancel()` deadline) would cancel the
	// just-"backgrounded" child the instant the tool call returned — which is
	// exactly the "context canceled" bug promotion is supposed to avoid. A
	// parent interrupt is still honored WHILE the child is in the foreground
	// window, via the explicit ctx.Done() case below; once promoted, the parent
	// can no longer kill it.
	base := context.WithValue(context.Background(), subtaskDepthKey{}, c.depth+1)
	cctx, cancel := context.WithTimeout(base, bgMaxRuntime)

	hb := newHeartbeat()
	// Install a settable relay ONCE so promotion can re-point the sinks without
	// racing the run goroutine (which reads OnEvent/Persist mid-drive). The
	// child's original OnEvent (e.g. group-report capture) chains through it.
	rl := &relay{onEvent: c.sub.OnEvent}
	c.sub.OnEvent = activitySink(hb, rl.emit)
	c.sub.Persist = rl.save
	// Heartbeat hook: a model call in flight switches the watchdog to its larger
	// budget so a slow non-streaming Complete() isn't mistaken for a hang.
	c.sub.onModelCall = hb.modelStart
	stalled := watchStall(cctx, hb, cancel, idle, modelWait, heartbeatGrace)

	ch := make(chan childDone, 1)
	sess := c.sub.NewSession()
	go func() {
		out, err := sess.Send(cctx, c.task)
		ch <- childDone{out: out, err: err, messages: sess.snapshot()}
	}()

	select {
	case d := <-ch:
		cancel()
		if d.err != nil && stalled() {
			return childResultFG{err: fmt.Errorf("subtask stalled (no tool activity for %s) and was stopped; try a smaller scope or background it", idle)}
		}
		return childResultFG{out: d.out, err: d.err}
	case <-ctx.Done():
		// Parent turn interrupted/canceled before the front window elapsed.
		// cctx is detached from ctx, so cancel() here is what actually stops the
		// child; then drain ch so the goroutine doesn't outlive us.
		cancel()
		<-ch
		return childResultFG{err: ctx.Err()}
	case <-time.After(front):
		// Still working past the front window → promote to background. cctx is
		// already detached from the parent, so the child keeps running after
		// this turn returns; the bg registry adopts the in-flight goroutine so
		// the orchestrator can task_status/cancel it.
		id := a.promoteRunning(cctx, cancel, c, rl, ch, stalled, idle, front)
		if id == "" {
			// No bg registry: fall back to blocking. Idle-stall still applies,
			// and a parent interrupt still aborts the (detached) child.
			select {
			case d := <-ch:
				cancel()
				if d.err != nil && stalled() {
					return childResultFG{err: fmt.Errorf("subtask stalled (no tool activity for %s) and was stopped", idle)}
				}
				return childResultFG{out: d.out, err: d.err}
			case <-ctx.Done():
				cancel()
				<-ch
				return childResultFG{err: ctx.Err()}
			}
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
			if !role.InheritTools {
				tools = a.Tools.Subset(role.Tools...)
			}
			if role.System != "" {
				if extraSystem != "" {
					extraSystem = role.System + "\n\n" + extraSystem
				} else {
					extraSystem = role.System
				}
			}
			if opts.Kind == "" {
				opts.Kind = role.Kind
			}
			if opts.Difficulty == "" {
				opts.Difficulty = role.Difficulty
			}
			if opts.Model == "" {
				opts.Model = role.Model
			}
			if opts.Type == "" {
				opts.Type = role.Type
			}
			if opts.Effort == "" {
				opts.Effort = role.Effort
			}
			where = "role " + role.Name
		}
	}

	// Subagent TYPE drives a capability-aware policy: when the caller didn't name
	// an explicit Model, pick the type's preferred credentialed model (capability
	// != price — e.g. research prefers glm-5.2, judge prefers gpt, explore a fast
	// cheap model), skipping suspended providers. Explicit Model still wins.
	stype := llm.NormalizeSubagentType(firstNonEmpty(opts.Type, opts.Kind))
	// Per-role CHAIN takes precedence: the user-configured chain for this
	// subagent type IS the provider (first reachable model, falling through on
	// quota), replacing the built-in ladder + manual fallback wrap. Only when no
	// explicit Model override and the policy isn't disabled.
	if opts.Model == "" && !subtaskPolicyDisabled() && a.ChainProvider != nil {
		if ch := a.ChainProvider(string(stype)); ch != nil {
			prov = ch
			compactor = llm.NewCompactor(ch)
			where = joinWhere(where, "type "+string(stype)+" chain (start "+ch.ModelID()+")")
		}
	}
	// Legacy ladder fallback (no chain configured): pick the type's preferred
	// credentialed model, wrapped in quota failover to the parent below.
	if opts.Model == "" && prov == a.provider() && !subtaskPolicyDisabled() && a.ChainProvider == nil {
		if m := llm.SubagentModel(stype); m != "" {
			opts.Model = m
			where = joinWhere(where, "type "+string(stype))
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
			// Wrap the picked model in quota failover to the parent's provider:
			// a TYPE-policy pick (e.g. research→glm-5.2) whose account is drained
			// 429s, and unlike the main loop the subagent had no safety net — so
			// a quota error here would just fail the subtask. NewFallback freezes
			// the drained provider (process-wide) + routes to the parent's
			// credentialed model. Only when the parent is a DIFFERENT model (no
			// point wrapping a provider in itself).
			parent := a.provider()
			if parent != nil && parent.ModelID() != p.ModelID() {
				prov = llm.NewFallback(p, parent)
			} else {
				prov = p
			}
			compactor = llm.NewCompactor(p)
			where = joinWhere(where, "running on "+opts.Model+" (explicit)")
		}
	}
	if prov == a.provider() && a.Router != nil { // no explicit override took effect
		if rp, _, label := a.Router(ctx, task, opts.Kind, opts.Difficulty, false); rp != nil {
			prov = rp
			compactor = llm.NewCompactor(rp)
			where = joinWhere(where, label)
		} else if label != "" {
			where = joinWhere(where, label)
		}
	}
	// Effort/fast discipline mutates the provider in place (SetEffort/SetFast).
	// Every provider reachable here is SHARED — the parent's live provider
	// (a.provider()) or a router-cache instance reused across sessions — so
	// mutating it would bleed a subtask's lowered effort / fast path into the
	// parent and other sessions. Before disciplining, give the subtask a
	// provider it EXCLUSIVELY owns (a fresh build of the same model). If we
	// can't own one, skip discipline rather than corrupt the shared instance.
	if !subtaskPolicyDisabled() {
		// Effort/fast discipline mutates the provider in place — but every provider
		// reachable here is SHARED (the parent's live provider, or a router-cache
		// instance reused across sessions), so mutating it would bleed this
		// subtask's effort into the parent + other sessions. Give the subtask a
		// provider it EXCLUSIVELY owns (a fresh build of the same model) first; if
		// that fails, skip discipline rather than corrupt the shared instance.
		owned, err := ownedSubtaskProvider(prov)
		if err != nil {
			where = joinWhere(where, "subtask effort skipped (own-provider build failed: "+err.Error()+")")
		} else {
			prov = owned
			compactor = llm.NewCompactor(owned)
			// Effort by POLICY: type sets the baseline (explore low … research/
			// code high, judge low-but-valid), difficulty nudges within it, and an
			// explicit opts.Effort overrides outright. This REPLACES the old
			// one-way trivial→medium downshift — a hard code subtask can now lift
			// to xhigh, a judge stays low regardless of the work's difficulty.
			level := opts.Effort
			if level == "" {
				level = llm.SubagentEffort(stype, opts.Difficulty)
			}
			if w := setSubtaskEffort(prov, level); w != "" {
				where = joinWhere(where, w)
			}
			// Latency: explore + judge (cheap, bounded jobs) take a fast-capable
			// provider's fast path; the heavier types keep quality over latency.
			if stype == llm.TypeExplore || stype == llm.TypeJudge {
				if w := applySubtaskFast(prov, "trivial"); w != "" {
					where = joinWhere(where, w)
				}
			}
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

// ownedSubtaskProvider builds a fresh provider instance for subtask-only live
// knobs (reasoning effort / fast mode). Do not call llm.New blindly for an
// unknown ModelID: with an empty provider, llm.New defaults to Mantle, so a
// test/mock provider named "safe" or "mock" can turn into a real network call
// to a non-existent Mantle model. Only clone models Eigen can resolve through
// its catalog/custom-provider registry; otherwise report a skip so callers keep
// the original provider UNMUTATED.
func ownedSubtaskProvider(prov llm.Provider) (llm.Provider, error) {
	return llm.CloneProvider(prov)
}

// firstNonEmpty returns the first non-blank string, or "".
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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

// subtaskPolicyDisabled reports whether the per-subagent effort/model policy is
// turned off (EIGEN_SUBTASK_EFFORT=keep) — the escape hatch that makes every
// subtask inherit the orchestrator's provider + effort untouched.
func subtaskPolicyDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("EIGEN_SUBTASK_EFFORT")), "keep")
}

// setSubtaskEffort sets a subtask provider's reasoning effort to the generic
// policy level (off/low/medium/high/xhigh), clamping to what THIS model
// supports — so a model lacking a rung still lands on the nearest legal one,
// and a model with no real reasoning ladder (e.g. GLM {off,on}) maps the level
// to on/off rather than being skipped. Unlike the old floor-only logic this
// goes BOTH ways: a hard code subtask can lift effort, a judge can pin it low.
// Never touches the orchestrator's own provider (subtask providers only).
// Returns a short "where" note when it changed something.
func setSubtaskEffort(prov llm.Provider, level string) string {
	level = strings.TrimSpace(level)
	if level == "" {
		return ""
	}
	es, ok := prov.(llm.EffortSetter)
	if !ok {
		return "" // non-reasoning model
	}
	levels := llm.ModelEffortLevels(prov.ModelID())
	target := clampEffortToModel(levels, level)
	if target == "" || strings.EqualFold(target, es.Effort()) {
		return ""
	}
	if es.SetEffort(target) {
		return "effort→" + target
	}
	return ""
}

// clampEffortToModel maps a generic effort level to the nearest level THIS model
// actually offers. Generic ladder is off<low<medium<high<xhigh; a model with a
// coarser ladder (GLM {off,on}) gets the proportional rung (off→off, anything
// higher→on), and a missing exact rung falls to the closest by position.
func clampEffortToModel(levels []string, want string) string {
	if len(levels) == 0 {
		return want // provider will validate/ignore
	}
	if effortRank(levels, want) >= 0 {
		return want
	}
	// Position `want` on the generic ladder, then scale to the model's ladder.
	generic := []string{"off", "none", "low", "medium", "high", "xhigh"}
	wi := -1
	for i, g := range generic {
		if strings.EqualFold(g, want) {
			wi = i
			break
		}
	}
	if wi < 0 {
		return levels[len(levels)/2] // unknown → model's middle
	}
	// Map proportionally low-end→first rung, high-end→last rung, rounding to the
	// NEAREST rung (so on a {off,on} ladder anything above the bottom lands on
	// "on" rather than collapsing to "off" via truncation).
	idx := (wi*(len(levels)-1)*2 + (len(generic) - 1)) / (2 * (len(generic) - 1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(levels) {
		idx = len(levels) - 1
	}
	return levels[idx]
}

// effortRank returns the index of level in the ordered set (lowest→highest), or
// -1 if absent. Used to compare two efforts so we only ever lower.
func effortRank(levels []string, level string) int {
	for i, l := range levels {
		if strings.EqualFold(l, level) {
			return i
		}
	}
	return -1
}

// applySubtaskFast turns ON the fast/low-latency path for trivial/easy subtasks
// on a fast-capable provider (Codex priority tier). A cheap mechanical
// delegation wants speed; medium/hard keep the configured tier (quality over
// latency). Only enables (never disables a provider the user set fast),
// subtask-only, same opt-out (EIGEN_SUBTASK_EFFORT=keep). Non-fast providers
// no-op.
func applySubtaskFast(prov llm.Provider, difficulty string) string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("EIGEN_SUBTASK_EFFORT")), "keep") {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(difficulty)) {
	case "trivial", "easy":
		// proceed
	default:
		return ""
	}
	fm, ok := prov.(llm.FastModer)
	if !ok || fm.FastMode() {
		return "" // not fast-capable, or already fast
	}
	if fm.SetFast(true) {
		return "fast (" + strings.ToLower(difficulty) + ")"
	}
	return ""
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
	s.lastCompactBefore = llm.EstimateTokens(msgs)
	s.lastCompactAfter = llm.EstimateTokens(compacted)
	budget := a.maxContextTokens()
	if budget <= 0 || s.lastCompactAfter <= int(compactTriggerFrac*float64(budget)) {
		s.compactStall = false
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

// compactTriggerFrac is the fraction of the budget at which proactive
// auto-compaction fires before a model call. Compacting at the FULL budget (1.0)
// means the priciest turns run at maximum context and risk a provider overflow;
// triggering with headroom keeps turns smaller and folds history before it gets
// expensive.
const compactTriggerFrac = 0.85

// compactTargetFrac is where auto-compaction aims after it fires. This is much
// close to the trigger but still below it: enough headroom to avoid immediate
// overflow without throwing away more recent context than necessary.
const compactTargetFrac = 0.80

// compactRecompactMinGrowthFrac is the minimum post-compaction growth before a
// second proactive compaction is worthwhile. With target=80% and trigger=85%, a
// long tool loop can otherwise compact after every ~5% tool-result bump forever.
// We still compact if the history reaches the full budget; this only suppresses
// repeated below-budget churn after a recent full/manual compaction.
const compactRecompactMinGrowthFrac = 0.15

// maybeCompact runs proactive auto-compaction with a circuit breaker. It fires
// before a model call when the conversation crosses compactTriggerFrac of the
// session budget — including between tool-use steps inside one long turn. After
// a compaction it records how much headroom remained; if a subsequent over-full
// step follows one that left too little headroom, it stops auto-compacting and
// emits a one-time note suggesting the user refocus or /clear, rather than
// spinning a summary call every step for negligible gain. It reports whether the
// session history was replaced and should be persisted.
func (s *Session) maybeCompact(ctx context.Context) bool { return s.maybeCompactWithNotes(ctx, true) }

func (s *Session) maybeCompactWithNotes(ctx context.Context, announce bool) bool {
	a := s.a
	msgs := s.snapshot()
	before := llm.EstimateTokens(msgs)
	budget := a.maxContextTokens()
	if budget <= 0 {
		return false
	}
	triggerAt := int(compactTriggerFrac * float64(budget))
	if before <= triggerAt {
		// Under the trigger threshold: reset the breaker so it can act again later.
		s.compactStall = false
		return false
	}
	if s.compactStall {
		// Already tripped and still over budget: don't keep summarizing.
		return false
	}
	// If the previous compaction left too little headroom and we're over the
	// trigger again, trip the breaker instead of compacting once more.
	if s.lastCompactAfter > 0 {
		headroom := budget - s.lastCompactAfter
		if float64(headroom) < compactStallHeadroomFrac*float64(budget) {
			s.compactStall = true
			a.emit(Event{Kind: EventNote, Text: "Context keeps refilling and compaction is no longer freeing much space. Consider /clear for a fresh thread or a more focused task."})
			return false
		}
		growth := before - s.lastCompactAfter
		minGrowth := int(compactRecompactMinGrowthFrac * float64(budget))
		if before < budget && growth >= 0 && growth < minGrowth {
			// A recent compaction left the history around the target, and only a
			// small amount has been added since. Do not pay for another summary just
			// because target=80% and trigger=85% are intentionally close; wait until
			// the post-compact working set has grown materially, or until it reaches
			// the full budget where overflow risk becomes real.
			return false
		}
	}
	// Target below the trigger threshold so the fold leaves headroom and won't
	// immediately re-trip on the next model/tool step.
	target := int(compactTargetFrac * float64(budget))
	if target <= 0 {
		target = triggerAt
	}
	if announce {
		a.emit(Event{Kind: EventNote, Text: fmt.Sprintf("context auto-compacting: ~%d tokens (target ~%d)…", before, target)})
	}
	compacted, err := llm.CompactWith(ctx, a.compactor(), msgs, target)
	if err != nil {
		return false
	}
	s.lastCompactBefore = before
	s.lastCompactAfter = llm.EstimateTokens(compacted)
	s.setMsgs(compacted)
	if announce && s.lastCompactAfter < before {
		a.emit(Event{Kind: EventNote, Text: fmt.Sprintf("context auto-compacted: ~%d → ~%d tokens", before, s.lastCompactAfter)})
	}
	return true
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
	afterTokens := llm.EstimateTokens(compacted)
	if len(compacted) >= before && afterTokens >= curTokens {
		return false // compaction couldn't shrink it; don't spin
	}
	s.lastCompactBefore = curTokens
	s.lastCompactAfter = afterTokens
	s.compactStall = false
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

func (a *Agent) pluginRoleCatalog() string {
	if a == nil || a.Tools == nil {
		return ""
	}
	_, canTask := a.Tools.Get("task")
	_, canTaskGroup := a.Tools.Get("task_group")
	return PluginRoleCatalog(canTask, canTaskGroup)
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
	s.persist()
	defer s.clearTurnTools()       // per-turn allowed-tools never leak into a later turn
	allow := s.turnTools()         // nil = unrestricted; non-empty = command's allowed-tools
	disclose := a.Tools.HasNiche() // progressive tool disclosure when any niche tools exist
	emptyTurns := 0
	reasoningOnlyTurns := 0               // consecutive reason-only turns (thinking across turns; bounded so a never-acting model can't spin forever)
	overflowRetried := false              // guard: force-compact-and-retry at most once per step
	var usedIn, usedOut int               // provider-reported usage, summed over the turn
	var usedCacheRead, usedCacheWrite int // prompt-cache hits/writes, summed over the turn
	autoCompactAnnounced := false         // avoid spamming the UI during one tool-heavy turn

	system := systemPrompt
	if a.ExtraSystem != "" {
		system += "\n\n" + a.ExtraSystem
	}
	if roleCatalog := a.pluginRoleCatalog(); roleCatalog != "" {
		system += "\n\n" + roleCatalog
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
		// Background-shell awareness: surface any shells you started so you
		// remember to poll/collect them — backgrounding a command then never
		// checking it is the failure mode this prevents.
		if bs := a.backgroundShellStatus(); bs != "" {
			sys += bs
		}
		// Proactive compaction must run before EVERY model request, not only at
		// the top of a user turn: a single long turn can accumulate enough tool
		// results to overflow the very next step.
		if a.maxContextTokens() > 0 && s.maybeCompactWithNotes(ctx, !autoCompactAnnounced) {
			autoCompactAnnounced = true
			s.persist()
		}
		// Progressive tool disclosure: send core tools' full schemas + the
		// already-unlocked niche tools, and list the rest by name so the model
		// can unlock them with search_tools. Recomputed per step because a
		// search_tools call mid-turn unlocks more.
		specs := a.Tools.Specs()
		if disclose {
			unlocked := a.unlockedSet()
			specs = a.Tools.CoreSpecs(unlocked)
			groups, loose := a.Tools.GroupCatalog(unlocked)
			if len(groups) > 0 || len(loose) > 0 {
				sys += "\n\nMORE TOOLS (not loaded — call search_tools to open them):"
				for _, g := range groups {
					sys += fmt.Sprintf("\n- %s (%d tools) — %s [search_tools \"%s\"]", g.Name, g.Count, g.Gist, g.Name)
				}
				for _, l := range loose {
					sys += "\n- " + l
				}
			}
		}
		// Per-turn allowed-tools (a slash command's `allowed-tools`): offer only
		// the permitted tools to the model. Enforcement is belt-and-suspenders —
		// dispatch also denies a non-allowed call below.
		if len(allow) > 0 {
			specs = filterSpecs(specs, allow)
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
		// Make provider failover VISIBLE: carry a per-turn notifier on the context
		// (NOT on the shared provider — concurrent sessions share provider
		// instances) so when the primary fails over to its fallback this turn, we
		// surface a note with the cause instead of silently serving a weaker model.
		callCtx := llm.WithFallbackNotifier(ctx, func(n llm.FallbackNotice) {
			a.emit(Event{Kind: EventNote, Step: step, Text: fmt.Sprintf(
				"%s unavailable (%s) — falling back to %s for this turn.",
				n.PrimaryID, truncateForNote(n.Cause.Error()), n.FallbackID)})
		})
		// Heartbeat the subtask watchdog at model-call START: a non-streaming
		// Complete() (e.g. the Converse/opus path) emits NOTHING until it
		// returns, so a slow-but-healthy inference would otherwise look "idle"
		// and trip the stall cancel. onModelCall switches the watchdog to its
		// larger in-flight budget (modelMaxWait). nil for the top-level agent
		// (not stall-watched); set by runChild for subtasks.
		if a.onModelCall != nil {
			a.onModelCall()
		}
		if sm, ok := prov.(llm.Streamer); ok && a.OnEvent != nil {
			streamed = true
			sink := func(c llm.StreamChunk) {
				kind := EventTextDelta
				if c.Kind == llm.ChunkReasoning {
					kind = EventReasoningDelta
				}
				a.emit(Event{Kind: kind, Step: step, Text: c.Text})
			}
			resp, err = sm.Stream(callCtx, req, sink)
		} else {
			resp, err = prov.Complete(callCtx, req)
		}
		if err != nil {
			// Error-driven compaction: if the provider rejected the request as
			// too large for its context window, shrink the conversation and
			// retry this step once. This is the context-size sibling of the
			// TUI's overload→failover path, and works for every provider.
			if llm.IsContextOverflow(err) && !overflowRetried {
				overflowRetried = true
				a.emit(Event{Kind: EventNote, Text: "Prompt exceeded the model's context window — compacting and retrying…"})
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
		usedCacheRead += resp.Usage.CacheReadTokens
		usedCacheWrite += resp.Usage.CacheWriteTokens
		if len(resp.ToolCalls) == 0 {
			if strings.TrimSpace(resp.Text) != "" {
				// Final answer — UNLESS a steer landed during this last model
				// call. A steer that arrives in the narrow window after the
				// step's drainSteer() but before the turn ends would otherwise
				// sit unconsumed until some future turn (the user saw the turn
				// end and assumes their message was dropped/queued). Instead,
				// record the answer and loop once more so the next iteration
				// drains the steer and the model responds to it — making a
				// late steer behave like a same-turn follow-up, not a silent
				// wait.
				s.appendMsg(llm.Message{
					Role:               llm.RoleAssistant,
					Text:               resp.Text,
					Reasoning:          resp.Reasoning,
					ReasoningID:        resp.ReasoningID,
					ReasoningEncrypted: resp.ReasoningEncrypted,
				})
				s.persist()
				if s.hasSteer() {
					// Non-streaming providers haven't shown this text yet; emit
					// it so the answer-before-the-steer is visible (streaming
					// already delivered it as deltas).
					if !streamed {
						a.emit(Event{Kind: EventTextDelta, Step: step, Text: resp.Text})
					}
					continue
				}
				prov := a.provider()
				providerName, modelID := "", ""
				if prov != nil {
					providerName, modelID = prov.Name(), prov.ModelID()
				}
				a.emit(Event{Kind: EventDone, Step: step, Text: resp.Text, Provider: providerName, Model: modelID, InTokens: usedIn, OutTokens: usedOut, CacheReadTokens: usedCacheRead, CacheWriteTokens: usedCacheWrite})
				return resp.Text, nil // final answer
			}
			// Empty turn: no tool call AND no text. A turn that produced
			// REASONING is NOT idle — the model is thinking across turns
			// (Codex/gpt-5.x reason-then-act in separate Responses turns). Carry
			// the reasoning forward (so the chain of thought isn't lost and the
			// model resumes instead of re-thinking) and loop. Bounded by
			// maxReasoningOnlyTurns so a model that NEVER acts can't spin forever.
			if strings.TrimSpace(resp.Reasoning) != "" {
				reasoningOnlyTurns++
				if reasoningOnlyTurns > maxReasoningOnlyTurns {
					return "", fmt.Errorf("model produced %d reasoning-only turns without acting", reasoningOnlyTurns)
				}
				s.appendMsg(llm.Message{
					Role:               llm.RoleAssistant,
					Reasoning:          resp.Reasoning,
					ReasoningID:        resp.ReasoningID,
					ReasoningEncrypted: resp.ReasoningEncrypted,
				})
				s.persist()
				if !streamed {
					a.emit(Event{Kind: EventReasoningDelta, Step: step, Text: resp.Reasoning})
				}
				continue
			}
			// Genuinely empty (no text, no tools, no reasoning): nudge to act,
			// bounded.
			emptyTurns++
			if emptyTurns > maxEmptyTurns {
				return "", fmt.Errorf("model returned no actionable output after %d empty turns", emptyTurns)
			}
			s.appendMsg(llm.Message{
				Role: llm.RoleUser,
				Text: "Continue: use a tool to make progress, or give your final answer.",
			})
			s.persist()
			continue
		}
		emptyTurns = 0
		reasoningOnlyTurns = 0

		s.appendMsg(llm.Message{
			Role:               llm.RoleAssistant,
			Text:               resp.Text,
			Reasoning:          resp.Reasoning,
			ReasoningID:        resp.ReasoningID,
			ReasoningEncrypted: resp.ReasoningEncrypted,
			ToolCalls:          resp.ToolCalls,
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
			result, isErr := a.dispatch(ctx, tc, allow)
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

// normalizeToolName lowercases a tool name and strips any Claude-style argument
// scope, so an allowed-tools entry like "Bash(git:*)" or "Read" matches the
// registered tool name ("bash", "read"). Hyphens/underscores are unified too
// (e.g. "MultiEdit" stays, "task-group" → "task_group").
func normalizeToolName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.ReplaceAll(s, "-", "_")
}

// toolAllowed reports whether name is permitted by the allowlist (case- and
// scope-insensitive). An empty allowlist means unrestricted (callers guard).
func toolAllowed(name string, allow []string) bool {
	n := normalizeToolName(name)
	for _, a := range allow {
		if normalizeToolName(a) == n {
			return true
		}
	}
	return false
}

// filterSpecs returns the subset of specs whose name is in the allowlist
// (normalized match). Used to offer a slash command's allowed-tools only.
func filterSpecs(specs []llm.ToolSpec, allow []string) []llm.ToolSpec {
	out := specs[:0:0]
	for _, sp := range specs {
		if toolAllowed(sp.Name, allow) {
			out = append(out, sp)
		}
	}
	return out
}

// dispatch runs one tool call, enforcing the permission posture, and returns the
// result (or an error string) to feed back to the model plus whether it failed.
// allow, when non-empty, is the per-turn allowed-tools allowlist: a call to a
// tool outside it is denied (belt-and-suspenders with the spec filtering).
func (a *Agent) dispatch(ctx context.Context, tc llm.ToolCall, allow []string) (tool.Result, bool) {
	errResult := func(s string) (tool.Result, bool) { return tool.Result{Text: s}, true }
	if len(allow) > 0 && !toolAllowed(tc.Name, allow) {
		return errResult(fmt.Sprintf("Denied: tool %q is not in this command's allowed-tools.", tc.Name))
	}
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

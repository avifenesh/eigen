// Package chat defines the seam between the chat UI and whatever runs the
// conversation. The SAME rich TUI drives both:
//
//   - Local: an in-process agent (today's standalone `eigen` chat), and
//   - Remote: a session hosted in the eigen daemon, attached over the socket.
//
// The interface is the audited coupling surface of the TUI (every m.a.* /
// m.session.* touchpoint), not an aspirational abstraction: if the TUI doesn't
// call it, it isn't here.
package chat

import (
	"context"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
)

// ToolInfo describes one registered tool for display.
type ToolInfo struct {
	Name     string
	ReadOnly bool
}

// ShellInfo describes one backgrounded bash shell for the shells panel.
type ShellInfo struct {
	ID       string
	Command  string
	Status   string // running | exited | killed
	ExitCode int
	LastLine string
}

// Backend runs one conversation for the chat UI.
type Backend interface {
	// Send runs a turn (with optional images). Progress arrives through the
	// event sink configured at construction; Send returns the final answer.
	Send(ctx context.Context, task string, images []llm.Image) (string, error)
	// Resend retries the last user turn (after a transient failure).
	Resend(ctx context.Context) (string, error)

	// Messages is the full conversation history (for rendering, saving,
	// compaction estimates). Remote backends serve a synced copy.
	Messages() []llm.Message
	// Tokens estimates the current context size.
	Tokens() int
	// Running reports whether a turn is in flight right now — true when a view
	// ATTACHES to a session whose turn another view (or no view) started, so
	// the UI shows "working" and queues input instead of erroring "busy".
	Running() bool
	// Compact summarizes the conversation toward targetTokens; returns
	// before/after message counts.
	Compact(ctx context.Context, targetTokens int) (before, after int, err error)

	// Model/Perm/Goal state (shown in the status bar; mutated by commands).
	ModelID() string
	ProviderName() string
	SetModel(provider llm.Provider, compactor llm.Compactor, maxTokens int) // live switch
	MaxContextTokens() int
	Perm() agent.Permission
	SetPerm(agent.Permission)
	Goal() string
	SetGoal(string)

	// Title is the session's display name (status bar, switcher, app). "" =
	// derived from the first user message. SetTitle renames it (persisted).
	Title() string
	SetTitle(string)

	// Effort/Search expose the provider's reasoning-effort and live-search
	// settings as chat state ("" = the model has no such setting), so the UI
	// never needs the provider handle — remote backends carry these over the
	// socket. SetEffort/SetSearch return false for an unknown level/mode.
	Effort() string
	SetEffort(string) bool
	SearchMode() string
	SetSearch(string) bool
	// FastMode/SetFast expose the provider's fast/low-latency service tier
	// (Codex "priority") as chat state. FastSupported is false when the model
	// has no fast path (the segment is hidden then). Remote backends carry
	// these over the socket.
	FastSupported() bool
	FastMode() bool
	SetFast(bool) bool

	// Tools lists registered tools (for /tools). Empty when unknown.
	Tools() []ToolInfo

	// SetTurnTools restricts the NEXT Send to the given tool names (a slash
	// command's allowed-tools); empty clears. Scoped to one turn by the agent.
	SetTurnTools([]string)

	// Shells lists the backgrounded bash shells (the shells panel). Empty when
	// none or unsupported. KillShell stops one by id (returns false if unknown
	// or already stopped).
	Shells() []ShellInfo
	KillShell(id string) bool

	// DetachBash backgrounds the bash command running RIGHT NOW in the current
	// turn (the user's "background this step" key) — it keeps running as a
	// shell while the agent continues. Returns true if a foreground bash was
	// running and got detached.
	DetachBash() bool

	// AddDir extends the tool sandbox with an additional allowed directory —
	// the user-invoked /add-dir grant (never the agent). Returns the normalized
	// root that was added. Roots lists the current allowed directories (primary
	// first). Remote backends carry both over the socket.
	AddDir(path string) (string, error)
	Roots() []string

	// Steer injects a message into a RUNNING turn — it lands between tool-call
	// rounds (mid-turn course-correction), not deferred to the next turn.
	// Returns true when a turn was running and the message was steered; false
	// when idle (the caller should Send instead). Never blocks on the turn.
	Steer(text string, images []llm.Image) bool

	// Provider exposes the live provider for capability checks (vision,
	// streaming) and the router. May be nil for remote backends.
	Provider() llm.Provider

	// Reset replaces the conversation (the /resume and /clear commands).
	Reset(history []llm.Message)

	// Wire connects the backend to the UI before the first Send:
	//   - sink receives agent events (including EventApproval for blocked
	//     gated tool calls — both local and remote broadcast approvals as
	//     events, answered with Answer).
	//   - persist is called after every appended message (autosave). Remote
	//     backends may ignore it (the daemon persists).
	Wire(sink agent.EventSink, persist func([]llm.Message))

	// Answer resolves a pending approval that arrived as an EventApproval
	// (Event.Result carries the approval id).
	Answer(approvalID string, allow bool)
}

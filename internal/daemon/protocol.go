package daemon

import (
	"encoding/json"

	"github.com/avifenesh/eigen/internal/agent"
	"github.com/avifenesh/eigen/internal/llm"
)

// Builder constructs an agent rooted at dir with the given model (empty = the
// daemon's default). Injected by package main (which owns tool/provider wiring)
// so this package stays transport-only. Returns the agent and a close func for
// the session's external resources (MCP/LSP/observe).
type Builder func(dir, model string) (*agent.Agent, func(), error)

// Request is a view→daemon command (line-delimited JSON over the socket).
type Request struct {
	Op       string `json:"op"`                 // list|new|attach|input|interrupt|remove|approve|state|set|compact|clear|resend|ping|add-dir
	ID       string `json:"id,omitempty"`       // session id
	Dir      string `json:"dir,omitempty"`      // new: working directory
	Model    string `json:"model,omitempty"`    // new / set: model id
	Text     string `json:"text,omitempty"`     // input: the message
	Approval string `json:"approval,omitempty"` // approve: pending approval id
	Allow    bool   `json:"allow,omitempty"`    // approve: the verdict
	// set: exactly one of these mutates session state
	Perm  string  `json:"perm,omitempty"`
	Goal  *string `json:"goal,omitempty"`  // pointer: empty string clears the goal
	Title *string `json:"title,omitempty"` // pointer: empty string clears (revert to derived)
	// compact: target tokens (0 = backend default)
	Target int `json:"target,omitempty"`
	// input: optional image attachments
	Images []llm.Image `json:"images,omitempty"`
	// new: optional resumed history + initial goal
	History []llm.Message `json:"history,omitempty"`
	// set: effort / search ("" = not in this request)
	Effort string `json:"effort,omitempty"`
	Search string `json:"search,omitempty"`
	Fast   *bool  `json:"fast,omitempty"` // set fast/priority service tier (pointer distinguishes on/off from absent)
	// add-dir: an additional allowed sandbox directory (user grant)
	AddDir string `json:"add_dir,omitempty"`
	// kill-shell: the backgrounded shell id to stop
	Shell string `json:"shell,omitempty"`
}

// Response is a daemon→view message. Type discriminates the payload.
type Response struct {
	Type     string        `json:"type"` // ok | error | sessions | attached | event | state | compacted
	Error    string        `json:"error,omitempty"`
	ID       string        `json:"id,omitempty"`       // session id (new/attached)
	Root     string        `json:"root,omitempty"`     // add-dir: the normalized root added
	Sessions []SessionInfo `json:"sessions,omitempty"` // list
	Event    *WireEvent    `json:"event,omitempty"`    // streamed agent event
	Replay   bool          `json:"replay,omitempty"`   // event is from the replay buffer
	State    *SessionState `json:"state,omitempty"`    // state op result
	Before   int           `json:"before,omitempty"`   // compact result (message counts)
	After    int           `json:"after,omitempty"`
	Pruned   []string      `json:"pruned,omitempty"`   // prune result: removed session ids
	Steered  bool          `json:"steered,omitempty"`  // input: delivered as a mid-turn steer (a turn was running)
	Killed   bool          `json:"killed,omitempty"`   // kill-shell: a running shell was signaled
	Detached bool          `json:"detached,omitempty"` // detach-bash: a foreground bash was backgrounded
	Stats    *DaemonStats  `json:"stats,omitempty"`    // stats op: daemon resource health
}

// DaemonStats is the daemon's resource-health snapshot (the `stats` op): enough
// to spot leaks/growth over long uptime without attaching a profiler.
type DaemonStats struct {
	UptimeSec    int64  `json:"uptime_sec"`
	Goroutines   int    `json:"goroutines"`
	HeapAllocB   uint64 `json:"heap_alloc_b"` // live heap bytes
	HeapSysB     uint64 `json:"heap_sys_b"`   // heap reserved from OS
	RSSB         uint64 `json:"rss_b"`        // resident set (0 if unavailable)
	NumGC        uint32 `json:"num_gc"`
	Sessions     int    `json:"sessions"`      // hosted sessions
	Views        int    `json:"views"`         // attached views across all sessions
	RunningTurns int    `json:"running_turns"` // turns in flight
	BgTasks      int    `json:"bg_tasks"`      // in-memory background-task records
	GoVersion    string `json:"go_version,omitempty"`

	// Cumulative token usage across all hosted sessions since daemon start.
	// CacheReadTokens vs InputTokens is the prompt-cache hit rate — the headline
	// token-efficiency metric (Tier 30).
	InputTokens      int64 `json:"input_tokens,omitempty"`
	OutputTokens     int64 `json:"output_tokens,omitempty"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
}

// SessionState is the snapshot a remote chat UI needs to render history and
// status: the conversation plus model/perm/goal/budget/tools.
type SessionState struct {
	Messages  []llm.Message `json:"messages"`
	Tokens    int           `json:"tokens"`
	Title     string        `json:"title,omitempty"`
	Model     string        `json:"model"`
	Provider  string        `json:"provider"`
	MaxTokens int           `json:"max_tokens"`
	Perm      string        `json:"perm"`
	Goal      string        `json:"goal"`
	Effort    string        `json:"effort,omitempty"`  // "" = unsupported
	Search    string        `json:"search,omitempty"`  // "" = unsupported
	Fast      bool          `json:"fast,omitempty"`    // fast/priority service tier active
	FastOK    bool          `json:"fast_ok,omitempty"` // model supports a fast tier (segment shown)
	Running   bool          `json:"running,omitempty"` // a turn is in flight right now
	Tools     []ToolInfo    `json:"tools,omitempty"`
	Roots     []string      `json:"roots,omitempty"`  // tool sandbox allowed dirs (primary first)
	Shells    []ShellInfo   `json:"shells,omitempty"` // backgrounded bash shells
}

// ShellInfo mirrors chat.ShellInfo over the wire (backgrounded bash shells).
type ShellInfo struct {
	ID       string `json:"id"`
	Command  string `json:"command"`
	Status   string `json:"status"`
	ExitCode int    `json:"exit_code"`
	LastLine string `json:"last_line,omitempty"`
}

// ToolInfo mirrors chat.ToolInfo over the wire.
type ToolInfo struct {
	Name     string `json:"name"`
	ReadOnly bool   `json:"read_only"`
}

// WireEvent is agent.Event flattened for the socket (kind as a string).
type WireEvent struct {
	Kind      string          `json:"kind"`
	Step      int             `json:"step,omitempty"`
	Text      string          `json:"text,omitempty"`
	ToolName  string          `json:"tool,omitempty"`
	ToolID    string          `json:"tool_id,omitempty"`
	ToolArgs  json.RawMessage `json:"tool_args,omitempty"`
	Result    string          `json:"result,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	InTokens  int             `json:"in_toks,omitempty"`
	OutTokens int             `json:"out_toks,omitempty"`
}

func wireEvent(e agent.Event) *WireEvent {
	return &WireEvent{
		Kind:      eventKindName(e.Kind),
		Step:      e.Step,
		Text:      e.Text,
		ToolName:  e.ToolName,
		ToolID:    e.ToolID,
		ToolArgs:  e.ToolArgs,
		Result:    e.Result,
		IsError:   e.IsError,
		InTokens:  e.InTokens,
		OutTokens: e.OutTokens,
	}
}

func eventKindName(k agent.EventKind) string {
	switch k {
	case agent.EventTextDelta:
		return "text"
	case agent.EventReasoningDelta:
		return "reasoning"
	case agent.EventToolStart:
		return "tool_start"
	case agent.EventToolResult:
		return "tool_result"
	case agent.EventDone:
		return "done"
	case agent.EventNote:
		return "note"
	case agent.EventApproval:
		return "approval"
	case agent.EventBgDone:
		return "bg_done" // the wake signal; the sibling EventNote handles display
	}
	return "unknown"
}

// encode marshals a response to a single JSON line.
func encode(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

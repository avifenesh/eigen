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
	// add-dir: an additional allowed sandbox directory (user grant)
	AddDir string `json:"add_dir,omitempty"`
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
	Pruned   []string      `json:"pruned,omitempty"` // prune result: removed session ids
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
	Running   bool          `json:"running,omitempty"` // a turn is in flight right now
	Tools     []ToolInfo    `json:"tools,omitempty"`
	Roots     []string      `json:"roots,omitempty"` // tool sandbox allowed dirs (primary first)
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

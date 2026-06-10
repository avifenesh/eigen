package daemon

import (
	"encoding/json"

	"github.com/avifenesh/eigen/internal/agent"
)

// Builder constructs an agent rooted at dir with the given model (empty = the
// daemon's default). Injected by package main (which owns tool/provider wiring)
// so this package stays transport-only. Returns the agent and a close func for
// the session's external resources (MCP/LSP/observe).
type Builder func(dir, model string) (*agent.Agent, func(), error)

// Request is a view→daemon command (line-delimited JSON over the socket).
type Request struct {
	Op       string `json:"op"`                 // list | new | attach | input | interrupt | remove | approve | ping
	ID       string `json:"id,omitempty"`       // session id (attach/input/interrupt/remove/approve)
	Dir      string `json:"dir,omitempty"`      // new: working directory
	Model    string `json:"model,omitempty"`    // new: model
	Text     string `json:"text,omitempty"`     // input: the message
	Approval string `json:"approval,omitempty"` // approve: pending approval id
	Allow    bool   `json:"allow,omitempty"`    // approve: the verdict
}

// Response is a daemon→view message. Type discriminates the payload.
type Response struct {
	Type     string        `json:"type"` // ok | error | sessions | attached | event
	Error    string        `json:"error,omitempty"`
	ID       string        `json:"id,omitempty"`       // session id (new/attached)
	Sessions []SessionInfo `json:"sessions,omitempty"` // list
	Event    *WireEvent    `json:"event,omitempty"`    // streamed agent event
	Replay   bool          `json:"replay,omitempty"`   // event is from the replay buffer
}

// WireEvent is agent.Event flattened for the socket (kind as a string).
type WireEvent struct {
	Kind     string `json:"kind"`
	Step     int    `json:"step,omitempty"`
	Text     string `json:"text,omitempty"`
	ToolName string `json:"tool,omitempty"`
	Result   string `json:"result,omitempty"`
	IsError  bool   `json:"is_error,omitempty"`
}

func wireEvent(e agent.Event) *WireEvent {
	return &WireEvent{
		Kind:     eventKindName(e.Kind),
		Step:     e.Step,
		Text:     e.Text,
		ToolName: e.ToolName,
		Result:   e.Result,
		IsError:  e.IsError,
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

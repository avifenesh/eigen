// Package llm defines eigen's provider contract: the minimal surface every
// model backend (Bedrock mantle, Bedrock Converse, local llama) implements,
// normalized so the agent loop never sees provider-specific shapes.
package llm

import (
	"context"
	"encoding/json"
)

// Role identifies the author of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolSpec is a provider-neutral tool description. Parameters is a JSON Schema
// object describing the tool's arguments; each provider wraps this into its own
// wire format (Responses function tool, Converse toolSpec, etc.).
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is a model's request to invoke a tool.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Message is one turn in the conversation.
type Message struct {
	Role Role
	Text string

	// Reasoning is the model's concise reasoning summary for this turn; it is
	// carried back across turns to preserve the chain of thought through tool
	// calls. ReasoningID is the provider's id for that reasoning item, if any.
	Reasoning   string
	ReasoningID string

	// ToolCalls is set on an assistant turn that invokes tools.
	ToolCalls []ToolCall

	// ToolCallID and ToolName identify which call a RoleTool message answers.
	ToolCallID string
	ToolName   string
}

// Request is a single completion request, normalized across providers.
type Request struct {
	System   string
	Messages []Message
	Tools    []ToolSpec
}

// Response is a normalized completion result. Either Text, ToolCalls, or both
// may be set; an empty ToolCalls slice signals the model is done.
type Response struct {
	Text        string
	Reasoning   string
	ReasoningID string
	ToolCalls   []ToolCall
}

// Provider is any model backend eigen can drive.
type Provider interface {
	// Name is a human-readable label for logs and the model picker.
	Name() string
	// Complete runs a non-streaming completion.
	Complete(ctx context.Context, req Request) (*Response, error)
}

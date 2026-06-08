// Package llm defines eigen's provider contract: the minimal surface every
// model backend (Bedrock mantle, Bedrock Converse, local llama) implements,
// normalized so the agent loop never sees provider-specific shapes.
package llm

import "context"

// Role identifies the author of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one turn in the conversation. Tool-call fields are added when we
// wire tools; for now a message is just authored text.
type Message struct {
	Role Role
	Text string
}

// Request is a single completion request, normalized across providers.
type Request struct {
	System   string
	Messages []Message
}

// Response is a normalized completion result.
type Response struct {
	Text string
}

// Provider is any model backend eigen can drive.
type Provider interface {
	// Name is a human-readable label for logs and the model picker.
	Name() string
	// Complete runs a non-streaming completion.
	Complete(ctx context.Context, req Request) (*Response, error)
}

package llm

import (
	"context"
	"os"
	"strings"
)

// Llama drives any OpenAI-compatible /v1/chat/completions server — primarily a
// local llama-server (llama.cpp), but anything speaking that dialect works. It
// is a thin wrapper over the shared chatClient: tools nest under "function",
// tool calls come back in message.tool_calls, and streaming uses SSE deltas.
type Llama struct {
	c *chatClient
}

// NewLlama builds a Llama provider. Base URL defaults to a local llama-server;
// override with EIGEN_LLAMA_BASE_URL. EIGEN_LLAMA_API_KEY is optional.
func NewLlama(model string) (*Llama, error) {
	base := os.Getenv("EIGEN_LLAMA_BASE_URL")
	if base == "" {
		base = "http://localhost:8080/v1"
	}
	if model == "" {
		model = "local"
	}
	return &Llama{
		c: newChatClient(strings.TrimRight(base, "/"), model, os.Getenv("EIGEN_LLAMA_API_KEY"), "llama"),
	}, nil
}

func (l *Llama) Name() string { return l.c.model + " (llama /v1)" }

func (l *Llama) Complete(ctx context.Context, req Request) (*Response, error) {
	return l.c.complete(ctx, req)
}

// Stream runs a chat-completions request with stream:true, forwarding content
// deltas to sink and assembling streamed tool-call deltas into the Response.
func (l *Llama) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	return l.c.stream(ctx, req, sink)
}

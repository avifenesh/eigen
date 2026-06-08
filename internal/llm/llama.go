package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Llama drives any OpenAI-compatible /v1/chat/completions server (llama.cpp's
// llama-server, Ollama, vLLM, etc.). Unlike mantle it speaks chat-completions:
// tools nest under "function", tool calls come back in message.tool_calls, and
// the system role is "system" (not "developer").
type Llama struct {
	BaseURL string
	Model   string
	apiKey  string
	http    *http.Client
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
		BaseURL: strings.TrimRight(base, "/"),
		Model:   model,
		apiKey:  os.Getenv("EIGEN_LLAMA_API_KEY"),
		http:    &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (l *Llama) Name() string { return l.Model + " (llama /v1)" }

type chatFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools,omitempty"`
}

type chatReply struct {
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (l *Llama) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	payload := chatRequest{
		Model:    l.Model,
		Messages: llamaMessages(req),
		Tools:    llamaTools(req.Tools),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	headers := map[string]string{}
	if l.apiKey != "" {
		headers["Authorization"] = "Bearer " + l.apiKey
	}
	raw, status, err := httpJSON(ctx, l.http, l.BaseURL+"/chat/completions", headers, body)
	if err != nil {
		return nil, fmt.Errorf("llama: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("llama HTTP %d: %s", status, string(raw))
	}

	var reply chatReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if reply.Error != nil {
		return nil, fmt.Errorf("llama error: %s", reply.Error.Message)
	}
	if len(reply.Choices) == 0 {
		return nil, fmt.Errorf("llama returned no choices")
	}

	msg := reply.Choices[0].Message
	out := &Response{Text: msg.Content}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: normalizeArgs(tc.Function.Arguments),
		})
	}
	return out, nil
}

func llamaMessages(req Request) []chatMessage {
	msgs := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleTool:
			msgs = append(msgs, chatMessage{Role: "tool", ToolCallID: m.ToolCallID, Content: m.Text})
		case RoleAssistant:
			cm := chatMessage{Role: "assistant", Content: m.Text}
			for _, tc := range m.ToolCalls {
				cm.ToolCalls = append(cm.ToolCalls, chatToolCall{
					ID:       tc.ID,
					Type:     "function",
					Function: chatFunction{Name: tc.Name, Arguments: argString(tc.Arguments)},
				})
			}
			msgs = append(msgs, cm)
		default: // system / user
			msgs = append(msgs, chatMessage{Role: string(m.Role), Content: m.Text})
		}
	}
	return msgs
}

func llamaTools(specs []ToolSpec) []chatTool {
	if len(specs) == 0 {
		return nil
	}
	tools := make([]chatTool, 0, len(specs))
	for _, s := range specs {
		var t chatTool
		t.Type = "function"
		t.Function.Name = s.Name
		t.Function.Description = s.Description
		t.Function.Parameters = s.Parameters
		tools = append(tools, t)
	}
	return tools
}

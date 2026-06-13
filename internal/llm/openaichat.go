package llm

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// chatClient is a reusable OpenAI-compatible /v1/chat/completions backend shared
// by providers that speak that protocol (xAI Grok, Zhipu GLM, local llama-style
// servers). It normalizes eigen's transcript to chat messages, wraps tools in
// the function-tool shape, and parses both non-streaming and streamed replies.
//
// label is the provider's human label (used in Name and error messages).
// extra, if non-nil, returns provider-specific top-level request fields to merge
// into the JSON body (e.g. Grok's search_parameters); it is called per request.
// Wire types for the chat-completions dialect, shared by every provider built
// on chatClient (llama, grok, glm).

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
	Role string `json:"role"`
	// Content is a plain string for text-only messages, or a part array
	// ([]chatPart: text + image_url blocks) for vision input.
	Content    any            `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// chatPart is one typed content block of a multimodal user message.
type chatPart struct {
	Type     string        `json:"type"`                // "text" | "image_url"
	Text     string        `json:"text,omitempty"`      // for type=text
	ImageURL *chatImageURL `json:"image_url,omitempty"` // for type=image_url
}

type chatImageURL struct {
	URL string `json:"url"` // data URL: data:image/png;base64,...
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
	Stream   bool          `json:"stream,omitempty"`
}

type chatReply struct {
	Choices []struct {
		Message struct {
			Content          string         `json:"content"`
			ReasoningContent string         `json:"reasoning_content"`
			ToolCalls        []chatToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage chatUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// chatUsage is the OpenAI-compatible usage block.
type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type chatClient struct {
	baseURL string
	model   string
	apiKey  string
	label   string
	http    *http.Client
	extra   func() map[string]any

	// extraHeaders are static headers added to every request (e.g. the grok-cli
	// proxy's client-version/token-auth headers). Optional.
	extraHeaders map[string]string

	// extraTools, if set, returns provider-specific tool entries to append to
	// the standard function-tool list in the request body (e.g. GLM's
	// {"type":"web_search"}). Returns nil to add nothing.
	extraTools func() []map[string]any
}

// newChatClient builds a chatClient with a 5-minute timeout. baseURL should end
// without a trailing slash (callers pass the /v1 root).
func newChatClient(baseURL, model, apiKey, label string) *chatClient {
	return &chatClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		label:   label,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *chatClient) headers() map[string]string {
	h := map[string]string{}
	if c.apiKey != "" {
		h["Authorization"] = "Bearer " + c.apiKey
	}
	for k, v := range c.extraHeaders {
		h[k] = v
	}
	return h
}

// body builds the request JSON, merging any provider-specific extra fields.
func (c *chatClient) body(req Request, stream bool) ([]byte, error) {
	payload := map[string]any{
		"model":    c.model,
		"messages": chatMessagesFrom(req),
	}
	if tools := chatToolsFrom(req.Tools); len(tools) > 0 {
		payload["tools"] = tools
	}
	// Provider-specific built-in tools (e.g. GLM web_search) go alongside the
	// function tools in the tools array.
	if c.extraTools != nil {
		if extra := c.extraTools(); len(extra) > 0 {
			var merged []any
			if existing, ok := payload["tools"]; ok {
				for _, t := range existing.([]chatTool) {
					merged = append(merged, t)
				}
			}
			for _, t := range extra {
				merged = append(merged, t)
			}
			payload["tools"] = merged
		}
	}
	if stream {
		payload["stream"] = true
	}
	if c.extra != nil {
		for k, v := range c.extra() {
			payload[k] = v
		}
	}
	return json.Marshal(payload)
}

// complete runs a non-streaming chat completion.
func (c *chatClient) complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	body, err := c.body(req, false)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	raw, status, err := httpJSON(ctx, c.http, c.baseURL+"/chat/completions", c.headers(), body, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.label, err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%s HTTP %d: %s", c.label, status, string(raw))
	}

	var reply chatReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if reply.Error != nil {
		return nil, fmt.Errorf("%s error: %s", c.label, reply.Error.Message)
	}
	if len(reply.Choices) == 0 {
		return nil, fmt.Errorf("%s returned no choices", c.label)
	}

	msg := reply.Choices[0].Message
	out := &Response{Text: msg.Content, Reasoning: msg.ReasoningContent,
		Usage: Usage{InputTokens: reply.Usage.PromptTokens, OutputTokens: reply.Usage.CompletionTokens}}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: normalizeArgs(tc.Function.Arguments),
		})
	}
	return out, nil
}

// stream runs a streamed chat completion, forwarding content deltas to sink and
// assembling fragmented tool-call deltas (keyed by index) into the final result.
func (c *chatClient) stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	body, err := c.body(req, true)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	resp, err := httpStream(ctx, c.http, c.baseURL+"/chat/completions", c.headers(), body, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.label, err)
	}
	defer resp.Body.Close()

	type partialCall struct {
		id, name string
		args     strings.Builder
	}
	byIndex := map[int]*partialCall{}
	var order []int
	var usage Usage
	var text strings.Builder
	var reasoning strings.Builder

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[len("data:"):])
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev struct {
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *chatUsage `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.Error != nil {
			return nil, fmt.Errorf("%s error: %s", c.label, ev.Error.Message)
		}
		if ev.Usage != nil {
			usage = Usage{InputTokens: ev.Usage.PromptTokens, OutputTokens: ev.Usage.CompletionTokens}
		}
		if len(ev.Choices) == 0 {
			continue
		}
		d := ev.Choices[0].Delta
		if d.Content != "" {
			text.WriteString(d.Content)
			if sink != nil {
				sink(StreamChunk{Kind: ChunkText, Text: d.Content})
			}
		}
		// Some providers (e.g. GLM) stream a separate reasoning channel.
		if d.ReasoningContent != "" {
			reasoning.WriteString(d.ReasoningContent)
			if sink != nil {
				sink(StreamChunk{Kind: ChunkReasoning, Text: d.ReasoningContent})
			}
		}
		for _, tc := range d.ToolCalls {
			p := byIndex[tc.Index]
			if p == nil {
				p = &partialCall{}
				byIndex[tc.Index] = p
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				p.id = tc.ID
			}
			if tc.Function.Name != "" {
				p.name = tc.Function.Name
			}
			p.args.WriteString(tc.Function.Arguments)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}

	out := &Response{Text: text.String(), Reasoning: reasoning.String(), Usage: usage}
	for _, idx := range order {
		p := byIndex[idx]
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        p.id,
			Name:      p.name,
			Arguments: normalizeArgs(p.args.String()),
		})
	}
	return out, nil
}

// chatMessagesFrom maps eigen's neutral transcript to chat-completions messages.
func chatMessagesFrom(req Request) []chatMessage {
	msgs := make([]chatMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.System})
	}
	// chat-completions tool messages are text-only — tool-result images are
	// buffered and flushed as ONE synthetic user message after the contiguous
	// tool run (preserving tool_calls → tool-result ordering).
	var toolImgs []Image
	for _, m := range req.Messages {
		if m.Role != RoleTool {
			flushToolImgsChat(&msgs, &toolImgs)
		}
		switch m.Role {
		case RoleTool:
			msgs = append(msgs, chatMessage{Role: "tool", ToolCallID: m.ToolCallID, Content: m.Text})
			toolImgs = append(toolImgs, m.Images...)
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
			if len(m.Images) > 0 {
				// Vision: typed part array — text + image_url data URLs (the
				// chat-completions multimodal format grok/glm follow).
				parts := make([]chatPart, 0, len(m.Images)+1)
				if m.Text != "" {
					parts = append(parts, chatPart{Type: "text", Text: m.Text})
				}
				for _, img := range m.Images {
					parts = append(parts, chatPart{Type: "image_url", ImageURL: &chatImageURL{
						URL: "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
					}})
				}
				msgs = append(msgs, chatMessage{Role: string(m.Role), Content: parts})
				continue
			}
			msgs = append(msgs, chatMessage{Role: string(m.Role), Content: m.Text})
		}
	}
	flushToolImgsChat(&msgs, &toolImgs) // transcript ended on a tool-result run
	return msgs
}

// flushToolImgsChat emits buffered tool-result images as one synthetic user
// message (chat-completions tool messages can't hold images), then clears the
// buffer. Provenance text tells the model these are tool output.
func flushToolImgsChat(msgs *[]chatMessage, toolImgs *[]Image) {
	if len(*toolImgs) == 0 {
		return
	}
	parts := []chatPart{{Type: "text", Text: "Image(s) returned by the preceding tool call(s)."}}
	for _, img := range *toolImgs {
		parts = append(parts, chatPart{Type: "image_url", ImageURL: &chatImageURL{
			URL: "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
		}})
	}
	*msgs = append(*msgs, chatMessage{Role: "user", Content: parts})
	*toolImgs = nil
}

// chatToolsFrom wraps neutral tool specs in the function-tool shape.
func chatToolsFrom(specs []ToolSpec) []chatTool {
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

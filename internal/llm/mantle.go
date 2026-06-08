package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// mantleDefaultRegion is where Bedrock serves the OpenAI-family models today.
// GPT-5.5 is us-east-2 in-region only, so we default here rather than reusing
// AWS_REGION (which may point at us-east-1 for the Converse/Claude path).
const mantleDefaultRegion = "us-east-2"

// Mantle drives an OpenAI-family model on the Bedrock "mantle" endpoint via the
// OpenAI Responses API. Auth is a Bedrock long-term API key (Bearer token).
type Mantle struct {
	BaseURL string
	Model   string
	token   string
	http    *http.Client
}

// NewMantle builds a Mantle provider from the environment.
// Requires AWS_BEARER_TOKEN_BEDROCK; region defaults to us-east-2.
func NewMantle(model string) (*Mantle, error) {
	token := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	if token == "" {
		return nil, fmt.Errorf("AWS_BEARER_TOKEN_BEDROCK not set")
	}
	region := os.Getenv("EIGEN_MANTLE_REGION")
	if region == "" {
		region = mantleDefaultRegion
	}
	if model == "" {
		model = "openai.gpt-5.5"
	}
	return &Mantle{
		BaseURL: fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1", region),
		Model:   model,
		token:   token,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (m *Mantle) Name() string { return m.Model + " (bedrock mantle)" }

// responsesInputItem is one entry in the Responses API "input" array. The shape
// is heterogeneous: plain messages carry role+content, while tool turns carry
// type plus call_id/name/arguments or output. omitempty keeps each item valid.
type responsesInputItem struct {
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// responsesTool is a Responses API function-tool definition (flat, type=function).
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type responsesRequest struct {
	Model string               `json:"model"`
	Input []responsesInputItem `json:"input"`
	Tools []responsesTool      `json:"tools,omitempty"`
}

type responsesReply struct {
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (m *Mantle) Complete(ctx context.Context, req Request) (*Response, error) {
	payload := responsesRequest{
		Model: m.Model,
		Input: buildInput(req),
		Tools: toResponsesTools(req.Tools),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mantle request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("mantle HTTP %d: %s", resp.StatusCode, string(raw))
	}

	var reply responsesReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if reply.Error != nil {
		return nil, fmt.Errorf("mantle error: %s", reply.Error.Message)
	}

	out := &Response{}
	for _, item := range reply.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					out.Text += part.Text
				}
			}
		case "function_call":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: json.RawMessage(item.Arguments),
			})
		}
	}
	return out, nil
}

// buildInput serializes the normalized transcript into Responses input items.
func buildInput(req Request) []responsesInputItem {
	items := make([]responsesInputItem, 0, len(req.Messages)+1)
	if req.System != "" {
		items = append(items, responsesInputItem{Role: "developer", Content: req.System})
	}
	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleTool:
			items = append(items, responsesInputItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: msg.Text,
			})
		case RoleAssistant:
			for _, tc := range msg.ToolCalls {
				items = append(items, responsesInputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: string(tc.Arguments),
				})
			}
			if len(msg.ToolCalls) == 0 && msg.Text != "" {
				items = append(items, responsesInputItem{Role: "assistant", Content: msg.Text})
			}
		default: // system / user
			role := string(msg.Role)
			if msg.Role == RoleSystem {
				role = "developer"
			}
			items = append(items, responsesInputItem{Role: role, Content: msg.Text})
		}
	}
	return items
}

func toResponsesTools(specs []ToolSpec) []responsesTool {
	if len(specs) == 0 {
		return nil
	}
	tools := make([]responsesTool, 0, len(specs))
	for _, s := range specs {
		tools = append(tools, responsesTool{
			Type:        "function",
			Name:        s.Name,
			Description: s.Description,
			Parameters:  s.Parameters,
		})
	}
	return tools
}

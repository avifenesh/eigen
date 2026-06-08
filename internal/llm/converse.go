package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// converseMaxTokens caps output; high enough for substantial file writes.
const converseMaxTokens = 16384

// Converse drives Anthropic Claude (and other Converse-capable models) on the
// Bedrock Runtime Converse API, authenticated with SigV4 from an AWS profile.
// Its wire format is content blocks (text / toolUse / toolResult), distinct
// from both mantle's Responses items and llama's chat messages.
type Converse struct {
	Model  string
	region string
	creds  awsCreds
	http   *http.Client
}

// NewConverse builds a Converse provider. Region defaults to us-east-2, profile
// to "aviary" (override with EIGEN_CONVERSE_REGION/PROFILE or AWS_REGION/PROFILE).
func NewConverse(model string) (*Converse, error) {
	region := firstNonEmpty(os.Getenv("EIGEN_CONVERSE_REGION"), os.Getenv("AWS_REGION"), "us-east-2")
	profile := firstNonEmpty(os.Getenv("EIGEN_CONVERSE_PROFILE"), os.Getenv("AWS_PROFILE"), "aviary")
	if model == "" {
		model = "us.anthropic.claude-sonnet-4-6"
	}
	creds, err := loadAWSCreds(profile)
	if err != nil {
		return nil, fmt.Errorf("converse credentials: %w", err)
	}
	return &Converse{
		Model:  model,
		region: region,
		creds:  creds,
		http:   &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (c *Converse) Name() string { return c.Model + " (bedrock converse)" }

type converseContent struct {
	Text       string              `json:"text,omitempty"`
	ToolUse    *converseToolUse    `json:"toolUse,omitempty"`
	ToolResult *converseToolResult `json:"toolResult,omitempty"`
}

type converseToolUse struct {
	ToolUseID string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

type converseToolResult struct {
	ToolUseID string                   `json:"toolUseId"`
	Content   []converseToolResultText `json:"content"`
	Status    string                   `json:"status"`
}

type converseToolResultText struct {
	Text string `json:"text"`
}

type converseMessage struct {
	Role    string            `json:"role"`
	Content []converseContent `json:"content"`
}

type converseToolSpec struct {
	ToolSpec struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema struct {
			JSON json.RawMessage `json:"json"`
		} `json:"inputSchema"`
	} `json:"toolSpec"`
}

type converseRequest struct {
	Messages        []converseMessage        `json:"messages"`
	System          []converseToolResultText `json:"system,omitempty"`
	ToolConfig      *converseToolConfig      `json:"toolConfig,omitempty"`
	InferenceConfig *converseInference       `json:"inferenceConfig,omitempty"`
}

type converseToolConfig struct {
	Tools []converseToolSpec `json:"tools"`
}

type converseInference struct {
	MaxTokens int `json:"maxTokens,omitempty"`
}

type converseReply struct {
	Output struct {
		Message struct {
			Content []struct {
				Text    string `json:"text"`
				ToolUse *struct {
					ToolUseID string          `json:"toolUseId"`
					Name      string          `json:"name"`
					Input     json.RawMessage `json:"input"`
				} `json:"toolUse"`
			} `json:"content"`
		} `json:"message"`
	} `json:"output"`
	StopReason string `json:"stopReason"`
	Message    string `json:"message"` // error message on failure
}

func (c *Converse) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	payload := converseRequest{
		Messages:        converseMessages(req),
		InferenceConfig: &converseInference{MaxTokens: converseMaxTokens},
	}
	if req.System != "" {
		payload.System = []converseToolResultText{{Text: req.System}}
	}
	if tools := converseTools(req.Tools); len(tools) > 0 {
		payload.ToolConfig = &converseToolConfig{Tools: tools}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/converse", c.region, c.Model)
	sign := func(r *http.Request, b []byte) {
		signV4(r, b, c.creds, "bedrock", c.region, time.Now())
	}
	raw, status, err := httpJSON(ctx, c.http, url, nil, body, sign)
	if err != nil {
		return nil, fmt.Errorf("converse: %w", err)
	}

	var reply converseReply
	if jerr := json.Unmarshal(raw, &reply); jerr != nil {
		return nil, fmt.Errorf("decode response: %w", jerr)
	}
	if status < 200 || status >= 300 {
		if reply.Message != "" {
			return nil, fmt.Errorf("converse HTTP %d: %s", status, reply.Message)
		}
		return nil, fmt.Errorf("converse HTTP %d: %s", status, string(raw))
	}
	// Refuse truncated output rather than applying it (parity with mantle).
	if reply.StopReason == "max_tokens" {
		return nil, fmt.Errorf("converse response truncated (max_tokens): refusing possibly-truncated output")
	}

	out := &Response{}
	for _, blk := range reply.Output.Message.Content {
		if blk.Text != "" {
			out.Text += blk.Text
		}
		if blk.ToolUse != nil {
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        blk.ToolUse.ToolUseID,
				Name:      blk.ToolUse.Name,
				Arguments: normalizeArgsRaw(blk.ToolUse.Input),
			})
		}
	}
	return out, nil
}

// converseMessages maps the neutral transcript to Converse content blocks.
// Critically, Converse expects strict user/assistant alternation with tool
// results delivered as a user message of toolResult blocks, so consecutive
// RoleTool messages are grouped into a single user turn.
func converseMessages(req Request) []converseMessage {
	var out []converseMessage
	var pendingResults []converseContent
	flush := func() {
		if len(pendingResults) > 0 {
			out = append(out, converseMessage{Role: "user", Content: pendingResults})
			pendingResults = nil
		}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case RoleTool:
			status := "success"
			if m.ToolError {
				status = "error"
			}
			pendingResults = append(pendingResults, converseContent{ToolResult: &converseToolResult{
				ToolUseID: m.ToolCallID,
				Content:   []converseToolResultText{{Text: m.Text}},
				Status:    status,
			}})
		case RoleUser:
			flush()
			out = append(out, converseMessage{Role: "user", Content: []converseContent{{Text: m.Text}}})
		case RoleAssistant:
			flush()
			var content []converseContent
			if m.Text != "" {
				content = append(content, converseContent{Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				content = append(content, converseContent{ToolUse: &converseToolUse{
					ToolUseID: tc.ID,
					Name:      tc.Name,
					Input:     normalizeArgsRaw(tc.Arguments),
				}})
			}
			out = append(out, converseMessage{Role: "assistant", Content: content})
		}
	}
	flush()
	return out
}

func converseTools(specs []ToolSpec) []converseToolSpec {
	if len(specs) == 0 {
		return nil
	}
	tools := make([]converseToolSpec, 0, len(specs))
	for _, s := range specs {
		var t converseToolSpec
		t.ToolSpec.Name = s.Name
		t.ToolSpec.Description = s.Description
		t.ToolSpec.InputSchema.JSON = s.Parameters
		tools = append(tools, t)
	}
	return tools
}

// normalizeArgsRaw ensures a tool input/argument object is always valid JSON.
func normalizeArgsRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

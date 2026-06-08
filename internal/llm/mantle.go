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

// mantleDefaultRegion is where Bedrock serves the OpenAI-family models today.
// GPT-5.5 is us-east-2 in-region only, so we default here rather than reusing
// AWS_REGION (which may point at us-east-1 for the Converse/Claude path).
const mantleDefaultRegion = "us-east-2"

// Mantle drives an OpenAI-family model on the Bedrock "mantle" endpoint via the
// OpenAI Responses API. Auth is a Bedrock long-term API key (Bearer token).
type Mantle struct {
	BaseURL string
	Model   string
	effort  string
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
	effort := os.Getenv("EIGEN_REASONING_EFFORT")
	if effort == "" {
		effort = reasoningEffort
	}
	return &Mantle{
		BaseURL: fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1", region),
		Model:   model,
		effort:  effort,
		token:   token,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (m *Mantle) Name() string { return m.Model + " (bedrock mantle)" }

// Reasoning configuration for GPT-5.5 on mantle.
//
// Effort is intentionally "high", not "xhigh": GPT-5.5 with xhigh stalls
// mid-task on Bedrock mantle (Codex bedrock #26860), while high is stable. Step
// back up toward xhigh only once that is confirmed fixed.
//
// Summary "concise" is requested so the reasoning trace can be carried across
// turns to preserve the chain of thought (mitigates #26195). NOTE: as of
// 2026-06, Bedrock mantle accepts the field but returns an empty summary for
// GPT-5.5 (verified for concise/detailed/auto), so the carry-forward is
// currently inert on mantle — it activates for providers that do return
// summaries. The real mantle mechanism for reasoning continuity is
// previous_response_id with store=true; defer until #26195 actually reproduces.
//
// reasoningEffort is the default; override per run with EIGEN_REASONING_EFFORT
// (e.g. medium if high still stalls, or xhigh once #26860 is fixed upstream).
const (
	reasoningEffort  = "high"
	reasoningSummary = "concise"
)

// responsesInputItem is one entry in the Responses API "input" array. The shape
// is heterogeneous: plain messages carry role+content, tool turns carry type
// plus call_id/name/arguments or output, and reasoning turns carry a summary.
// omitempty keeps each item valid.
type responsesInputItem struct {
	Type      string        `json:"type,omitempty"`
	Role      string        `json:"role,omitempty"`
	Content   string        `json:"content,omitempty"`
	CallID    string        `json:"call_id,omitempty"`
	Name      string        `json:"name,omitempty"`
	Arguments string        `json:"arguments,omitempty"`
	Output    string        `json:"output,omitempty"`
	ID        string        `json:"id,omitempty"`
	Summary   []summaryPart `json:"summary,omitempty"`
}

type summaryPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type reasoningConfig struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// responsesTool is a Responses API function-tool definition (flat, type=function).
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type responsesRequest struct {
	Model     string               `json:"model"`
	Input     []responsesInputItem `json:"input"`
	Tools     []responsesTool      `json:"tools,omitempty"`
	Reasoning *reasoningConfig     `json:"reasoning,omitempty"`
}

type responsesReply struct {
	Status            string `json:"status"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Output []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		ID      string `json:"id"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Summary []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"output"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// maxEmptyRetries bounds re-requests when Bedrock Mantle returns a completed
// response with no message and no tool call — a known intermittent quirk (the
// reason the standalone mantle proxy retried empty completions).
const maxEmptyRetries = 2

func (m *Mantle) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	payload := responsesRequest{
		Model:     m.Model,
		Input:     buildInput(req),
		Tools:     toResponsesTools(req.Tools),
		Reasoning: &reasoningConfig{Effort: m.effort, Summary: reasoningSummary},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	for attempt := 0; ; attempt++ {
		raw, err := m.post(ctx, body)
		if err != nil {
			return nil, err
		}
		out, status, reason, err := parseReply(raw)
		if err != nil {
			return nil, err
		}
		// Refuse possibly-truncated output rather than applying it (a truncated
		// write/edit is worse than a loud failure). See Codex bedrock #26297.
		if status == "incomplete" {
			if reason == "" {
				reason = "unknown"
			}
			return nil, fmt.Errorf("mantle response incomplete (%s): refusing possibly-truncated output", reason)
		}
		if out.Text != "" || len(out.ToolCalls) > 0 || attempt >= maxEmptyRetries {
			return out, nil
		}
		// Empty completed response: re-request (the mantle quirk).
	}
}

// post sends the request body to the Responses endpoint via the shared
// transport (retry/backoff/Retry-After) and surfaces non-2xx as an error.
func (m *Mantle) post(ctx context.Context, body []byte) ([]byte, error) {
	raw, status, err := httpJSON(ctx, m.http, m.BaseURL+"/responses",
		map[string]string{"Authorization": "Bearer " + m.token}, body, nil)
	if err != nil {
		return nil, fmt.Errorf("mantle: %w", err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("mantle HTTP %d: %s", status, string(raw))
	}
	return raw, nil
}

// parseReply decodes a Responses API body into a normalized Response, returning
// the response status and any incomplete reason for the caller to act on.
func parseReply(raw []byte) (*Response, string, string, error) {
	var reply responsesReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, "", "", fmt.Errorf("decode response: %w", err)
	}
	if reply.Error != nil {
		return nil, "", "", fmt.Errorf("mantle error: %s", reply.Error.Message)
	}
	reason := ""
	if reply.IncompleteDetails != nil {
		reason = reply.IncompleteDetails.Reason
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
		case "reasoning":
			for _, s := range item.Summary {
				if s.Type == "summary_text" {
					out.Reasoning += s.Text
				}
			}
			if out.ReasoningID == "" {
				out.ReasoningID = item.ID
			}
		case "function_call":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: normalizeArgs(item.Arguments),
			})
		}
	}
	return out, reply.Status, reason, nil
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
			if msg.Reasoning != "" {
				items = append(items, responsesInputItem{
					Type:    "reasoning",
					ID:      msg.ReasoningID,
					Summary: []summaryPart{{Type: "summary_text", Text: msg.Reasoning}},
				})
			}
			if msg.Text != "" {
				items = append(items, responsesInputItem{Role: "assistant", Content: msg.Text})
			}
			for _, tc := range msg.ToolCalls {
				items = append(items, responsesInputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: argString(tc.Arguments),
				})
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

// argString renders tool-call arguments for the wire, defaulting empty/nil to
// an empty JSON object so the Responses API always receives valid JSON.
func argString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

// normalizeArgs ensures parsed tool-call arguments are always valid JSON.
func normalizeArgs(s string) json.RawMessage {
	if strings.TrimSpace(s) == "" {
		return json.RawMessage("{}")
	}
	return json.RawMessage(s)
}

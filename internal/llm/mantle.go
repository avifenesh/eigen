package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
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
	payload := responsesRequest{
		Model:     m.Model,
		Input:     buildInput(req),
		Tools:     toResponsesTools(req.Tools),
		Reasoning: &reasoningConfig{Effort: reasoningEffort, Summary: reasoningSummary},
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

// maxAttempts bounds retries for transient provider failures.
const maxAttempts = 4

// maxResponseBytes caps how much of a response body we read into memory.
const maxResponseBytes = 16 << 20 // 16 MiB

// post sends the request body to the Responses endpoint, retrying transient
// failures (network errors, HTTP 429, and 5xx) with jittered exponential
// backoff that honors a server Retry-After. It returns the response body on
// success, or the last error after maxAttempts.
func (m *Mantle) post(ctx context.Context, body []byte) ([]byte, error) {
	var lastErr error
	var retryAfter time.Duration
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt, retryAfter); err != nil {
				return nil, err
			}
			retryAfter = 0
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.BaseURL+"/responses", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+m.token)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("User-Agent", "eigen/0.1.0")

		resp, err := m.http.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("mantle request: %w", err)
			continue // network error: retry
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return raw, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("mantle HTTP %d: %s", resp.StatusCode, string(raw))
			continue // transient: retry
		}
		// 4xx (other than 429): not retryable.
		return nil, fmt.Errorf("mantle HTTP %d: %s", resp.StatusCode, string(raw))
	}
	return nil, fmt.Errorf("mantle failed after %d attempts: %w", maxAttempts, lastErr)
}

// sleepBackoff waits for an exponential backoff (honoring a server Retry-After
// when larger) plus jitter, or returns early if the context is cancelled.
func sleepBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := time.Duration(1<<(attempt-1)) * time.Second
	if retryAfter > delay {
		delay = retryAfter
	}
	delay += time.Duration(rand.Int63n(int64(500 * time.Millisecond)))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// parseRetryAfter parses a Retry-After header expressed in whole seconds.
func parseRetryAfter(h string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
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

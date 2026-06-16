package llm

import (
	"bufio"
	"context"
	"encoding/base64"
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
	// Per-model default from the catalog, falling back to the package default.
	effort := reasoningEffort
	if info, ok := Lookup(model); ok && info.Effort != "" {
		effort = info.Effort
	}
	// An explicit EIGEN_REASONING_EFFORT applies ONLY if this model supports it
	// (e.g. "max" is Anthropic-only — GPT caps at xhigh and 400s on max). An
	// unsupported value is ignored, keeping the model's valid default, so a
	// global effort setting never breaks a cross-model/cross-vendor call.
	if env := strings.TrimSpace(os.Getenv("EIGEN_REASONING_EFFORT")); env != "" {
		if levels := ModelEffortLevels(model); effortSupported(env, levels) {
			effort = env
		}
	}
	return &Mantle{
		BaseURL: fmt.Sprintf("https://bedrock-mantle.%s.api.aws/openai/v1", region),
		Model:   model,
		effort:  effort,
		token:   token,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

func (m *Mantle) Name() string    { return m.Model + " (bedrock mantle)" }
func (m *Mantle) ModelID() string { return m.Model }

// SetEffort changes the reasoning effort for subsequent requests. Returns false
// for an unrecognized level (validated against the per-model set when known).
func (m *Mantle) SetEffort(level string) bool {
	levels := ModelEffortLevels(m.Model)
	if len(levels) == 0 {
		levels = EffortLevels
	}
	for _, l := range levels {
		if l == level {
			m.effort = level
			return true
		}
	}
	return false
}

// Effort returns the current reasoning effort.
func (m *Mantle) Effort() string { return m.effort }

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
	Type      string          `json:"type,omitempty"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Output    string          `json:"output,omitempty"`
	ID        string          `json:"id,omitempty"`
	Summary   []summaryPart   `json:"summary,omitempty"`
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
	Model        string               `json:"model"`
	Instructions string               `json:"instructions,omitempty"`
	Input        []responsesInputItem `json:"input"`
	Tools        []responsesTool      `json:"tools,omitempty"`
	Reasoning    *reasoningConfig     `json:"reasoning,omitempty"`
	Stream       bool                 `json:"stream,omitempty"`
	// ServiceTier selects the throughput/latency tier on backends that support
	// it (Codex: "priority" = fast/low-latency, "flex" = cheap/slow). Empty =
	// the backend default. Mantle leaves this empty.
	ServiceTier string `json:"service_tier,omitempty"`
	// Store, when non-nil, sets the Responses API `store` flag. The Codex
	// backend REQUIRES store:false (it manages its own thread state and rejects
	// store:true / a missing store with "Store must be set to false"). A pointer
	// so mantle (which omits it) and codex (explicit false) differ cleanly.
	Store *bool `json:"store,omitempty"`
	// Include lists extra response data to return. Codex sends
	// ["reasoning.encrypted_content"] so the model's reasoning carries across
	// turns (reason-then-act spans multiple Responses turns). Mantle omits it.
	Include []string `json:"include,omitempty"`
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
	Usage struct {
		InputTokens        int `json:"input_tokens"`
		OutputTokens       int `json:"output_tokens"`
		InputTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
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
	if dbg := os.Getenv("EIGEN_DEBUG_REQUEST"); dbg != "" {
		_ = os.WriteFile(dbg, body, 0o600)
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

// Stream runs a completion over SSE, forwarding text/reasoning deltas to sink
// and returning the final assembled Response parsed from the completed event.
//
// Bedrock mantle intermittently emits a "response.failed" event carrying a
// transient server error ("The server had an error while processing your
// request") on an otherwise-HTTP-200 stream. When that arrives BEFORE any
// output has been streamed to the sink, the attempt is safe to retry (no
// partial output was shown), mirroring how a non-streaming 5xx is retried.
// A failure that arrives after deltas were emitted is surfaced (retrying would
// duplicate streamed text).
func (m *Mantle) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	payload := responsesRequest{
		Model:     m.Model,
		Input:     buildInput(req),
		Tools:     toResponsesTools(req.Tools),
		Reasoning: &reasoningConfig{Effort: m.effort, Summary: reasoningSummary},
		Stream:    true,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if dbg := os.Getenv("EIGEN_DEBUG_REQUEST"); dbg != "" {
		_ = os.WriteFile(dbg, body, 0o600)
	}

	for attempt := 0; ; attempt++ {
		final, emitted, failErr, err := m.streamOnce(ctx, body, sink)
		if err != nil {
			return nil, err
		}
		if failErr != nil {
			// A pre-output failure is the transient mantle quirk: retry with
			// backoff (capped). Once any delta has been emitted, or retries are
			// exhausted, surface it.
			if !emitted && attempt < maxStreamFailRetries {
				if berr := sleepBackoff(ctx, attempt+1, 0); berr != nil {
					return nil, berr
				}
				continue
			}
			return nil, failErr
		}
		return final, nil
	}
}

// maxStreamFailRetries bounds re-requests when a streamed completion reports a
// transient "response.failed" with NO recoverable output. mantle's gpt-5.5
// fails this way often (codex#27185), and a fresh request usually succeeds, so
// the budget is generous; each retry backs off.
const maxStreamFailRetries = 6

// streamOnce performs a single SSE attempt. It returns the assembled Response
// on success; emitted reports whether any text/reasoning delta was forwarded to
// sink (so the caller can decide whether a retry is safe); failErr is non-nil
// when the stream reported "response.failed".
func (m *Mantle) streamOnce(ctx context.Context, body []byte, sink StreamSink) (final *Response, emitted bool, failErr error, err error) {
	resp, err := httpStream(ctx, m.http, m.BaseURL+"/responses",
		map[string]string{"Authorization": "Bearer " + m.token}, body, nil)
	if err != nil {
		return nil, false, nil, fmt.Errorf("mantle: %w", err)
	}
	defer resp.Body.Close()

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
			Type     string          `json:"type"`
			Delta    string          `json:"delta"`
			Response json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			emitted = true
			if sink != nil {
				sink(StreamChunk{Kind: ChunkText, Text: ev.Delta})
			}
		case "response.reasoning_summary_text.delta":
			emitted = true
			if sink != nil {
				sink(StreamChunk{Kind: ChunkReasoning, Text: ev.Delta})
			}
		case "response.completed", "response.incomplete":
			out, status, reason, perr := parseReply(ev.Response)
			if perr != nil {
				return nil, emitted, nil, perr
			}
			if status == "incomplete" {
				if reason == "" {
					reason = "unknown"
				}
				return nil, emitted, nil, fmt.Errorf("mantle response incomplete (%s): refusing possibly-truncated output", reason)
			}
			final = out
		case "response.failed":
			if dbg := os.Getenv("EIGEN_DEBUG_STREAM"); dbg != "" {
				_ = os.WriteFile(dbg, []byte(data), 0o600)
			}
			// mantle gpt-5.5 quirk (codex#27185): it streams a COMPLETE reply,
			// then tags the turn response.failed with a spurious server_error —
			// but the finished output is right there in the event. Recover it
			// instead of failing/retrying: if the failed response already
			// carries text or tool calls, use it as the answer. (parseReply
			// rejects on the error field, so extract output directly here.)
			if out := outputFromFailed(ev.Response); out != nil &&
				(strings.TrimSpace(out.Text) != "" || len(out.ToolCalls) > 0) {
				return out, true, nil, nil
			}
			// Otherwise it failed with no usable output → a real transient: let
			// the caller retry.
			return nil, emitted, fmt.Errorf("mantle stream failed: %s", streamFailReason(ev.Response)), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, emitted, nil, fmt.Errorf("read stream: %w", err)
	}
	if final == nil {
		return &Response{}, emitted, nil, nil // empty; the agent's empty-turn nudge handles it
	}
	return final, emitted, nil, nil
}

// outputFromFailed extracts any completed output (text + tool calls) from a
// response.failed event's response object, IGNORING its error field — mantle
// gpt-5.5 often streams a full reply then flags the turn failed (codex#27185).
// Returns nil when there's nothing usable.
func outputFromFailed(raw json.RawMessage) *Response {
	var reply responsesReply
	if json.Unmarshal(raw, &reply) != nil {
		return nil
	}
	out := &Response{Usage: mantleUsage(reply.Usage.InputTokens, reply.Usage.OutputTokens, reply.Usage.InputTokensDetails.CachedTokens)}
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
				Arguments: normalizeArgs(item.Arguments),
			})
		}
	}
	return out
}

// streamFailReason extracts a concise error message from a failed response
// payload, falling back to the raw JSON when no structured error is present.
func streamFailReason(raw json.RawMessage) string {
	var r struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &r); err == nil && r.Error != nil {
		if r.Error.Code != "" {
			return r.Error.Code + ": " + r.Error.Message
		}
		return r.Error.Message
	}
	return string(raw)
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

	out := &Response{Usage: mantleUsage(reply.Usage.InputTokens, reply.Usage.OutputTokens, reply.Usage.InputTokensDetails.CachedTokens)}
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
		items = append(items, responsesInputItem{Role: "developer", Content: jsonStr(req.System)})
	}
	// The Responses API function_call_output is text-only — it can't carry an
	// image. So tool-result images are buffered and emitted as ONE synthetic
	// user message AFTER the contiguous run of tool outputs (preserving the
	// strict function_call → function_call_output ordering), with provenance so
	// the model knows they're tool output, not a fresh user upload.
	var toolImgs []Image
	var toolImgNote string
	flushToolImgs := func() {
		if len(toolImgs) == 0 {
			return
		}
		items = append(items, responsesInputItem{
			Role:    "user",
			Content: inputParts(Message{Text: toolImgNote, Images: toolImgs}),
		})
		toolImgs = nil
		toolImgNote = ""
	}
	for _, msg := range req.Messages {
		if msg.Role != RoleTool {
			flushToolImgs() // a non-tool message ends the tool-result run
		}
		switch msg.Role {
		case RoleTool:
			out := msg.Text
			if out == "" {
				out = "(no output)" // output is required; omitempty would drop ""
			}
			items = append(items, responsesInputItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: out,
			})
			if len(msg.Images) > 0 {
				toolImgs = append(toolImgs, msg.Images...)
				toolImgNote = "Image(s) returned by the preceding tool call(s)."
			}
		case RoleAssistant:
			if msg.Reasoning != "" {
				items = append(items, responsesInputItem{
					Type:    "reasoning",
					ID:      msg.ReasoningID,
					Summary: []summaryPart{{Type: "summary_text", Text: msg.Reasoning}},
				})
			}
			if msg.Text != "" {
				// Assistant history must use typed output_text content (a plain
				// string is rejected as input by the Responses API).
				items = append(items, responsesInputItem{Type: "message", Role: "assistant", Content: outputText(msg.Text)})
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
			if len(msg.Images) > 0 {
				// Vision: typed content parts — input_text + input_image blocks
				// (Responses API; images ride as data URLs).
				items = append(items, responsesInputItem{Role: role, Content: inputParts(msg)})
				continue
			}
			items = append(items, responsesInputItem{Role: role, Content: jsonStr(msg.Text)})
		}
	}
	flushToolImgs() // transcript ended on a tool-result run
	return items
}

// inputParts builds typed user content: input_text plus input_image blocks
// carrying base64 data URLs (the Responses API vision format).
func inputParts(msg Message) json.RawMessage {
	parts := make([]map[string]string, 0, len(msg.Images)+1)
	if msg.Text != "" {
		parts = append(parts, map[string]string{"type": "input_text", "text": msg.Text})
	}
	for _, img := range msg.Images {
		parts = append(parts, map[string]string{
			"type":      "input_image",
			"image_url": "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
		})
	}
	b, _ := json.Marshal(parts)
	return b
}

// jsonStr encodes s as a JSON string value for a message's content field.
func jsonStr(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// outputText builds a Responses assistant-content array of one output_text block.
func outputText(s string) json.RawMessage {
	b, _ := json.Marshal([]map[string]string{{"type": "output_text", "text": s}})
	return b
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

// mantleUsage builds a neutral Usage from the Responses-API counts. cached is
// part of input (input_tokens_details.cached_tokens), so split it out:
// InputTokens is the fresh slice, CacheReadTokens the cache-hit slice.
func mantleUsage(input, output, cached int) Usage {
	in := input - cached
	if in < 0 {
		in = 0
	}
	return Usage{InputTokens: in, OutputTokens: output, CacheReadTokens: cached}
}

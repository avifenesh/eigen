package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// converseMaxTokens caps output; high enough for substantial file writes.
const converseMaxTokens = 16384

// context1mBeta is the Anthropic beta flag that unlocks the 1M-token context
// window on Bedrock, passed through additionalModelRequestFields.anthropic_beta.
const context1mBeta = "context-1m-2025-08-07"

// Converse drives Anthropic Claude (and other Converse-capable models) on the
// Bedrock Runtime Converse API, authenticated with SigV4 from an AWS profile.
// Its wire format is content blocks (text / toolUse / toolResult), distinct
// from both mantle's Responses items and llama's chat messages.
type Converse struct {
	Model   string
	region  string
	profile string
	creds   awsCreds
	bearer  string // AWS_BEARER_TOKEN_BEDROCK — when set, auth via Bearer header (no SigV4)
	http    *http.Client

	// Capabilities resolved from the catalog (with env overrides), driving the
	// extra wire features: prompt caching, 1M-context beta, extended thinking.
	cache     bool
	context1M bool

	// adaptive selects the newer Anthropic thinking API
	// (thinking.type=adaptive + output_config.effort) used by opus-4-8+, vs the
	// older budget API (thinking.type=enabled + budget_tokens) used by sonnet-4-6.
	adaptive bool

	mu             sync.RWMutex
	thinkingBudget int    // 0 disables extended thinking (budget-style models)
	effort         string // reasoning-effort label
}

// effortBudget maps an effort label to an Anthropic extended-thinking token
// budget. "minimal" disables thinking; higher levels allocate more budget.
var effortBudget = map[string]int{
	"off":     0, // budget-style only: disables extended thinking
	"minimal": 0, // back-compat alias
	"low":     4096,
	"medium":  8192,
	"high":    16384,
	"xhigh":   32768, // accepted but no adaptive model uses it
}

// budgetToEffort is the inverse used to report the current effort label.
func budgetToEffort(budget int) string {
	if budget == 0 {
		return "off"
	}
	best, bestB := "", -1
	for label, b := range effortBudget {
		if b == 0 {
			continue // skip off/minimal when budget > 0
		}
		if b <= budget && b > bestB {
			best, bestB = label, b
		}
	}
	if best == "" {
		return "off"
	}
	return best
}

// NewConverse builds a Converse provider. Region defaults to us-east-2, profile
// to "aviary" (override with EIGEN_CONVERSE_REGION/PROFILE or AWS_REGION/PROFILE).
//
// Per-model capabilities come from the catalog; env vars override:
//   - EIGEN_CONVERSE_CACHE=0/1       prompt caching
//   - EIGEN_CONVERSE_1M=0/1          1M-context beta
//   - EIGEN_THINKING_BUDGET=<tokens> extended-thinking budget (0 disables)
func NewConverse(model string) (*Converse, error) {
	region := firstNonEmpty(os.Getenv("EIGEN_CONVERSE_REGION"), os.Getenv("AWS_REGION"), "us-east-2")
	profile := firstNonEmpty(os.Getenv("EIGEN_CONVERSE_PROFILE"), os.Getenv("AWS_PROFILE"), "aviary")
	if model == "" {
		model = "us.anthropic.claude-opus-4-8"
	}
	// Prefer the Bedrock bearer token (AWS_BEARER_TOKEN_BEDROCK): a single
	// credential that drives the converse endpoint via an Authorization: Bearer
	// header — no SigV4, no ~/.aws/credentials. This is what makes a remote
	// daemon work with just the token snapshot (no AWS file to copy). Fall back
	// to SigV4 from ~/.aws/credentials when no token is set.
	bearer := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	var creds awsCreds
	if bearer == "" {
		var err error
		creds, err = loadAWSCreds(profile)
		if err != nil {
			return nil, fmt.Errorf("converse credentials: %w (or set AWS_BEARER_TOKEN_BEDROCK)", err)
		}
	}
	c := &Converse{
		Model:   model,
		region:  region,
		profile: profile,
		creds:   creds,
		bearer:  bearer,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
	// Resolve capabilities from the catalog, then apply env overrides.
	if info, ok := Lookup(model); ok {
		c.cache = info.Cache
		c.context1M = info.Context1M
		c.thinkingBudget = info.ThinkingBudget
		// A catalog Effort label means this model uses the adaptive thinking API
		// (output_config.effort); a ThinkingBudget means the older budget API.
		if info.Effort != "" {
			c.adaptive = true
			c.effort = info.Effort
		}
	}
	c.cache = envBool("EIGEN_CONVERSE_CACHE", c.cache)
	c.context1M = envBool("EIGEN_CONVERSE_1M", c.context1M)
	c.thinkingBudget = envInt("EIGEN_THINKING_BUDGET", c.thinkingBudget)
	if e := strings.TrimSpace(os.Getenv("EIGEN_REASONING_EFFORT")); e != "" {
		c.SetEffort(e)
	}
	if c.effort == "" {
		c.effort = budgetToEffort(c.thinkingBudget)
	}
	return c, nil
}

func (c *Converse) Name() string    { return c.Model + " (bedrock converse)" }
func (c *Converse) ModelID() string { return c.Model }

// SetEffort changes the reasoning effort. For adaptive-thinking models it sets
// the effort level directly; for budget-style models it maps the level to a
// thinking-token budget. Validates against the per-model level set when known.
func (c *Converse) SetEffort(level string) bool {
	// Validate against the per-model level set; fall back to the global list
	// (so tests with no catalog entry still reject truly unknown levels).
	levels := ModelEffortLevels(c.Model)
	if len(levels) == 0 {
		levels = EffortLevels
	}
	valid := false
	for _, l := range levels {
		if l == level {
			valid = true
			break
		}
	}
	if !valid {
		return false
	}
	b, ok := effortBudget[level]
	c.mu.Lock()
	defer c.mu.Unlock()
	if !ok {
		// Adaptive effort (auto/low/medium/high): not in the budget map.
		// For adaptive models the effort string is sent directly; set budget=0.
		c.thinkingBudget = 0
		c.effort = level
		return true
	}
	c.thinkingBudget = b
	c.effort = level
	return true
}

// Effort returns the current reasoning-effort label.
func (c *Converse) Effort() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.effort
}

func (c *Converse) snapshotSettings() (context1M bool, thinkingBudget int, effort string, adaptive bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.context1M, c.thinkingBudget, c.effort, c.adaptive
}

type converseContent struct {
	Text       string              `json:"text,omitempty"`
	Image      *converseImage      `json:"image,omitempty"`
	ToolUse    *converseToolUse    `json:"toolUse,omitempty"`
	ToolResult *converseToolResult `json:"toolResult,omitempty"`
	CachePoint *converseCachePoint `json:"cachePoint,omitempty"`
}

// converseImage is a Bedrock Converse image block: a format plus raw bytes
// (the AWS JSON marshals []byte as base64, which is exactly the wire shape).
type converseImage struct {
	Format string              `json:"format"` // png | jpeg | gif | webp
	Source converseImageSource `json:"source"`
}

type converseImageSource struct {
	Bytes []byte `json:"bytes"`
}

// converseCachePoint marks a prompt-caching breakpoint: everything before it in
// the prompt is cached and reused across requests with the same prefix.
type converseCachePoint struct {
	Type string `json:"type"` // "default"
}

type converseToolUse struct {
	ToolUseID string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

type converseToolResult struct {
	ToolUseID string            `json:"toolUseId"`
	Content   []converseContent `json:"content"`
	Status    string            `json:"status"`
}

type converseMessage struct {
	Role    string            `json:"role"`
	Content []converseContent `json:"content"`
}

type converseToolConfig struct {
	Tools []converseToolEntry `json:"tools"`
}

// converseToolEntry is one entry in toolConfig.tools — either a tool spec or a
// cachePoint marking the end of the (stable) tool-definition prefix.
type converseToolEntry struct {
	ToolSpec   *converseToolSpecInner `json:"toolSpec,omitempty"`
	CachePoint *converseCachePoint    `json:"cachePoint,omitempty"`
}

type converseToolSpecInner struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema struct {
		JSON json.RawMessage `json:"json"`
	} `json:"inputSchema"`
}

type converseRequest struct {
	Messages                     []converseMessage   `json:"messages"`
	System                       []converseContent   `json:"system,omitempty"`
	ToolConfig                   *converseToolConfig `json:"toolConfig,omitempty"`
	InferenceConfig              *converseInference  `json:"inferenceConfig,omitempty"`
	AdditionalModelRequestFields json.RawMessage     `json:"additionalModelRequestFields,omitempty"`
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
	Usage      struct {
		InputTokens           int `json:"inputTokens"`
		OutputTokens          int `json:"outputTokens"`
		CacheReadInputTokens  int `json:"cacheReadInputTokens"`
		CacheWriteInputTokens int `json:"cacheWriteInputTokens"`
	} `json:"usage"`
	Message string `json:"message"` // error message on failure
}

func (c *Converse) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	// Extended thinking needs maxTokens > thinking budget; give the answer room
	// on top of the reasoning budget.
	context1M, thinkingBudget, effort, adaptive := c.snapshotSettings()
	maxTokens := converseMaxTokens
	if thinkingBudget > 0 && maxTokens <= thinkingBudget {
		maxTokens = thinkingBudget + converseMaxTokens
	}
	payload := converseRequest{
		Messages:        converseMessages(req),
		InferenceConfig: &converseInference{MaxTokens: maxTokens},
	}
	if req.System != "" {
		// Cache the (large, stable) system prompt prefix when caching is on.
		payload.System = []converseContent{{Text: req.System}}
		if c.cache {
			payload.System = append(payload.System, converseContent{CachePoint: &converseCachePoint{Type: "default"}})
		}
	}
	if tools := converseTools(req.Tools, c.cache); len(tools) > 0 {
		payload.ToolConfig = &converseToolConfig{Tools: tools}
	}
	if extra := additionalConverseFields(context1M, thinkingBudget, effort, adaptive); extra != nil {
		payload.AdditionalModelRequestFields = extra
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/converse", c.region, urlPathEscape(c.Model))
	var headers map[string]string
	var sign func(r *http.Request, b []byte)
	if c.bearer != "" {
		// Bearer-token auth: a single header, no SigV4, no ~/.aws/credentials.
		headers = map[string]string{"Authorization": "Bearer " + c.bearer}
	} else {
		// SigV4. Re-resolve credentials per request: the daemon is long-lived,
		// so an AWS profile's session token can rotate/expire while a session
		// stays open. Re-reading picks up refreshed creds without restarting
		// the daemon. Fall back to the creds loaded at construction.
		creds := c.creds
		if fresh, err := loadAWSCreds(c.profile); err == nil {
			creds = fresh
		}
		sign = func(r *http.Request, b []byte) {
			signV4(r, b, creds, "bedrock", c.region, time.Now())
		}
	}
	raw, status, err := httpJSON(ctx, c.http, url, headers, body, sign)
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

	out := &Response{Usage: Usage{InputTokens: reply.Usage.InputTokens, OutputTokens: reply.Usage.OutputTokens, CacheReadTokens: reply.Usage.CacheReadInputTokens, CacheWriteTokens: reply.Usage.CacheWriteInputTokens}}
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

// additionalFields builds the additionalModelRequestFields JSON carrying the
// 1M-context beta flag and the extended-thinking config, or nil when none
// apply. Adaptive-thinking models (opus-4-8+) use thinking.type=adaptive with
// output_config.effort; budget-style models (sonnet-4-6) use
// thinking.type=enabled with budget_tokens.
func (c *Converse) additionalFields() json.RawMessage {
	context1M, thinkingBudget, effort, adaptive := c.snapshotSettings()
	return additionalConverseFields(context1M, thinkingBudget, effort, adaptive)
}

func additionalConverseFields(context1M bool, thinkingBudget int, effort string, adaptive bool) json.RawMessage {
	extra := map[string]any{}
	if context1M {
		extra["anthropic_beta"] = []string{context1mBeta}
	}
	switch {
	case adaptive && effort != "" && effort != "minimal" && effort != "off":
		extra["thinking"] = map[string]any{"type": "adaptive"}
		extra["output_config"] = map[string]any{"effort": effort}
	case !adaptive && thinkingBudget > 0:
		extra["thinking"] = map[string]any{"type": "enabled", "budget_tokens": thinkingBudget}
	}
	if len(extra) == 0 {
		return nil
	}
	b, err := json.Marshal(extra)
	if err != nil {
		return nil
	}
	return b
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
			rc := []converseContent{{Text: m.Text}}
			// Bedrock toolResult content can hold image blocks alongside text —
			// screenshots from browser/computer-use tools ride here directly.
			for _, img := range m.Images {
				if f := converseImageFormat(img.MediaType); f != "" {
					rc = append(rc, converseContent{Image: &converseImage{
						Format: f,
						Source: converseImageSource{Bytes: img.Data},
					}})
				}
			}
			pendingResults = append(pendingResults, converseContent{ToolResult: &converseToolResult{
				ToolUseID: m.ToolCallID,
				Content:   rc,
				Status:    status,
			}})
		case RoleUser:
			flush()
			content := []converseContent{}
			if m.Text != "" {
				content = append(content, converseContent{Text: m.Text})
			}
			for _, img := range m.Images {
				if f := converseImageFormat(img.MediaType); f != "" {
					content = append(content, converseContent{Image: &converseImage{
						Format: f,
						Source: converseImageSource{Bytes: img.Data},
					}})
				}
			}
			if len(content) == 0 {
				content = append(content, converseContent{Text: ""})
			}
			out = append(out, converseMessage{Role: "user", Content: content})
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
			// Converse requires every message to carry at least one content
			// block (a null/empty content array is rejected with HTTP 400). A
			// reasoning-only assistant turn — which providers like GLM emit and
			// persist with empty Text and no tool calls — would otherwise produce
			// an empty array, so drop it: the dropped reasoning carries no state
			// the model needs to continue.
			if len(content) == 0 {
				continue
			}
			out = append(out, converseMessage{Role: "assistant", Content: content})
		}
	}
	flush()
	return out
}

// converseImageFormat maps an IANA media type to a Bedrock image format token,
// or "" if unsupported.
func converseImageFormat(mediaType string) string {
	switch mediaType {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	}
	return ""
}

// converseTools maps neutral tool specs to Converse tool entries. When caching
// is enabled, a cachePoint is appended after the (stable) tool definitions so
// the system+tools prefix is cached across turns.
func converseTools(specs []ToolSpec, cache bool) []converseToolEntry {
	if len(specs) == 0 {
		return nil
	}
	tools := make([]converseToolEntry, 0, len(specs)+1)
	for _, s := range specs {
		inner := &converseToolSpecInner{Name: s.Name, Description: s.Description}
		inner.InputSchema.JSON = s.Parameters
		tools = append(tools, converseToolEntry{ToolSpec: inner})
	}
	if cache {
		tools = append(tools, converseToolEntry{CachePoint: &converseCachePoint{Type: "default"}})
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

// envBool returns the boolean value of an env var (1/true/yes/on => true,
// 0/false/no/off => false), or def when unset or unrecognized.
func envBool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

// envInt returns the integer value of an env var, or def when unset/invalid.
func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// urlPathEscape escapes a Bedrock model id for use as a URL path segment.
// url.PathEscape leaves ':' unescaped (it is legal in a path), but Bedrock's
// SigV4 canonicalization for the model resource requires ':' encoded as %3A,
// so a versioned profile id like "...-v1:0" signs correctly.
func urlPathEscape(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
}

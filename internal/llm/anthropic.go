package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Anthropic is a provider for the native Anthropic Messages API
// (api.anthropic.com/v1/messages), distinct from Bedrock Converse. It can
// authenticate with an ANTHROPIC_API_KEY or — the point of this provider —
// reuse the Claude Code OAuth credentials in ~/.claude/.credentials.json, so a
// Claude Code (Pro/Max) subscription drives eigen against the same catalog
// models Claude Code itself uses.
//
// To be accepted with an OAuth token, requests must mirror Claude Code: the
// oauth beta flag, the Claude Code "spoof" as the first system block, and the
// adaptive-thinking + output_config.effort shape for effort models. These were
// verified live by capturing Claude Code's own /v1/messages traffic.
type Anthropic struct {
	Model string
	http  *http.Client

	apiKey    string // x-api-key auth (ANTHROPIC_API_KEY)
	oauthFile string // path to ~/.claude/.credentials.json (OAuth fallback)

	// Capabilities from the catalog.
	cache          bool
	context1M      bool
	thinkingBudget int
	effort         string
	adaptive       bool
}

const (
	anthropicURL     = "https://api.anthropic.com/v1/messages?beta=true"
	anthropicVersion = "2023-06-01"
	// claudeCodeSpoof must be the first system block for OAuth-token requests;
	// the API rejects OAuth access whose system prompt isn't Claude Code's.
	claudeCodeSpoof = "You are Claude Code, Anthropic's official CLI for Claude."
	anthropicMaxTok = 32000
)

// anthropicBeta is the beta-flag set Claude Code sends. oauth-2025-04-20 is
// mandatory for OAuth tokens; the rest unlock the features eigen uses (1M
// context, interleaved thinking, prompt caching, adaptive effort).
var anthropicBetas = []string{
	"oauth-2025-04-20",
	"claude-code-20250219",
	"interleaved-thinking-2025-05-14",
	"fine-grained-tool-streaming-2025-05-14",
}

// NewAnthropic builds the native Anthropic provider. Credentials resolve in
// order: ANTHROPIC_API_KEY (api-key auth), else the Claude Code OAuth token in
// ~/.claude/.credentials.json (EIGEN_CLAUDE_CREDENTIALS overrides the path).
func NewAnthropic(model string) (*Anthropic, error) {
	if model == "" {
		model = "claude-sonnet-4-5-20250929"
	}
	a := &Anthropic{
		Model: model,
		http:  &http.Client{Timeout: 5 * time.Minute},
	}
	a.apiKey = strings.TrimSpace(firstNonEmpty(os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("EIGEN_ANTHROPIC_API_KEY")))
	a.oauthFile = firstNonEmpty(os.Getenv("EIGEN_CLAUDE_CREDENTIALS"), claudeCredentialsPath())
	if a.apiKey == "" {
		if _, err := claudeOAuthToken(a.oauthFile); err != nil {
			return nil, fmt.Errorf("no Anthropic credentials: set ANTHROPIC_API_KEY or log in with Claude Code (%s): %w", a.oauthFile, err)
		}
	}

	if info, ok := Lookup(model); ok {
		a.cache = info.Cache
		a.context1M = info.Context1M
		a.thinkingBudget = info.ThinkingBudget
		if info.Effort != "" {
			a.adaptive = true
			a.effort = info.Effort
		} else if info.ThinkingBudget > 0 {
			a.effort = budgetToEffort(info.ThinkingBudget)
		}
	}
	if e := strings.TrimSpace(os.Getenv("EIGEN_REASONING_EFFORT")); e != "" {
		a.SetEffort(e)
	}
	return a, nil
}

// Name reports the model and auth mode for the status bar.
func (a *Anthropic) Name() string {
	mode := "api-key"
	if a.apiKey == "" {
		mode = "claude-code oauth"
	}
	return a.Model + " (anthropic " + mode + ")"
}

// ModelID is the raw model id (no suffix), what llm.New accepts.
func (a *Anthropic) ModelID() string { return a.Model }

// SetEffort changes the reasoning effort (adaptive models) / thinking budget.
// Validates against the per-model level set from the catalog when available.
func (a *Anthropic) SetEffort(level string) bool {
	// Validate against the per-model level set; fall back to the global list
	// (so tests with no catalog entry still reject truly unknown levels).
	levels := ModelEffortLevels(a.Model)
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
	if !ok {
		// Adaptive effort (auto/low/medium/high): not in the budget map.
		// For adaptive models the effort string is sent directly; set budget=0.
		a.thinkingBudget = 0
		a.effort = level
		return true
	}
	a.thinkingBudget = b
	a.effort = level
	return true
}

// Effort returns the current reasoning-effort label.
func (a *Anthropic) Effort() string { return a.effort }

// --- wire types (native Messages API) ---

type anthropicTextBlock struct {
	Type         string             `json:"type"`
	Text         string             `json:"text"`
	CacheControl *anthropicCacheCtl `json:"cache_control,omitempty"`
}

type anthropicCacheCtl struct {
	Type string `json:"type"` // "ephemeral"
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	// CacheControl on the LAST tool marks the end of the (fully static) tool
	// prefix as a cache breakpoint, so the tool schemas stay cached even when
	// the smaller system/memory portion changes between turns.
	CacheControl *anthropicCacheCtl `json:"cache_control,omitempty"`
}

// anthropicContent is one block in a message's content array (text / image /
// tool_use / tool_result). Only the fields for the active type are set.
type anthropicContent struct {
	Type      string             `json:"type"`
	Text      string             `json:"text,omitempty"`
	Source    *anthropicImageSrc `json:"source,omitempty"`      // image
	ID        string             `json:"id,omitempty"`          // tool_use
	Name      string             `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage    `json:"input,omitempty"`       // tool_use
	ToolUseID string             `json:"tool_use_id,omitempty"` // tool_result
	Content   any                `json:"content,omitempty"`     // tool_result: string, or []anthropicContent (text+image)
	IsError   bool               `json:"is_error,omitempty"`    // tool_result
}

// anthropicImageSrc is the base64 image source for an "image" content block.
type anthropicImageSrc struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // image/png, image/jpeg, image/webp, image/gif
	Data      string `json:"data"`       // base64-encoded image bytes
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicRequest struct {
	Model        string               `json:"model"`
	MaxTokens    int                  `json:"max_tokens"`
	System       []anthropicTextBlock `json:"system,omitempty"`
	Messages     []anthropicMessage   `json:"messages"`
	Tools        []anthropicTool      `json:"tools,omitempty"`
	Thinking     json.RawMessage      `json:"thinking,omitempty"`
	OutputConfig json.RawMessage      `json:"output_config,omitempty"`
}

type anthropicReply struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (a *Anthropic) Complete(ctx context.Context, req Request) (*Response, error) {
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("request has no messages")
	}
	maxTokens := anthropicMaxTok
	if a.thinkingBudget > 0 && maxTokens <= a.thinkingBudget {
		maxTokens = a.thinkingBudget + anthropicMaxTok
	}
	payload := anthropicRequest{
		Model:     a.Model,
		MaxTokens: maxTokens,
		System:    a.systemBlocks(req.System),
		Messages:  anthropicMessages(req),
		Tools:     anthropicTools(req.Tools, a.cache),
	}
	// Thinking: adaptive models use thinking.type=adaptive + output_config.effort;
	// budget models use thinking.type=enabled + budget_tokens.
	switch {
	case a.adaptive && a.effort != "" && a.effort != "minimal" && a.effort != "off":
		payload.Thinking = json.RawMessage(`{"type":"adaptive"}`)
		payload.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":%q}`, a.effort))
	case !a.adaptive && a.thinkingBudget > 0:
		payload.Thinking = json.RawMessage(fmt.Sprintf(`{"type":"enabled","budget_tokens":%d}`, a.thinkingBudget))
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	headers, err := a.headers()
	if err != nil {
		return nil, err
	}
	raw, status, err := httpJSON(ctx, a.http, anthropicURL, headers, body, nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	var reply anthropicReply
	if jerr := json.Unmarshal(raw, &reply); jerr != nil {
		return nil, fmt.Errorf("decode response: %w", jerr)
	}
	if status < 200 || status >= 300 {
		if reply.Error != nil {
			return nil, fmt.Errorf("anthropic HTTP %d: %s: %s", status, reply.Error.Type, reply.Error.Message)
		}
		return nil, fmt.Errorf("anthropic HTTP %d: %s", status, string(raw))
	}
	if reply.StopReason == "max_tokens" {
		return nil, fmt.Errorf("anthropic response truncated (max_tokens): refusing possibly-truncated output")
	}

	out := &Response{Usage: Usage{InputTokens: reply.Usage.InputTokens, OutputTokens: reply.Usage.OutputTokens, CacheReadTokens: reply.Usage.CacheReadInputTokens, CacheWriteTokens: reply.Usage.CacheCreationInputTokens}}
	for _, blk := range reply.Content {
		switch blk.Type {
		case "text":
			out.Text += blk.Text
		case "thinking":
			out.Reasoning += blk.Text
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        blk.ID,
				Name:      blk.Name,
				Arguments: normalizeArgsRaw(blk.Input),
			})
		}
	}
	return out, nil
}

// systemBlocks builds the system array. The first block MUST be the Claude Code
// spoof (the OAuth gate); the caller's system prompt follows as a second block,
// cached when caching is on.
func (a *Anthropic) systemBlocks(system string) []anthropicTextBlock {
	blocks := []anthropicTextBlock{{Type: "text", Text: claudeCodeSpoof}}
	if s := strings.TrimSpace(system); s != "" {
		blk := anthropicTextBlock{Type: "text", Text: s}
		if a.cache {
			blk.CacheControl = &anthropicCacheCtl{Type: "ephemeral"}
		}
		blocks = append(blocks, blk)
	}
	return blocks
}

// headers builds the request headers, mirroring Claude Code. OAuth uses a
// Bearer token; an API key uses x-api-key.
func (a *Anthropic) headers() (map[string]string, error) {
	h := map[string]string{
		"anthropic-version": anthropicVersion,
		"anthropic-beta":    strings.Join(anthropicBetas, ","),
	}
	if a.apiKey != "" {
		h["x-api-key"] = a.apiKey
		return h, nil
	}
	tok, err := claudeOAuthToken(a.oauthFile)
	if err != nil {
		return nil, err
	}
	h["authorization"] = "Bearer " + tok
	return h, nil
}

// anthropicMessages maps the neutral transcript to native content blocks.
// Anthropic uses role user/assistant only; tool results are user-role messages
// of tool_result blocks, so consecutive RoleTool messages group into one user
// turn (same constraint as Converse).
func anthropicMessages(req Request) []anthropicMessage {
	var out []anthropicMessage
	var pending []anthropicContent
	flush := func() {
		if len(pending) > 0 {
			out = append(out, anthropicMessage{Role: "user", Content: pending})
			pending = nil
		}
	}
	for _, m := range req.Messages {
		switch m.Role {
		case RoleUser:
			flush()
			content := []anthropicContent{}
			if m.Text != "" {
				content = append(content, anthropicContent{Type: "text", Text: m.Text})
			}
			for _, img := range m.Images {
				if isAnthropicImageType(img.MediaType) {
					content = append(content, anthropicContent{
						Type: "image",
						Source: &anthropicImageSrc{
							Type:      "base64",
							MediaType: img.MediaType,
							Data:      base64.StdEncoding.EncodeToString(img.Data),
						},
					})
				}
			}
			if len(content) == 0 {
				content = append(content, anthropicContent{Type: "text", Text: ""})
			}
			out = append(out, anthropicMessage{Role: "user", Content: content})
		case RoleAssistant:
			flush()
			var content []anthropicContent
			if strings.TrimSpace(m.Text) != "" {
				content = append(content, anthropicContent{Type: "text", Text: m.Text})
			}
			for _, tc := range m.ToolCalls {
				input := tc.Arguments
				if len(input) == 0 {
					input = json.RawMessage(`{}`)
				}
				content = append(content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			// Skip assistant turns with no content (Anthropic rejects empties).
			if len(content) > 0 {
				out = append(out, anthropicMessage{Role: "assistant", Content: content})
			}
		case RoleTool:
			tr := anthropicContent{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Text,
				IsError:   m.ToolError,
			}
			// Images (screenshots) make the content an array of text+image
			// blocks instead of a plain string — Anthropic tool_result accepts
			// either form.
			var imgs []anthropicContent
			for _, img := range m.Images {
				if !isAnthropicImageType(img.MediaType) {
					continue
				}
				imgs = append(imgs, anthropicContent{
					Type: "image",
					Source: &anthropicImageSrc{
						Type:      "base64",
						MediaType: img.MediaType,
						Data:      base64.StdEncoding.EncodeToString(img.Data),
					},
				})
			}
			if len(imgs) > 0 {
				blocks := []anthropicContent{{Type: "text", Text: m.Text}}
				tr.Content = append(blocks, imgs...)
			}
			pending = append(pending, tr)
		}
	}
	flush()
	return out
}

// isAnthropicImageType reports whether a media type is one Anthropic accepts.
func isAnthropicImageType(mt string) bool {
	switch mt {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	}
	return false
}

// anthropicTools maps neutral tool specs to native tool definitions. When cache
// is on, the LAST tool gets a cache breakpoint so the (static) tool prefix is
// cached independently of the system/message tail.
func anthropicTools(tools []ToolSpec, cache bool) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object"}`)
		}
		out = append(out, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	if cache {
		out[len(out)-1].CacheControl = &anthropicCacheCtl{Type: "ephemeral"}
	}
	return out
}

// claudeCredentialsPath is the default Claude Code credentials file.
func claudeCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

// claudeOAuthToken reads an unexpired Claude Code OAuth access token from the
// credentials file. It does NOT refresh; an expired token returns an error
// telling the user to re-run Claude Code (which refreshes it).
func claudeOAuthToken(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("no credentials path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var creds struct {
		ClaudeAiOauth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"` // ms since epoch
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &creds); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	tok := strings.TrimSpace(creds.ClaudeAiOauth.AccessToken)
	if tok == "" {
		return "", fmt.Errorf("no claudeAiOauth.accessToken in %s", path)
	}
	if creds.ClaudeAiOauth.ExpiresAt > 0 && time.Now().UnixMilli() >= creds.ClaudeAiOauth.ExpiresAt {
		return "", fmt.Errorf("claude code token expired; run `claude` to refresh it")
	}
	return tok, nil
}

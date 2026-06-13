package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// glmDefaultBaseURL is Zhipu's OpenAI-compatible "coding plan" API root, the
// endpoint used by coding clients (Claude Code / GLM coding plan). The general
// API root (api.z.ai/api/paas/v4) also works; override with EIGEN_GLM_BASE_URL.
const glmDefaultBaseURL = "https://api.z.ai/api/coding/paas/v4"

// Compile-time interface checks.
var _ Searcher = (*GLM)(nil)
var _ EffortSetter = (*GLM)(nil)

// GLM drives Zhipu's GLM models (glm-5.2, glm-5.1, glm-4.6, …) over their
// OpenAI-compatible chat-completions API. When search is enabled, it injects
// the server-side web_search tool so GLM can ground answers in live web data
// without requiring client-side fetch — a zero-cost alternative to eigen's
// built-in fetch for web queries.
type GLM struct {
	c *chatClient

	// search controls the server-side web_search tool: "off" disables it;
	// "auto" and "on" enable it (GLM does not distinguish auto vs on — both
	// mean the model may search when it deems it useful).
	search string

	// thinking is GLM's reasoning mode. GLM exposes exactly TWO modes via the
	// `thinking.type` request field: "enabled" (the model emits
	// reasoning_content before its answer) and "disabled" (no reasoning). We
	// map eigen's effort vocabulary onto these: "off" → disabled, anything
	// else → enabled. Empty = don't send the field (server default).
	thinking string

	// clearThinking controls GLM's "Preserved Thinking": when false (the
	// default while thinking is enabled), GLM keeps reasoning_content from
	// prior assistant turns for coherence + prompt-cache hits — the recommended
	// mode for coding/agent use. EIGEN_GLM_CLEAR_THINKING=1 flips it to clear
	// cross-turn reasoning (the standard-endpoint default).
	clearThinking bool
}

// NewGLM builds a GLM provider from the environment.
//
//	GLM_API_KEY (or ZHIPUAI_API_KEY / EIGEN_GLM_API_KEY)  required: key from z.ai
//	EIGEN_GLM_BASE_URL                                    override the API root
//	EIGEN_GLM_SEARCH=off|auto|on                          web search mode (default: auto)
func NewGLM(model string) (*GLM, error) {
	key := firstNonEmpty(
		os.Getenv("GLM_API_KEY"),
		os.Getenv("ZHIPUAI_API_KEY"),
		os.Getenv("EIGEN_GLM_API_KEY"),
	)
	if key == "" {
		return nil, fmt.Errorf("GLM_API_KEY not set (Zhipu/z.ai key)")
	}
	base := firstNonEmpty(os.Getenv("EIGEN_GLM_BASE_URL"), glmDefaultBaseURL)
	if model == "" {
		model = "glm-5.1"
	}
	g := &GLM{
		c:      newChatClient(base, model, key, "glm"),
		search: "auto", // default on — GLM's web_search is free/included
	}
	if v := strings.TrimSpace(os.Getenv("EIGEN_GLM_SEARCH")); v != "" {
		g.search = v
	}
	// Default thinking mode from the catalog (reasoning models default to the
	// catalog Effort; "off" disables). GLM exposes only enabled/disabled.
	info, cataloged := Lookup(model)
	if cataloged && info.Reasoning {
		g.thinking = "enabled"
		if info.Effort == "off" {
			g.thinking = "disabled"
		}
		// EIGEN_REASONING_EFFORT can override at startup (off|on for GLM).
		if v := strings.TrimSpace(os.Getenv("EIGEN_REASONING_EFFORT")); v != "" {
			g.SetEffort(v)
		}
	}
	// Preserved Thinking is on by default (clear_thinking:false) for coherence
	// + cache hits; EIGEN_GLM_CLEAR_THINKING=1 drops cross-turn reasoning.
	if v := strings.TrimSpace(os.Getenv("EIGEN_GLM_CLEAR_THINKING")); v == "1" || strings.EqualFold(v, "true") {
		g.clearThinking = true
	}
	// Wire the web_search tool injection + the thinking body field.
	g.c.extraTools = g.webSearchTool
	g.c.extra = g.bodyExtra
	return g, nil
}

func (g *GLM) Name() string    { return g.c.model + " (zhipu glm)" }
func (g *GLM) ModelID() string { return g.c.model }

func (g *GLM) Complete(ctx context.Context, req Request) (*Response, error) {
	return g.c.complete(ctx, g.prepare(req))
}

func (g *GLM) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	return g.c.stream(ctx, g.prepare(req), sink)
}

// SearchMode reports the current web search mode (off|auto|on).
func (g *GLM) SearchMode() string { return g.search }

// SetSearch changes the web search mode. Returns false for an unknown mode.
func (g *GLM) SetSearch(mode string) bool {
	switch mode {
	case "off", "auto", "on":
		g.search = mode
		return true
	default:
		return false
	}
}

// Effort reports the current reasoning mode in eigen's vocabulary: "off" when
// thinking is disabled, "on" when enabled, "" when this model has no thinking
// control.
func (g *GLM) Effort() string {
	switch g.thinking {
	case "disabled":
		return "off"
	case "enabled":
		return "on"
	default:
		return ""
	}
}

// SetEffort maps eigen's effort vocabulary onto GLM's two thinking modes:
// "off" → disabled, any other accepted level ("on", or any reasoning word) →
// enabled. Returns false when this model has no thinking control, or for an
// unrecognized value.
func (g *GLM) SetEffort(level string) bool {
	if g.thinking == "" {
		return false // non-reasoning model: no thinking control
	}
	switch level {
	case "off", "none", "disabled", "minimal":
		g.thinking = "disabled"
		return true
	case "on", "enabled", "low", "medium", "high", "xhigh", "max":
		g.thinking = "enabled"
		return true
	default:
		return false
	}
}

// bodyExtra injects GLM's reasoning request fields when a mode is set:
//   - thinking.type enabled|disabled — the on/off toggle (eigen effort on|off).
//   - clear_thinking:false when ENABLED — "Preserved Thinking": GLM retains
//     reasoning_content from prior assistant turns for coherence + cache hits
//     (the recommended mode for coding/agent use, per z.ai docs). eigen already
//     carries Message.Reasoning back across turns, so this is the right default.
//     Override with EIGEN_GLM_CLEAR_THINKING=1 to drop cross-turn reasoning.
func (g *GLM) bodyExtra() map[string]any {
	if g.thinking == "" {
		return nil
	}
	extra := map[string]any{"thinking": map[string]any{"type": g.thinking}}
	if g.thinking == "enabled" && !g.clearThinking {
		extra["clear_thinking"] = false // preserve reasoning across turns
	}
	return extra
}

// prepare appends a hint to the system prompt when web_search is active, telling
// GLM to prefer its built-in search over the client-side fetch tool.
func (g *GLM) prepare(req Request) Request {
	if g.search == "off" {
		return req
	}
	req.System += "\n\nYou have a built-in web_search tool that can search the live web. Use it instead of the fetch tool for any web lookups — it is faster, more reliable, and returns fresher results. Prefer web_search over fetch for all online information."
	return req
}

// webSearchTool returns the GLM web_search built-in tool entry when search is
// enabled, or nil when off.
func (g *GLM) webSearchTool() []map[string]any {
	if g.search == "off" {
		return nil
	}
	return []map[string]any{
		{
			"type": "web_search",
			"web_search": map[string]any{
				"enable":        true,
				"search_result": true,
			},
		},
	}
}

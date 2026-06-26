package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
)

// glmDefaultBaseURL is Zhipu's OpenAI-compatible "coding plan" API root, the
// endpoint used by coding clients (Claude Code / GLM coding plan). The general
// API root (api.z.ai/api/paas/v4) also works; override with EIGEN_GLM_BASE_URL.
const glmDefaultBaseURL = "https://api.z.ai/api/coding/paas/v4"

// Compile-time interface checks.
var _ Searcher = (*GLM)(nil)
var _ EffortSetter = (*GLM)(nil)

// GLM drives Zhipu's GLM models (glm-5.2 default, glm-5.1, glm-4.6, …) over their
// OpenAI-compatible chat-completions API. When search is enabled, it injects
// the server-side web_search tool so GLM can ground answers in live web data
// without requiring client-side fetch — a zero-cost alternative to eigen's
// built-in fetch for web queries.
type GLM struct {
	c *chatClient

	mu sync.RWMutex

	// search controls the server-side web_search tool: "off" disables it;
	// "auto" and "on" enable it (GLM does not distinguish auto vs on — both
	// mean the model may search when it deems it useful).
	search string

	// thinking is GLM's reasoning ON/OFF toggle, sent as the `thinking.type`
	// request field: "enabled" (the model emits reasoning_content before its
	// answer) or "disabled" (no reasoning). Empty = don't send the field
	// (server default).
	thinking string

	// effort is GLM-5.2's graded reasoning level, sent as the OpenAI-style
	// `reasoning_effort` field WHEN thinking is enabled. GLM-5.2 documents two
	// levels — "high" and "max" — and z.ai recommends "max" for coding/agent
	// use. Empty means this model has no graded effort (older GLM: thinking is a
	// bare enabled/disabled toggle with no reasoning_effort field), so the field
	// is omitted. Without reasoning_effort, GLM-5.2 does NOT stream
	// reasoning_content even with thinking enabled — which is why thoughts never
	// appeared.
	effort string

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
		model = "glm-5.2"
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
		// Graded effort (GLM-5.2: high|max): when the catalog ladder carries a
		// real level beyond the bare off/on toggle, seed reasoning_effort from
		// the catalog default ("max" for glm-5.2, per z.ai's coding guidance) so
		// reasoning_content actually streams. Detect "graded" as a ladder that
		// contains anything other than off/on.
		if glmGradedEffort(info.EffortLevels) {
			g.effort = info.Effort
			if g.effort == "" || g.effort == "on" {
				g.effort = "max"
			}
		}
		// EIGEN_REASONING_EFFORT can override at startup.
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
	search, thinking, effort, clearThinking := g.snapshot()
	cc := *g.c
	cc.extraTools = func() []map[string]any { return glmWebSearchTool(search) }
	cc.extra = func() map[string]any { return glmBodyExtra(thinking, effort, clearThinking) }
	return cc.complete(ctx, glmPrepare(req, search))
}

func (g *GLM) Stream(ctx context.Context, req Request, sink StreamSink) (*Response, error) {
	search, thinking, effort, clearThinking := g.snapshot()
	cc := *g.c
	cc.extraTools = func() []map[string]any { return glmWebSearchTool(search) }
	cc.extra = func() map[string]any { return glmBodyExtra(thinking, effort, clearThinking) }
	return cc.stream(ctx, glmPrepare(req, search), sink)
}

// SearchMode reports the current web search mode (off|auto|on).
func (g *GLM) SearchMode() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.search
}

// SetSearch changes the web search mode. Returns false for an unknown mode.
func (g *GLM) SetSearch(mode string) bool {
	switch mode {
	case "off", "auto", "on":
		g.mu.Lock()
		g.search = mode
		g.mu.Unlock()
		return true
	default:
		return false
	}
}

// Effort reports the current reasoning level in eigen's vocabulary. For a
// GRADED model (GLM-5.2: high|max) it returns the live reasoning_effort; for a
// bare-toggle model it returns "off"/"on"; "" when this model has no thinking
// control.
func (g *GLM) Effort() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.thinking == "disabled" {
		return "off"
	}
	if g.thinking == "enabled" {
		if g.effort != "" {
			return g.effort // graded: report the actual reasoning_effort (high|max)
		}
		return "on"
	}
	return ""
}

// SetEffort maps eigen's effort vocabulary onto GLM's reasoning controls. "off"
// disables thinking; a graded model (GLM-5.2) records the reasoning_effort
// (high|max, clamped) AND enables thinking; a bare-toggle model just enables.
// Returns false when this model has no thinking control, or for an
// unrecognized value.
func (g *GLM) SetEffort(level string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.thinking == "" {
		return false // non-reasoning model: no thinking control
	}
	switch level {
	case "off", "none", "disabled", "minimal":
		g.thinking = "disabled"
		return true
	case "on", "enabled", "low", "medium", "high", "xhigh", "max":
		g.thinking = "enabled"
		// On a graded model, also set reasoning_effort. GLM-5.2 accepts only
		// "high" and "max". A bare on/enabled (and xhigh/max) maps to "max" — the
		// recommended coding default and consistent with NewGLM's seed, so
		// toggling thinking off→on doesn't silently downgrade max→high; only an
		// explicit low/medium/high lands on "high".
		if g.effort != "" {
			switch level {
			case "on", "enabled", "xhigh", "max":
				g.effort = "max"
			default:
				g.effort = "high"
			}
		}
		return true
	default:
		return false
	}
}

// glmGradedEffort reports whether an effort ladder is GLM-5.2-style graded
// (carries a level beyond the bare off/on toggle) rather than a plain on/off.
func glmGradedEffort(levels []string) bool {
	for _, l := range levels {
		switch l {
		case "off", "on", "":
			// toggle rungs — ignore
		default:
			return true
		}
	}
	return false
}

func (g *GLM) snapshot() (search, thinking, effort string, clearThinking bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.search, g.thinking, g.effort, g.clearThinking
}

// bodyExtra injects GLM's reasoning request fields when a mode is set:
//   - thinking.type enabled|disabled — the on/off toggle (eigen effort on|off).
//   - reasoning_effort high|max — GLM-5.2's graded level. REQUIRED for GLM-5.2
//     to actually stream reasoning_content; without it the model returns no
//     thoughts even with thinking enabled (z.ai coding guidance recommends max).
//   - clear_thinking:false when ENABLED — "Preserved Thinking": GLM retains
//     reasoning_content from prior assistant turns for coherence + cache hits
//     (the recommended mode for coding/agent use, per z.ai docs). eigen already
//     carries Message.Reasoning back across turns, so this is the right default.
//     Override with EIGEN_GLM_CLEAR_THINKING=1 to drop cross-turn reasoning.
func (g *GLM) bodyExtra() map[string]any {
	_, thinking, effort, clearThinking := g.snapshot()
	return glmBodyExtra(thinking, effort, clearThinking)
}

func glmBodyExtra(thinking, effort string, clearThinking bool) map[string]any {
	if thinking == "" {
		return nil
	}
	extra := map[string]any{"thinking": map[string]any{"type": thinking}}
	if thinking == "enabled" {
		// GLM-5.2 needs reasoning_effort (high|max) to actually stream
		// reasoning_content; older GLM has no graded effort (effort==""), so the
		// field is omitted there.
		if effort != "" {
			extra["reasoning_effort"] = effort
		}
		if !clearThinking {
			extra["clear_thinking"] = false // preserve reasoning across turns
		}
	}
	return extra
}

func glmPrepare(req Request, search string) Request {
	if search == "off" {
		return req
	}
	req.System += "\n\nYou have a built-in web_search tool that can search the live web. Use it instead of the fetch tool for any web lookups — it is faster, more reliable, and returns fresher results. Prefer web_search over fetch for all online information."
	return req
}

// webSearchTool returns the GLM web_search built-in tool entry when search is
// enabled, or nil when off.
func (g *GLM) webSearchTool() []map[string]any {
	search, _, _, _ := g.snapshot()
	return glmWebSearchTool(search)
}

func glmWebSearchTool(search string) []map[string]any {
	if search == "off" {
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

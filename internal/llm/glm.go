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

// GLM drives Zhipu's GLM models (glm-5.1, glm-4.6, glm-4.5, …) over their
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
	// Wire the web_search tool injection.
	g.c.extraTools = g.webSearchTool
	return g, nil
}

func (g *GLM) Name() string { return g.c.model + " (zhipu glm)" }

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

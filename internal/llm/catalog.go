package llm

import "strings"

// ModelInfo records what eigen knows about a model: its provider, context
// window, and capabilities. The window drives context-budget auto-detection and
// auto-compaction; the capability flags drive provider wiring (prompt caching,
// 1M-context beta, reasoning/extended-thinking) so eigen fits each model
// without hand-tuned flags.
type ModelInfo struct {
	ID            string
	Provider      string // mantle | converse | llama
	ContextWindow int    // total tokens the model accepts (default window)

	// Cache reports that the provider supports prompt caching for this model
	// (Anthropic cachePoint blocks on Converse), so eigen marks cache breakpoints.
	Cache bool

	// Context1M reports that the model supports a 1M-token context via a beta
	// flag (Anthropic context-1m on Bedrock). When enabled, ContextWindow1M is
	// the budget eigen targets.
	Context1M       bool
	ContextWindow1M int

	// Reasoning reports the model exposes a reasoning/effort or extended-thinking
	// control. Effort is the default reasoning effort for effort-style models
	// (mantle GPT: minimal|low|medium|high|xhigh). ThinkingBudget is the default
	// extended-thinking token budget for thinking-style models (Anthropic).
	Reasoning      bool
	Effort         string
	ThinkingBudget int

	// Search reports the model supports server-side live search (xAI Grok Live
	// Search over web + X). When set, the Grok provider enables search by default.
	Search bool
}

// Catalog is the set of models eigen knows about. It is additive: an unknown
// model simply falls back to provider defaults.
var Catalog = []ModelInfo{
	// Bedrock "mantle" (OpenAI-family). Effort-style reasoning (high is stable;
	// xhigh stalls mid-task on mantle — see mantle.go).
	{ID: "openai.gpt-5.5", Provider: "mantle", ContextWindow: 272000, Reasoning: true, Effort: "high"},
	{ID: "openai.gpt-5.4", Provider: "mantle", ContextWindow: 272000, Reasoning: true, Effort: "high"},
	{ID: "openai.gpt-5", Provider: "mantle", ContextWindow: 272000, Reasoning: true, Effort: "high"},

	// Bedrock Converse (Anthropic Claude). Prompt caching + 1M context (beta) +
	// extended thinking. Default 200k window; 1M when the beta is enabled.
	//
	// claude-fable-5 is the current flagship and eigen's default. It is served
	// via the GLOBAL Bedrock inference profile (global.anthropic.claude-fable-5 —
	// there is no us. profile) and requires a non-default data-retention mode
	// enabled on the AWS account. Uses the adaptive thinking API (Effort) like
	// opus-4-8, prompt caching, and the 1M-context beta (on by default; force
	// off with EIGEN_CONVERSE_1M=0).
	{ID: "global.anthropic.claude-fable-5", Provider: "converse", ContextWindow: 200000,
		Cache: true, Context1M: true, ContextWindow1M: 1000000, Reasoning: true, Effort: "high"},
	{ID: "us.anthropic.claude-opus-4-8", Provider: "converse", ContextWindow: 200000,
		Cache: true, Context1M: true, ContextWindow1M: 1000000, Reasoning: true, Effort: "high"},
	{ID: "us.anthropic.claude-sonnet-4-6", Provider: "converse", ContextWindow: 200000,
		Cache: true, Context1M: true, ContextWindow1M: 1000000, Reasoning: true, ThinkingBudget: 8192},
	{ID: "us.anthropic.claude-opus-4-1", Provider: "converse", ContextWindow: 200000, Cache: true},
	{ID: "us.anthropic.claude-3-5-sonnet", Provider: "converse", ContextWindow: 200000, Cache: true},
	// Haiku 4.5: the small/fast/cheap model eigen uses for background chores
	// (session titling, dreaming, skill vulnerability scans).
	{ID: "us.anthropic.claude-haiku-4-5", Provider: "converse", ContextWindow: 200000, Cache: true},

	// Native Anthropic API (api.anthropic.com), authenticated with a Claude
	// Code OAuth login (~/.claude/.credentials.json) or ANTHROPIC_API_KEY.
	// These are Anthropic's own model ids (not the Bedrock us.anthropic.* names)
	// — the same catalog Claude Code drives. Adaptive thinking (Effort) like the
	// Bedrock opus entries; 1M context via beta.
	{ID: "claude-fable-5", Provider: "anthropic", ContextWindow: 200000,
		Cache: true, Context1M: true, ContextWindow1M: 1000000, Reasoning: true, Effort: "high"},
	{ID: "claude-opus-4-1-20250805", Provider: "anthropic", ContextWindow: 200000,
		Cache: true, Context1M: true, ContextWindow1M: 1000000, Reasoning: true, Effort: "high"},
	{ID: "claude-sonnet-4-5-20250929", Provider: "anthropic", ContextWindow: 200000,
		Cache: true, Context1M: true, ContextWindow1M: 1000000, Reasoning: true, ThinkingBudget: 8192},
	{ID: "claude-opus-4-20250514", Provider: "anthropic", ContextWindow: 200000, Cache: true, Reasoning: true, Effort: "high"},

	// Local llama (OpenAI-compatible server). Window is modest by default.
	{ID: "local", Provider: "llama", ContextWindow: 40000},

	// xAI Grok (OpenAI-compatible API + Live Search). grok-build is the advanced
	// coding model with server-side web/X search; composer is Cursor's coding
	// model (no backend search).
	{ID: "grok-build", Provider: "grok", ContextWindow: 512000, Search: true},
	{ID: "grok-composer-2.5-fast", Provider: "grok", ContextWindow: 200000},
	{ID: "grok-4", Provider: "grok", ContextWindow: 256000, Search: true},
	{ID: "grok-code-fast-1", Provider: "grok", ContextWindow: 256000},

	// Zhipu GLM coding models (OpenAI-compatible coding API). GLM-5.1 is the
	// current flagship; 200K context across the 5.x/4.6/4.7 line.
	{ID: "glm-5.1", Provider: "glm", ContextWindow: 200000},
	{ID: "glm-5", Provider: "glm", ContextWindow: 200000},
	{ID: "glm-5-turbo", Provider: "glm", ContextWindow: 200000},
	{ID: "glm-4.7", Provider: "glm", ContextWindow: 200000},
	{ID: "glm-4.6", Provider: "glm", ContextWindow: 200000},
	{ID: "glm-4.5", Provider: "glm", ContextWindow: 128000},
	{ID: "glm-4.5-air", Provider: "glm", ContextWindow: 128000},
}

// defaultModelByProvider mirrors each provider's built-in default, so callers
// can resolve the effective model before construction (for window lookup).
var defaultModelByProvider = map[string]string{
	"":                 "openai.gpt-5.5",
	"mantle":           "openai.gpt-5.5",
	"bedrock-mantle":   "openai.gpt-5.5",
	"converse":         "global.anthropic.claude-fable-5",
	"bedrock-converse": "global.anthropic.claude-fable-5",
	"claude":           "global.anthropic.claude-fable-5",
	"llama":            "local",
	"local":            "local",
	"grok":             "grok-build",
	"xai":              "grok-build",
	"glm":              "glm-5.1",
	"zhipu":            "glm-5.1",
	"z.ai":             "glm-5.1",
}

// DefaultModel returns the model id a provider uses when none is specified.
func DefaultModel(provider string) string { return defaultModelByProvider[provider] }

// ResolveProvider reconciles a (provider, model) pair against the catalog so a
// known model is never sent to the wrong backend. If the model is a known
// catalog entry whose provider differs from the requested one, the catalog's
// provider wins (e.g. asking mantle for "us.anthropic.claude-opus-4-8" — which
// only exists on converse — corrects to converse instead of 404ing). An unknown
// model, or a model with no catalog provider, leaves the requested provider
// untouched. Empty provider/model are returned unchanged for the caller's
// own defaulting.
func ResolveProvider(provider, model string) string {
	if model == "" {
		return provider
	}
	if info, ok := Lookup(model); ok && info.Provider != "" && info.Provider != provider {
		// Only override when the requested provider is a different *real* provider
		// (or empty). Aliases that map to the same backend should not flip.
		if canonicalProvider(provider) != canonicalProvider(info.Provider) {
			return info.Provider
		}
	}
	return provider
}

// canonicalProvider collapses provider aliases to a single canonical name so
// alias differences (e.g. "claude" vs "converse") are not treated as a mismatch.
func canonicalProvider(p string) string {
	switch p {
	case "", "mantle", "bedrock-mantle":
		return "mantle"
	case "converse", "bedrock-converse", "claude":
		return "converse"
	case "anthropic", "claude-code", "claude-api":
		return "anthropic"
	case "llama", "local":
		return "llama"
	case "grok", "xai":
		return "grok"
	case "glm", "zhipu", "z.ai":
		return "glm"
	default:
		return p
	}
}

// Models returns the known catalog entries in a stable order, so callers (e.g.
// the TUI `/model` picker) can present the models a user may switch to.
func Models() []ModelInfo {
	out := make([]ModelInfo, len(Catalog))
	copy(out, Catalog)
	return out
}

// Lookup returns the catalog entry for a model id, matching exactly first, then
// by prefix (so versioned/region-qualified ids still resolve). The boolean
// reports whether a match was found.
func Lookup(model string) (ModelInfo, bool) {
	if model == "" {
		return ModelInfo{}, false
	}
	for _, m := range Catalog {
		if m.ID == model {
			return m, true
		}
	}
	for _, m := range Catalog {
		if strings.HasPrefix(model, m.ID) || strings.HasPrefix(m.ID, model) {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// EffectiveContextWindow returns the window eigen should budget against for a
// model: the 1M window when the model supports it and the 1M beta is enabled
// (default on for Context1M models; force off with EIGEN_CONVERSE_1M=0). Falls
// back to the standard window, or 0 if the model is unknown.
func EffectiveContextWindow(model string) int {
	m, ok := Lookup(model)
	if !ok {
		return 0
	}
	if m.Context1M && m.ContextWindow1M > 0 && envBool("EIGEN_CONVERSE_1M", true) {
		return m.ContextWindow1M
	}
	return m.ContextWindow
}

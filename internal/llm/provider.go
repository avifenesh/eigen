package llm

import (
	"fmt"
	"strings"
)

// New selects a provider by name. Empty defaults to mantle (Bedrock GPT-5.5).
// The (provider, model) pair is reconciled against the catalog first, so a
// known model is never dispatched to the wrong backend (which would 404).
// The model accepts ref form ("mantle:us.openai.gpt-5.5"): an explicit tag
// wins over both the provider argument and the catalog.
func New(provider, model string) (Provider, error) {
	// Defensive: a model id that accidentally carried a Name() suffix like
	// "claude-opus-4-8 (bedrock converse)" (an old daemon model-switch bug
	// persisted such ids) is stripped back to the raw id so it resolves.
	if i := strings.Index(model, " ("); i > 0 && strings.HasSuffix(model, ")") {
		model = model[:i]
	}
	if tag, id := ParseRef(model); tag != "" {
		// An explicit tag FORCES the backend (that is its purpose) — no
		// catalog second-guessing. Untagged ids self-tag via the catalog.
		provider, model = tag, id
	} else {
		provider = ResolveProvider(provider, model)
	}
	provider = canonicalProvider(provider) // aliases ("ant", "xai") → real backend
	switch provider {
	case "", "mantle", "bedrock-mantle":
		return NewMantle(model)
	case "llama", "local":
		return NewLlama(model)
	case "converse", "bedrock-converse", "claude":
		return NewConverse(model)
	case "anthropic", "claude-code", "claude-api":
		return NewAnthropic(model)
	case "codex", "openai-codex", "chatgpt":
		return NewCodex(model)
	case "grok", "xai":
		return NewGrok(model)
	case "glm", "zhipu", "z.ai":
		return NewGLM(model)
	case "moa":
		return newMoAProvider(model)
	default:
		return newCustomProvider(provider, model)
	}
}

// CloneProvider rebuilds p as a fresh provider instance while preserving Eigen's
// provider decorators (fallbackProvider / chainProvider). It is used when a
// subtask needs exclusive runtime knobs (effort/search/fast) without mutating a
// provider shared by the parent session or router cache.
//
// Important: this must NOT collapse a fallback/chain wrapper to only its
// headline ModelID. A research chain such as glm→opus would otherwise become a
// bare glm provider; a GLM quota 429 would then fail the subagent instead of
// falling through to opus. Unknown/test providers are rejected rather than being
// sent through New("", model), because an empty provider defaults to Mantle and
// can turn mock model ids into real network calls.
func CloneProvider(p Provider) (Provider, error) {
	switch v := p.(type) {
	case nil:
		return nil, fmt.Errorf("nil provider")
	case *chainProvider:
		return v.clone(), nil
	case *fallbackProvider:
		primary, err := CloneProvider(v.primary)
		if err != nil {
			return nil, fmt.Errorf("clone fallback primary: %w", err)
		}
		fallback, err := CloneProvider(v.fallback)
		if err != nil {
			return nil, fmt.Errorf("clone fallback target: %w", err)
		}
		return NewFallback(primary, fallback), nil
	default:
		model := strings.TrimSpace(p.ModelID())
		if model == "" {
			return nil, fmt.Errorf("empty model id")
		}
		info, ok := Lookup(model)
		if !ok || strings.TrimSpace(info.Provider) == "" {
			return nil, fmt.Errorf("unknown model %q", model)
		}
		return New(info.Provider, model)
	}
}

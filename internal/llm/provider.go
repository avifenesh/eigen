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
	case "grok", "xai":
		return NewGrok(model)
	case "glm", "zhipu", "z.ai":
		return NewGLM(model)
	default:
		return nil, fmt.Errorf("unknown provider %q (want: mantle | llama | converse | anthropic | grok | glm)", provider)
	}
}

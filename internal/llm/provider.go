package llm

import (
	"fmt"
	"strings"
)

// New selects a provider by name. Empty defaults to mantle (Bedrock GPT-5.5).
// The (provider, model) pair is reconciled against the catalog first, so a
// known model is never dispatched to the wrong backend (which would 404).
func New(provider, model string) (Provider, error) {
	// Defensive: a model id that accidentally carried a Name() suffix like
	// "claude-opus-4-8 (bedrock converse)" (an old daemon model-switch bug
	// persisted such ids) is stripped back to the raw id so it resolves.
	if i := strings.Index(model, " ("); i > 0 && strings.HasSuffix(model, ")") {
		model = model[:i]
	}
	provider = ResolveProvider(provider, model)
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

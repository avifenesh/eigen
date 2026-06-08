package llm

import "fmt"

// New selects a provider by name. Empty defaults to mantle (Bedrock GPT-5.5).
func New(provider, model string) (Provider, error) {
	switch provider {
	case "", "mantle", "bedrock-mantle":
		return NewMantle(model)
	case "llama", "local":
		return NewLlama(model)
	case "converse", "bedrock-converse", "claude":
		return NewConverse(model)
	default:
		return nil, fmt.Errorf("unknown provider %q (want: mantle | llama | converse)", provider)
	}
}

package gui

import (
	"github.com/avifenesh/eigen/internal/llm"
)

// Routing bridge layer. Surfaces the model catalog and which providers are
// credentialed, so the GUI can show every route candidate and its capabilities.
// All reads are local + fast (catalog + env/credential checks); no network
// probe (that's the separate `eigen models` discovery path).

// ModelDTO mirrors llm.ModelInfo for the catalog view, plus a derived
// available flag (its provider is credentialed).
type ModelDTO struct {
	ID             string   `json:"id"`
	Provider       string   `json:"provider"`
	ContextWindow  int      `json:"contextWindow"`
	Cache          bool     `json:"cache"`
	Context1M      bool     `json:"context1m"`
	Reasoning      bool     `json:"reasoning"`
	Effort         string   `json:"effort,omitempty"`
	EffortLevels   []string `json:"effortLevels,omitempty"`
	ThinkingBudget int      `json:"thinkingBudget,omitempty"`
	Search         bool     `json:"search,omitempty"`
	Available      bool     `json:"available"`
}

// ProviderDTO is one provider and whether it's credentialed in this environment.
type ProviderDTO struct {
	Name        string `json:"name"`
	Credentialed bool  `json:"credentialed"`
	ModelCount  int    `json:"modelCount"`
}

// RoutingDTO is the routing/models snapshot: the catalog + provider status.
type RoutingDTO struct {
	Models    []ModelDTO    `json:"models"`
	Providers []ProviderDTO `json:"providers"`
}

// the provider universe eigen knows (canonical names).
var providerUniverse = []string{"mantle", "converse", "anthropic", "codex", "grok", "glm", "llama"}

// Routing returns the model catalog (each flagged with provider availability)
// and the provider credential status.
func (b *Bridge) Routing() (*RoutingDTO, error) {
	avail := map[string]bool{}
	for _, p := range providerUniverse {
		avail[p] = llm.ProviderAvailable(p)
	}

	models := llm.Models()
	out := make([]ModelDTO, 0, len(models))
	counts := map[string]int{}
	for _, m := range models {
		prov := llm.ResolveProvider(m.Provider, m.ID)
		counts[prov]++
		out = append(out, ModelDTO{
			ID: m.ID,
			// Emit the RESOLVED canonical provider so the view filters on the
			// same key the rail counts/labels by (raw aliases like "claude"
			// resolve to "converse"); avoids count/grid disagreement.
			Provider:       prov,
			ContextWindow:  m.ContextWindow,
			Cache:          m.Cache,
			Context1M:      m.Context1M,
			Reasoning:      m.Reasoning,
			Effort:         m.Effort,
			EffortLevels:   m.EffortLevels,
			ThinkingBudget: m.ThinkingBudget,
			Search:         m.Search,
			Available:      avail[prov],
		})
	}

	provs := make([]ProviderDTO, 0, len(providerUniverse))
	for _, p := range providerUniverse {
		provs = append(provs, ProviderDTO{Name: p, Credentialed: avail[p], ModelCount: counts[p]})
	}

	return &RoutingDTO{Models: out, Providers: provs}, nil
}

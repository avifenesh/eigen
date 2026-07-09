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
	Vision         bool     `json:"vision,omitempty"`
	Social         bool     `json:"social,omitempty"`
	Available      bool     `json:"available"`
}

// ProviderDTO is one provider and whether it's credentialed in this environment.
type ProviderDTO struct {
	Name         string `json:"name"`
	Credentialed bool   `json:"credentialed"`
	ModelCount   int    `json:"modelCount"`
}

// RoutingDTO is the routing/models snapshot: the catalog + provider status.
type RoutingDTO struct {
	Models    []ModelDTO    `json:"models"`
	Providers []ProviderDTO `json:"providers"`
}

// the provider universe eigen knows (canonical names).
var providerUniverse = []string{"mantle", "converse", "anthropic", "codex", "grok", "glm", "llama", "moa"}

// Routing returns the model catalog (each flagged with provider availability)
// and the provider credential status.
func (b *Bridge) Routing() (*RoutingDTO, error) {
	return routingSnapshot(), nil
}

func routingSnapshot() *RoutingDTO {
	avail := map[string]bool{}
	for _, p := range providerUniverse {
		avail[p] = llm.ProviderAvailable(p)
	}

	// availFor resolves credential status for any provider key — canonicalizing
	// first so an alias ("claude") hits the same entry as its backend
	// ("converse"), and probing on demand for genuinely custom providers not in
	// the canonical universe (so they aren't falsely shown as uncredentialed).
	availFor := func(p string) bool {
		c := llm.CanonicalProvider(p)
		if v, ok := avail[c]; ok {
			return v
		}
		v := llm.ProviderAvailable(c)
		avail[c] = v
		return v
	}

	models := llm.Models()
	out := make([]ModelDTO, 0, len(models))
	counts := map[string]int{}
	// Preserve first-seen order for any custom providers beyond the canonical set.
	var extraProvs []string
	seenProv := map[string]bool{}
	for _, p := range providerUniverse {
		seenProv[p] = true
	}
	for _, m := range models {
		// Canonicalize the resolved provider so the view filters, the rail
		// counts, and the credential check all key off the same backend name
		// (raw aliases like "claude" resolve to "converse").
		prov := llm.CanonicalProvider(llm.ResolveProvider(m.Provider, m.ID))
		counts[prov]++
		if !seenProv[prov] {
			seenProv[prov] = true
			extraProvs = append(extraProvs, prov)
		}
		out = append(out, ModelDTO{
			ID:             m.ID,
			Provider:       prov,
			ContextWindow:  m.ContextWindow,
			Cache:          m.Cache,
			Context1M:      m.Context1M,
			Reasoning:      m.Reasoning,
			Effort:         m.Effort,
			EffortLevels:   m.EffortLevels,
			ThinkingBudget: m.ThinkingBudget,
			Search:         m.Search,
			Vision:         m.Vision,
			Social:         m.Social,
			Available:      availFor(prov),
		})
	}

	provs := make([]ProviderDTO, 0, len(providerUniverse)+len(extraProvs))
	for _, p := range providerUniverse {
		provs = append(provs, ProviderDTO{Name: p, Credentialed: availFor(p), ModelCount: counts[p]})
	}
	// Surface custom providers (from ~/.eigen/providers.json) the universe omits.
	for _, p := range extraProvs {
		provs = append(provs, ProviderDTO{Name: p, Credentialed: availFor(p), ModelCount: counts[p]})
	}

	return &RoutingDTO{Models: out, Providers: provs}
}

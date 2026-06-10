package llm

import "testing"

func TestRouteCandidatesCurrentProviderOnly(t *testing.T) {
	// Empty allowlist → only the current provider's models (those that are
	// available). We can't assume creds in CI, so verify the FILTERING logic:
	// every returned id must belong to the current provider when allowlist empty.
	got := RouteCandidates("grok", nil)
	for _, id := range got {
		m, _ := Lookup(id)
		if canonicalProvider(m.Provider) != "grok" {
			t.Fatalf("empty allowlist must stay within current provider, got %s (%s)", id, m.Provider)
		}
	}
}

func TestRouteCandidatesAllowlistFilters(t *testing.T) {
	// With an allowlist, returned models belong to current ∪ allowed only.
	got := RouteCandidates("grok", []string{"glm"})
	for _, id := range got {
		m, _ := Lookup(id)
		cp := canonicalProvider(m.Provider)
		if cp != "grok" && cp != "glm" {
			t.Fatalf("allowlist {grok,glm} leaked %s (%s)", id, cp)
		}
	}
}

func TestProviderAvailableUnknown(t *testing.T) {
	if ProviderAvailable("nonexistent-provider") {
		t.Fatal("unknown provider must not be available")
	}
}

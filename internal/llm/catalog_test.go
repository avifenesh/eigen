package llm

import (
	"strings"
	"testing"
)

func TestContextWindowExact(t *testing.T) {
	if got := lookupWindow("openai.gpt-5.5"); got != 272000 {
		t.Fatalf("gpt-5.5 window = %d", got)
	}
	if got := lookupWindow("us.anthropic.claude-sonnet-5"); got != 200000 {
		t.Fatalf("sonnet window = %d", got)
	}
}

func TestContextWindowPrefix(t *testing.T) {
	// A more specific versioned id should still resolve via prefix match.
	if got := lookupWindow("us.anthropic.claude-opus-4-8-20250101"); got != 200000 {
		t.Fatalf("opus prefix window = %d", got)
	}
}

func TestContextWindowUnknown(t *testing.T) {
	if got := lookupWindow("some-unknown-model"); got != 0 {
		t.Fatalf("unknown window = %d, want 0", got)
	}
	if got := lookupWindow(""); got != 0 {
		t.Fatalf("empty window = %d, want 0", got)
	}
}

func TestDefaultModel(t *testing.T) {
	if DefaultModel("mantle") != "openai.gpt-5.5" {
		t.Fatal("mantle default wrong")
	}
	if DefaultModel("converse") != "us.anthropic.claude-opus-4-8" {
		t.Fatal("converse default wrong")
	}
	if DefaultModel("") != "openai.gpt-5.5" {
		t.Fatal("empty-provider default wrong")
	}
	if DefaultModel("codex") != "gpt-5.5" {
		t.Fatal("codex default wrong")
	}
	if DefaultModel("anthropic") != "claude-sonnet-4-5-20250929" {
		t.Fatal("native anthropic default wrong")
	}
	if DefaultModel("claude-code") != "claude-sonnet-4-5-20250929" {
		t.Fatal("claude-code default wrong")
	}
}

func TestModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	models := Models()
	if len(models) != len(Catalog) {
		t.Fatalf("Models() returned %d, want %d", len(models), len(Catalog))
	}
	// Must be a copy: mutating the result must not affect the catalog.
	if len(models) > 0 {
		models[0].ID = "tampered"
		if Catalog[0].ID == "tampered" {
			t.Fatal("Models() must return a copy, not the backing slice")
		}
	}
}

func TestLookupCapabilities(t *testing.T) {
	// Sonnet: caching + 1M context + extended thinking.
	s, ok := Lookup("us.anthropic.claude-sonnet-5")
	if !ok {
		t.Fatal("sonnet should be in the catalog")
	}
	if !s.Cache || !s.Context1M || s.ContextWindow1M != 1000000 || !s.Reasoning {
		t.Fatalf("sonnet capabilities wrong: %+v", s)
	}
	// Opus 4-8: same family of capabilities.
	o, ok := Lookup("us.anthropic.claude-opus-4-8")
	if !ok || !o.Cache || !o.Context1M {
		t.Fatalf("opus-4-8 capabilities wrong: %+v (ok=%v)", o, ok)
	}
	// Mantle GPT: effort-style reasoning, no cache/1M.
	g, ok := Lookup("openai.gpt-5.5")
	if !ok || !g.Reasoning || g.Effort != "medium" || g.Cache || g.Context1M || len(g.EffortLevels) != 5 || g.EffortLevels[4] != "xhigh" {
		t.Fatalf("gpt-5.5 capabilities wrong: %+v (ok=%v)", g, ok)
	}
	// llama local present.
	if l, ok := Lookup("local"); !ok || l.Provider != "llama" {
		t.Fatalf("llama local should be catalogued: %+v (ok=%v)", l, ok)
	}
	// Native Anthropic entries resolve to the native provider (not Bedrock
	// converse) — exact-id lookup must not collide with the us.anthropic.* ids.
	if a, ok := Lookup("claude-sonnet-4-5-20250929"); !ok || a.Provider != "anthropic" {
		t.Fatalf("native anthropic sonnet should map to provider anthropic: %+v (ok=%v)", a, ok)
	}
	if a, ok := Lookup("claude-opus-4-1-20250805"); !ok || a.Provider != "anthropic" || !a.Cache {
		t.Fatalf("native anthropic opus should map to provider anthropic: %+v (ok=%v)", a, ok)
	}
	// The Bedrock converse opus id still resolves to converse (no cross-contamination).
	if o, ok := Lookup("us.anthropic.claude-opus-4-8"); !ok || o.Provider != "converse" {
		t.Fatalf("bedrock opus must stay converse: %+v (ok=%v)", o, ok)
	}
}

func TestLookupPrefixAndUnknown(t *testing.T) {
	if m, ok := Lookup("us.anthropic.claude-sonnet-5-20990101"); !ok || !m.Cache {
		t.Fatalf("versioned id should prefix-match the catalogued model: %+v (ok=%v)", m, ok)
	}
	if _, ok := Lookup("totally-unknown"); ok {
		t.Fatal("unknown model should not match")
	}
	if _, ok := Lookup(""); ok {
		t.Fatal("empty model should not match")
	}
}

// TestLookupNoReversePrefix guards against the bug where a truncated query
// silently inherited a longer catalog id's capabilities (APP-048). A query that
// is a *prefix* of a catalog id (but not present itself) must NOT match — the
// only legitimate prefix direction is catalog-id-is-a-prefix-of-query.
func TestLookupNoReversePrefix(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// "gpt-5" is not a catalogued id; it must not inherit "gpt-5.5"/"gpt-5.4".
	if m, ok := Lookup("gpt-5"); ok {
		t.Fatalf("truncated query %q must not reverse-prefix-match a longer catalog id, got %+v", "gpt-5", m)
	}
	// "glm-4" must not inherit "glm-4.7"/"glm-4.6"/"glm-4.5".
	if m, ok := Lookup("glm-4"); ok {
		t.Fatalf("truncated query %q must not reverse-prefix-match a longer catalog id, got %+v", "glm-4", m)
	}
	// "openai.gpt-5" IS a catalogued id (mantle) and must still resolve exactly,
	// not be confused by the longer "openai.gpt-5.5"/"openai.gpt-5.4" siblings.
	if m, ok := Lookup("openai.gpt-5"); !ok || m.ID != "openai.gpt-5" {
		t.Fatalf("openai.gpt-5 should resolve to its own exact entry, got %+v (ok=%v)", m, ok)
	}
}

// TestLookupLongestPrefixWins ensures that when multiple catalog ids are
// prefixes of the query, the most specific (longest) id wins rather than the
// first iterated. A versioned "openai.gpt-5.5-2026..." must resolve to
// "openai.gpt-5.5" and not the shorter "openai.gpt-5".
func TestLookupLongestPrefixWins(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, ok := Lookup("openai.gpt-5.5-2026-01-01")
	if !ok || m.ID != "openai.gpt-5.5" {
		t.Fatalf("versioned gpt-5.5 should pick the longest prefix openai.gpt-5.5, got %+v (ok=%v)", m, ok)
	}
}

func TestGrokAndGLMCatalog(t *testing.T) {
	// grok-build: large window + live search.
	gb, ok := Lookup("grok-build")
	if !ok || gb.Provider != "grok" || !gb.Search || gb.ContextWindow != 512000 {
		t.Fatalf("grok-build catalog wrong: %+v (ok=%v)", gb, ok)
	}
	// composer: no backend search.
	gc, ok := Lookup("grok-composer-2.5-fast")
	if !ok || gc.Provider != "grok" || gc.Search {
		t.Fatalf("grok-composer catalog wrong: %+v (ok=%v)", gc, ok)
	}
	// glm coding models.
	gm, ok := Lookup("glm-4.6")
	if !ok || gm.Provider != "glm" {
		t.Fatalf("glm-4.6 catalog wrong: %+v (ok=%v)", gm, ok)
	}
	// New GLM lineup is present (5.1, 5, 5-turbo, 4.7).
	for _, id := range []string{"glm-5.1", "glm-5", "glm-5-turbo", "glm-4.7"} {
		if mi, ok := Lookup(id); !ok || mi.Provider != "glm" {
			t.Fatalf("expected %s in the glm catalog: %+v (ok=%v)", id, mi, ok)
		}
	}
	// Provider defaults.
	if DefaultModel("grok") != "grok-build" {
		t.Fatal("grok default should be grok-build")
	}
	if DefaultModel("glm") != "glm-5.2" {
		t.Fatal("glm default should be glm-5.2")
	}
}

func TestNewRegistersGrokAndGLM(t *testing.T) {
	t.Setenv("XAI_API_KEY", "xai-test")
	t.Setenv("GLM_API_KEY", "glm-test")
	if _, err := New("grok", "grok-build"); err != nil {
		t.Fatalf("New(grok) failed: %v", err)
	}
	if _, err := New("xai", "grok-build"); err != nil {
		t.Fatalf("New(xai) alias failed: %v", err)
	}
	if _, err := New("glm", "glm-4.6"); err != nil {
		t.Fatalf("New(glm) failed: %v", err)
	}
	if _, err := New("zhipu", "glm-4.6"); err != nil {
		t.Fatalf("New(zhipu) alias failed: %v", err)
	}
	if _, err := New("nonsense", ""); err == nil {
		t.Fatal("unknown provider should error")
	}
}

func TestResolveProviderReconcilesMismatch(t *testing.T) {
	// A converse-only model requested on mantle must be corrected to converse
	// (this is the mantle HTTP 404 "model does not exist" bug).
	if got := ResolveProvider("mantle", "us.anthropic.claude-opus-4-8"); got != "converse" {
		t.Fatalf("mantle+opus should reconcile to converse, got %q", got)
	}
	// A mantle model requested on converse corrects to mantle.
	if got := ResolveProvider("converse", "openai.gpt-5.5"); got != "mantle" {
		t.Fatalf("converse+gpt should reconcile to mantle, got %q", got)
	}
	// Matching pairs are untouched.
	if got := ResolveProvider("converse", "us.anthropic.claude-opus-4-8"); got != "converse" {
		t.Fatalf("matching pair should be unchanged, got %q", got)
	}
	// Aliases of the same backend are NOT flipped (claude == converse).
	if got := ResolveProvider("claude", "us.anthropic.claude-opus-4-8"); got != "claude" {
		t.Fatalf("alias of the same backend should be preserved, got %q", got)
	}
	// Unknown model leaves the requested provider alone.
	if got := ResolveProvider("mantle", "some-unknown-model"); got != "mantle" {
		t.Fatalf("unknown model should not flip provider, got %q", got)
	}
	// Empty model is returned unchanged for the caller's own defaulting.
	if got := ResolveProvider("glm", ""); got != "glm" {
		t.Fatalf("empty model should be unchanged, got %q", got)
	}
}

func TestNewReconcilesProviderModel(t *testing.T) {
	// New must dispatch to the converse backend even when asked for mantle,
	// because the opus model only exists on converse. Construction may fail on
	// missing AWS creds in a bare environment — but the error must come from the
	// converse path, never a mantle 404 for a converse model.
	p, err := New("mantle", "us.anthropic.claude-opus-4-8")
	if err != nil {
		if !contains(err.Error(), "converse") {
			t.Fatalf("reconciled construction should hit the converse path, got: %v", err)
		}
		return
	}
	if name := p.Name(); !contains(name, "converse") {
		t.Fatalf("reconciled provider should be converse, got %q", name)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

// lookupWindow is the test shim for the removed ContextWindow accessor: the
// catalog's standard window for a model id, 0 when unknown.
func lookupWindow(model string) int {
	if m, ok := Lookup(model); ok {
		return m.ContextWindow
	}
	return 0
}

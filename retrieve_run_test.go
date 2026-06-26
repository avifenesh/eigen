package main

import "testing"

// TestConfiguredEmbedderHonorsExplicitConfig pins the APP-006 fix: an embedder
// is only "present" when EIGEN_EMBED_BASE_URL is EXPLICITLY set. Previously the
// runner discarded llm.NewEmbedder's ok bool, and because NewEmbedder defaults
// the base URL to a local BGE service it always returned a non-nil embedder —
// so with no local service every retrieve hammered a dead localhost. With the
// var unset, configuredEmbedder must return nil (lexical-only).
func TestConfiguredEmbedderHonorsExplicitConfig(t *testing.T) {
	// Unset (no embedder configured) → nil, so retrieval stays lexical-only and
	// never points at the default localhost:8181 service.
	t.Setenv("EIGEN_EMBED_BASE_URL", "")
	if emb := configuredEmbedder(); emb != nil {
		t.Fatalf("unset EIGEN_EMBED_BASE_URL must yield nil embedder (lexical-only), got %T", emb)
	}

	// Explicitly configured → a real embedder, honoring NewEmbedder's bool.
	t.Setenv("EIGEN_EMBED_BASE_URL", "http://example.invalid:9999")
	t.Setenv("EIGEN_EMBED_MODEL", "my-embed")
	emb := configuredEmbedder()
	if emb == nil {
		t.Fatal("explicit EIGEN_EMBED_BASE_URL must yield a configured embedder")
	}
	if emb.ModelID() != "my-embed" {
		t.Fatalf("embedder model = %q, want my-embed", emb.ModelID())
	}
}

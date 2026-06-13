package llm

import "testing"

func TestCosineSim(t *testing.T) {
	a := []float32{1, 0, 0}
	if got := CosineSim(a, a); got < 0.999 {
		t.Fatalf("identical vectors should be ~1, got %f", got)
	}
	if got := CosineSim([]float32{1, 0}, []float32{0, 1}); got != 0 {
		t.Fatalf("orthogonal should be 0, got %f", got)
	}
	// closer direction scores higher
	q := []float32{1, 1, 0}
	near := []float32{1, 0.8, 0}
	far := []float32{0, 0, 1}
	if CosineSim(q, near) <= CosineSim(q, far) {
		t.Fatal("nearer vector should score higher")
	}
	// length mismatch / zero vector → 0 (no panic)
	if CosineSim([]float32{1}, []float32{1, 2}) != 0 {
		t.Fatal("length mismatch should be 0")
	}
	if CosineSim([]float32{0, 0}, []float32{1, 1}) != 0 {
		t.Fatal("zero vector should be 0")
	}
}

func TestNewEmbedderConfigurable(t *testing.T) {
	t.Setenv("EIGEN_EMBED_BASE_URL", "http://example.invalid:9999")
	t.Setenv("EIGEN_EMBED_MODEL", "my-embed")
	e, ok := NewEmbedder()
	if !ok {
		t.Fatal("embedder should construct (no probe at build time)")
	}
	if e.ModelID() != "my-embed" {
		t.Fatalf("model = %q", e.ModelID())
	}
}

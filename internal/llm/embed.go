package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

// Embedder turns text into vectors — the non-chat model kind behind semantic
// retrieval (Tier 18). Distinct from Provider (chat completions): an embedder
// only embeds. Backed first by a local llama.cpp --embedding server (the BGE
// service), OpenAI /v1/embeddings dialect.
type Embedder interface {
	// Embed returns one vector per input string, in order. All vectors share
	// Dims() length. A batch keeps round-trips down.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dims is the embedding dimensionality (e.g. 384 for bge-small).
	Dims() int
	// ModelID is the resolvable model id (for logs + index provenance: a vector
	// is only comparable to others from the SAME model).
	ModelID() string
}

// httpEmbedder is an OpenAI-compatible /v1/embeddings client (llama.cpp, or any
// server speaking that dialect).
type httpEmbedder struct {
	base   string
	model  string
	apiKey string
	dims   int
	http   *http.Client
}

// NewEmbedder builds the configured embedder, or (nil,false) when none is set
// up — retrieval is OPTIONAL, so callers degrade gracefully. Config:
//
//	EIGEN_EMBED_BASE_URL  (default http://127.0.0.1:8181 — the local BGE service)
//	EIGEN_EMBED_MODEL     (default bge-small-en-v1.5)
//	EIGEN_EMBED_API_KEY   (optional)
//
// It does NOT probe the server here (cheap construction); the first Embed call
// surfaces an unreachable server as an error the caller reports as
// "retrieval unavailable".
func NewEmbedder() (Embedder, bool) {
	base := strings.TrimRight(firstNonEmptyEnv("EIGEN_EMBED_BASE_URL", "http://127.0.0.1:8181"), "/")
	if base == "" {
		return nil, false
	}
	model := firstNonEmptyEnv("EIGEN_EMBED_MODEL", "bge-small-en-v1.5")
	return &httpEmbedder{
		base:   base,
		model:  model,
		apiKey: os.Getenv("EIGEN_EMBED_API_KEY"),
		http:   &http.Client{Timeout: 2 * time.Minute},
	}, true
}

func firstNonEmptyEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func (e *httpEmbedder) ModelID() string { return e.model }
func (e *httpEmbedder) Dims() int       { return e.dims }

type embedReq struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embedResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// maxEmbedBatch bounds how many texts go in one request (BGE servers have
// modest batch limits; keep requests small and predictable).
const maxEmbedBatch = 64

func (e *httpEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += maxEmbedBatch {
		end := start + maxEmbedBatch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (e *httpEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, _ := json.Marshal(embedReq{Input: texts, Model: e.model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w (is the embedder at %s running?)", err, e.base)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return nil, fmt.Errorf("embed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(buf.String()))
	}
	var out embedResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed: decode: %w", err)
	}
	// Order by Index (the API may not preserve input order).
	vecs := make([][]float32, len(out.Data))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) {
			continue
		}
		vecs[d.Index] = d.Embedding
	}
	for i, v := range vecs {
		if len(v) == 0 {
			return nil, fmt.Errorf("embed: missing vector for input %d", i)
		}
	}
	if e.dims == 0 && len(vecs) > 0 {
		e.dims = len(vecs[0])
	}
	return vecs, nil
}

// CosineSim returns the cosine similarity of two equal-length vectors (1 =
// identical direction, 0 = orthogonal). Returns 0 on length mismatch or a
// zero vector.
func CosineSim(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (sqrt32(na) * sqrt32(nb))
}

func sqrt32(x float32) float32 { return float32(math.Sqrt(float64(x))) }

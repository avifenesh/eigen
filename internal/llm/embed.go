package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

	// In-process result cache (the C3 "app-layer result cache"). Re-indexing
	// unchanged chunks and repeated query embeds hand us the SAME text over and
	// over; without this every call re-hits the embedding server. The cache is
	// process-local (no persistence) and bounded — see maxEmbedCacheEntries.
	cacheOn bool
	cacheMu sync.RWMutex // guards cache: Embed runs from parallel indexers
	cache   map[string][]float32

	// Atomic counters for cheap observability (CacheStats). Kept off the
	// mutex so reads never contend with embed traffic.
	cacheHits   int64
	cacheMisses int64
}

// NewEmbedder builds the configured embedder, or (nil,false) when none is set
// up — retrieval is OPTIONAL, so callers degrade gracefully. Config:
//
//	EIGEN_EMBED_BASE_URL  (default http://127.0.0.1:8181 — the local BGE service)
//	EIGEN_EMBED_MODEL     (default bge-small-en-v1.5)
//	EIGEN_EMBED_API_KEY   (optional)
//	EIGEN_EMBED_CACHE     ("off" disables the in-process result cache; default on)
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
	// Cache is on by default; an operator can disable it with EIGEN_EMBED_CACHE=off
	// (e.g. to measure raw server cost, or if memory is precious and embeds never
	// repeat). When off we skip both the lookup and the store.
	cacheOn := !strings.EqualFold(strings.TrimSpace(os.Getenv("EIGEN_EMBED_CACHE")), "off")
	return &httpEmbedder{
		base:    base,
		model:   model,
		apiKey:  os.Getenv("EIGEN_EMBED_API_KEY"),
		http:    &http.Client{Timeout: 2 * time.Minute},
		cacheOn: cacheOn,
		cache:   make(map[string][]float32),
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

// maxEmbedCacheEntries caps the in-process result cache. bge-small vectors are
// 384 float32 ≈ 1.5 KiB each, so 50k entries is on the order of ~75 MiB of
// vectors plus key overhead — a sane ceiling for a long-lived indexer. Eviction
// is deliberately coarse (drop the whole map on overflow, below) rather than
// LRU: the cache is a pure latency optimization, the embedding server is always
// the source of truth, and a clear is O(1) to reason about and cheap to write —
// not worth a heap/linked-list for a few rebuild misses after a flush.
const maxEmbedCacheEntries = 50000

func (e *httpEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out := make([][]float32, len(texts))

	// Fast path: caching disabled — behave exactly as before.
	if !e.cacheOn {
		return e.embedAll(ctx, texts)
	}

	// 1) Resolve cache hits and gather the unique misses. dedupe maps a text's
	// cache key to the slot in misses[] that will hold its fresh vector, so two
	// identical texts in one call embed ONCE and still both reassemble in order.
	type pending struct {
		idx int    // index into out[] to fill once the vector arrives
		key string // cache key (model-scoped)
	}
	var (
		misses []string // unique miss texts, in first-seen order, for embedBatch
		dedupe = map[string]int{}
		wants  []pending // every miss slot we must backfill, in input order
	)
	for i, t := range texts {
		key := e.cacheKey(t)
		if v, ok := e.cacheGet(key); ok {
			atomic.AddInt64(&e.cacheHits, 1)
			out[i] = v
			continue
		}
		atomic.AddInt64(&e.cacheMisses, 1)
		if _, seen := dedupe[key]; !seen {
			dedupe[key] = len(misses)
			misses = append(misses, t)
		}
		wants = append(wants, pending{idx: i, key: key})
	}

	// 2) Embed only the misses (still batched at maxEmbedBatch), then store each
	// fresh vector under its key and stitch it into every output slot that asked
	// for it — preserving the original input order.
	if len(misses) > 0 {
		fresh, err := e.embedAll(ctx, misses)
		if err != nil {
			return nil, err
		}
		// Store first so concurrent callers can hit these immediately.
		for j, t := range misses {
			e.cachePut(e.cacheKey(t), fresh[j])
		}
		for _, p := range wants {
			out[p.idx] = fresh[dedupe[p.key]]
		}
	}
	return out, nil
}

// embedAll runs texts through embedBatch in maxEmbedBatch-sized chunks and
// returns one vector per input, in order. Shared by the cached and cache-off
// paths so batching lives in exactly one place.
func (e *httpEmbedder) embedAll(ctx context.Context, texts []string) ([][]float32, error) {
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

// cacheKey hashes (model + NUL + text) to a hex string. The model id is folded
// IN ON PURPOSE: vectors are only comparable within a single embed model, so a
// key that ignored the model would let bge-small and a swapped-in model collide
// on identical text and return the wrong vector. The NUL separator keeps
// model/text boundaries unambiguous (no "ab"+"c" vs "a"+"bc" aliasing). sha256
// is plenty cheap here — embeds are network round-trips, not a tight inner loop.
func (e *httpEmbedder) cacheKey(text string) string {
	sum := sha256.Sum256([]byte(e.model + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

// cacheGet returns the cached vector for key under a read lock.
func (e *httpEmbedder) cacheGet(key string) ([]float32, bool) {
	e.cacheMu.RLock()
	v, ok := e.cache[key]
	e.cacheMu.RUnlock()
	return v, ok
}

// cachePut stores a vector under a write lock, applying the coarse size cap.
func (e *httpEmbedder) cachePut(key string, vec []float32) {
	e.cacheMu.Lock()
	if e.cache == nil { // lazy init guard (cache-off path never allocates)
		e.cache = make(map[string][]float32)
	}
	// Coarse eviction: at the cap, drop everything and start fresh rather than
	// tracking ages. See maxEmbedCacheEntries for why coarse is acceptable.
	if len(e.cache) >= maxEmbedCacheEntries {
		e.cache = make(map[string][]float32)
	}
	e.cache[key] = vec
	e.cacheMu.Unlock()
}

// CacheStats reports cumulative result-cache hits and misses for this embedder
// instance. Concrete method (NOT on the Embedder interface): httpEmbedder is the
// only implementation, but the counters are a diagnostic detail of THIS backend,
// so future embedders aren't forced to carry a cache they may not have.
func (e *httpEmbedder) CacheStats() (hits, misses int64) {
	return atomic.LoadInt64(&e.cacheHits), atomic.LoadInt64(&e.cacheMisses)
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

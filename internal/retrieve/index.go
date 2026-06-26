// Package retrieve provides semantic + lexical retrieval over a project's files
// (Tier 18 #2): a per-project on-disk index stores chunked file content, BM25
// ranks it lexically (always available — no embedder needed), and when an
// embedder is configured the chunks are also vectorized and search FUSES BM25
// with cosine similarity (Reciprocal Rank Fusion). Context is RETRIEVED on
// demand (the `retrieve` tool) instead of pasted whole — the main remaining
// token-efficiency lever.
//
// v1 scope: brute-force cosine (project scale = thousands of chunks, no ANN
// needed), line-window chunking with overlap (robust, no AST dependency),
// incremental by file mtime+size, lazy build on first retrieve. BM25 gives a
// working floor with zero setup. Deferred: reranker, session/memory indexing,
// AST chunking, ANN.
package retrieve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/avifenesh/eigen/internal/llm"
)

// chunkLines is the line-window size per chunk; chunkOverlap re-includes the
// tail of the previous window so a symbol split across a boundary still
// matches. Small windows suit BGE's modest context (512 tokens).
const (
	chunkLines    = 40
	chunkOverlap  = 10
	maxChunkBytes = 4000 // skip/trim pathological long-line chunks
	maxFiles      = 4000 // cap a huge repo's first index
	maxFileBytes  = 256 << 10
)

// Chunk is one indexed span of a file.
type Chunk struct {
	Path   string    `json:"path"`   // relative to the project root
	Start  int       `json:"start"`  // 1-based start line
	End    int       `json:"end"`    // inclusive end line
	Text   string    `json:"text"`   // the chunk content (for the result snippet)
	Vector []float32 `json:"vector"` // embedding (omitted from the snippet API)
}

// fileMeta tracks a file's indexed state for incremental re-embedding.
type fileMeta struct {
	ModTime int64 `json:"mtime"`
	Size    int64 `json:"size"`
}

// Index is a project's vector index, persisted under ~/.eigen/index/<hash>/.
type Index struct {
	root   string
	dir    string // index storage dir
	model  string // embedder model id (vectors only compare within one model)
	emb    llm.Embedder
	chunks []Chunk
	files  map[string]fileMeta
	bm25   *bm25Index // lexical index, lazily (re)built from chunks on Search
}

// Result is a retrieval hit.
type Result struct {
	Path    string
	Start   int
	End     int
	Snippet string
	Score   float32
}

// Open prepares the index for root. emb may be nil: with no embedder, the
// index still chunks every file and answers via BM25 lexical ranking (the
// always-available floor); with an embedder, chunks are also vectorized and
// Search fuses BM25 + cosine. The caller calls Sync() to (incrementally) bring
// it up to date before Search.
func Open(root string, emb llm.Embedder) (*Index, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	home, _ := os.UserHomeDir()
	h := sha256.Sum256([]byte(abs))
	// The embedder model is part of the index identity. With no embedder we use
	// a distinct "bm25" model tag so a lexical-only index and a vector index for
	// the same project never clobber each other's vectors on disk.
	model := "bm25"
	if emb != nil {
		model = emb.ModelID()
	}
	dir := filepath.Join(home, ".eigen", "index", hex.EncodeToString(h[:8]))
	idx := &Index{root: abs, dir: dir, model: model, emb: emb, files: map[string]fileMeta{}}
	idx.load() // best-effort; a missing/corrupt index just rebuilds
	return idx, nil
}

type persisted struct {
	Model  string              `json:"model"`
	Files  map[string]fileMeta `json:"files"`
	Chunks []Chunk             `json:"chunks"`
}

func (idx *Index) load() {
	data, err := os.ReadFile(filepath.Join(idx.dir, "index.json"))
	if err != nil {
		return
	}
	var p persisted
	if json.Unmarshal(data, &p) != nil {
		return
	}
	// A model change invalidates every vector (different space).
	if p.Model != idx.model {
		return
	}
	idx.files = p.Files
	idx.chunks = p.Chunks
	if idx.files == nil {
		idx.files = map[string]fileMeta{}
	}
}

func (idx *Index) save() error {
	if err := os.MkdirAll(idx.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(persisted{Model: idx.model, Files: idx.files, Chunks: idx.chunks})
	if err != nil {
		return err
	}
	tmp := filepath.Join(idx.dir, "index.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(idx.dir, "index.json"))
}

// Sync brings the index up to date with the project: it enumerates indexable
// files (gitignore-aware via ripgrep when available, else a bounded walk),
// re-embeds only files whose mtime/size changed, and drops chunks for deleted
// files. Bounded by ctx + caps. Returns the number of files (re)embedded.
func (idx *Index) Sync(ctx context.Context) (int, error) {
	files := idx.listFiles(ctx)
	live := make(map[string]bool, len(files))
	var changed []string
	for _, rel := range files {
		live[rel] = true
		fi, err := os.Stat(filepath.Join(idx.root, rel))
		if err != nil {
			continue
		}
		m := fileMeta{ModTime: fi.ModTime().Unix(), Size: fi.Size()}
		if prev, ok := idx.files[rel]; !ok || prev != m {
			changed = append(changed, rel)
		}
	}
	// Drop chunks for files that are gone or changed (changed ones re-added).
	if len(changed) > 0 || len(live) != len(idx.files) {
		keep := idx.chunks[:0:0]
		drop := map[string]bool{}
		for _, c := range changed {
			drop[c] = true
		}
		for _, c := range idx.chunks {
			if live[c.Path] && !drop[c.Path] {
				keep = append(keep, c)
			}
		}
		idx.chunks = keep
		idx.bm25 = nil // corpus changed → rebuild lexical index lazily on next Search
		for rel := range idx.files {
			if !live[rel] {
				delete(idx.files, rel)
			}
		}
	}
	embedded := 0
	for _, rel := range changed {
		if ctx.Err() != nil {
			break
		}
		if err := idx.embedFile(ctx, rel); err != nil {
			continue // skip unreadable/un-embeddable files; keep going
		}
		embedded++
	}
	if embedded > 0 || len(changed) > 0 {
		_ = idx.save()
	}
	return embedded, nil
}

// embedFile chunks one file, embeds the chunks, and adds them + updates meta.
func (idx *Index) embedFile(ctx context.Context, rel string) error {
	full := filepath.Join(idx.root, rel)
	fi, err := os.Stat(full)
	if err != nil {
		return err
	}
	if fi.Size() > maxFileBytes || fi.Size() == 0 {
		idx.files[rel] = fileMeta{ModTime: fi.ModTime().Unix(), Size: fi.Size()}
		return nil // skip huge/empty, but remember so we don't re-stat each Sync
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	if !looksTextual(data) {
		idx.files[rel] = fileMeta{ModTime: fi.ModTime().Unix(), Size: fi.Size()}
		return nil
	}
	chunks := chunkFile(rel, string(data))
	if len(chunks) == 0 {
		idx.files[rel] = fileMeta{ModTime: fi.ModTime().Unix(), Size: fi.Size()}
		return nil
	}
	// Embed only when an embedder is configured; otherwise the chunks carry no
	// vector and BM25 alone ranks them. If embedding FAILS (embedder configured
	// but unreachable — e.g. the default localhost service isn't running), index
	// the chunks lexically (no vector) rather than dropping the file, so BM25
	// still covers it. A later edit re-triggers an embed attempt.
	if idx.emb != nil {
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			// Prefix the path so the embedding captures location context.
			texts[i] = rel + "\n" + c.Text
		}
		if vecs, err := idx.emb.Embed(ctx, texts); err == nil && len(vecs) == len(chunks) {
			for i := range chunks {
				chunks[i].Vector = vecs[i]
			}
		}
	}
	idx.chunks = append(idx.chunks, chunks...)
	idx.bm25 = nil // corpus changed → rebuild lexical index lazily on next Search
	idx.files[rel] = fileMeta{ModTime: fi.ModTime().Unix(), Size: fi.Size()}
	return nil
}

// Search returns the top-k chunks for query, fusing lexical (BM25) and, when an
// embedder is configured, semantic (cosine) rankings via Reciprocal Rank Fusion
// (RRF). With no embedder it is pure BM25. RRF is rank-based, so it needs no
// score normalization between the two very different score scales. Results are
// re-validated against disk (a chunk from a since-edited file is dropped) so a
// hit always reflects current content.
func (idx *Index) Search(ctx context.Context, query string, k int) ([]Result, error) {
	if k <= 0 {
		k = 8
	}
	if len(idx.chunks) == 0 {
		return nil, nil
	}

	// Lexical ranking (always available): (re)build the BM25 index lazily.
	if idx.bm25 == nil {
		idx.bm25 = buildBM25(idx.chunks)
	}
	lexRank := idx.bm25.rank(query)

	// Semantic ranking (only when an embedder is configured AND chunks carry
	// vectors). An embedder failure here is non-fatal: fall back to BM25.
	var vecRank []int
	if idx.emb != nil {
		if qv, err := idx.emb.Embed(ctx, []string{query}); err == nil && len(qv) > 0 {
			type scored struct {
				i     int
				score float32
			}
			hits := make([]scored, 0, len(idx.chunks))
			for i, c := range idx.chunks {
				if len(c.Vector) == 0 {
					continue
				}
				hits = append(hits, scored{i, llm.CosineSim(qv[0], c.Vector)})
			}
			sort.Slice(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
			vecRank = make([]int, len(hits))
			for j, h := range hits {
				vecRank[j] = h.i
			}
		}
	}

	order, fused := fuseRRF(lexRank, vecRank, len(idx.chunks))
	out := make([]Result, 0, k)
	for _, ci := range order {
		if len(out) >= k {
			break
		}
		c := idx.chunks[ci]
		snip, ok := idx.validate(c)
		if !ok {
			continue // file changed/gone since indexing — skip stale hit
		}
		out = append(out, Result{Path: c.Path, Start: c.Start, End: c.End, Snippet: snip, Score: float32(fused[ci])})
	}
	return out, nil
}

// fuseRRF merges two ranked index lists by Reciprocal Rank Fusion: each list
// contributes 1/(rrfK+rank) to a chunk's fused score; chunks are then ordered
// by the sum. A chunk present in only one list still ranks. rrfK=60 is the
// standard constant. When vecRank is nil (no embedder), this returns lexRank
// order unchanged. Returns the ordering plus the fused score per chunk index.
func fuseRRF(lexRank, vecRank []int, n int) ([]int, map[int]float64) {
	const rrfK = 60.0
	score := make(map[int]float64, n)
	add := func(list []int) {
		for rank, ci := range list {
			score[ci] += 1.0 / (rrfK + float64(rank))
		}
	}
	add(lexRank)
	add(vecRank)
	order := make([]int, 0, len(score))
	for ci := range score {
		order = append(order, ci)
	}
	sort.Slice(order, func(a, b int) bool {
		sa, sb := score[order[a]], score[order[b]]
		if sa != sb {
			return sa > sb
		}
		return order[a] < order[b] // stable tiebreak by chunk index
	})
	return order, score
}

// validate re-reads the chunk's lines from disk; returns the current text and
// false if the file is gone or too short now (stale chunk).
func (idx *Index) validate(c Chunk) (string, bool) {
	data, err := os.ReadFile(filepath.Join(idx.root, c.Path))
	if err != nil {
		return "", false
	}
	lines := strings.Split(string(data), "\n")
	if c.Start < 1 || c.End > len(lines) || c.Start > c.End {
		return "", false
	}
	return strings.Join(lines[c.Start-1:c.End], "\n"), true
}

// listFiles enumerates indexable files relative to root: ripgrep --files
// (gitignore-aware) when available, else a bounded walk. Capped at maxFiles.
func (idx *Index) listFiles(ctx context.Context) []string {
	if rel := idx.ripgrepFiles(ctx); rel != nil {
		return rel
	}
	return idx.walkFiles()
}

func (idx *Index) ripgrepFiles(ctx context.Context) []string {
	rg, err := exec.LookPath("rg")
	if err != nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, rg, "--files", "--", idx.root)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var rel []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r, err := filepath.Rel(idx.root, line)
		if err != nil || strings.HasPrefix(r, "..") {
			continue
		}
		if denied(r) {
			continue
		}
		rel = append(rel, r)
		if len(rel) >= maxFiles {
			break
		}
	}
	return rel
}

func (idx *Index) walkFiles() []string {
	var rel []string
	_ = filepath.WalkDir(idx.root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		r, e := filepath.Rel(idx.root, p)
		if e != nil || denied(r) {
			return nil
		}
		rel = append(rel, r)
		if len(rel) >= maxFiles {
			return filepath.SkipAll
		}
		return nil
	})
	return rel
}

func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "target", "dist", "build", ".eigen":
		return true
	}
	return false
}

// denied excludes secrets and binary-ish paths from the index. The index is
// read back and embedded into snippets, so any secret material that slips in
// here leaks — hence the broad coverage of common credential files (keys,
// PKCS#12 bundles, .env/.envrc, .netrc, cloud/SSH credential stores).
func denied(rel string) bool {
	low := strings.ToLower(rel)
	// Normalize to forward slashes so segment matching works regardless of the
	// OS path separator (e.g. a Windows-style "a\\.ssh\\id_rsa").
	low = strings.ReplaceAll(low, "\\", "/")
	for _, seg := range strings.Split(low, "/") {
		switch seg {
		case ".git", ".ssh", ".aws", ".gnupg", "node_modules", "vendor", ".eigen":
			return true
		}
	}
	switch filepath.Ext(low) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".pdf", ".zip", ".gz", ".tar",
		".bin", ".exe", ".so", ".dylib", ".o", ".a", ".wasm", ".mp4", ".mp3", ".lock",
		".pem", ".key", ".p12", ".pfx":
		return true
	}
	base := path.Base(low)
	switch base {
	case ".env", ".envrc", ".netrc", "credentials":
		return true
	}
	return strings.HasPrefix(base, ".env.") ||
		strings.HasPrefix(base, "id_rsa") ||
		strings.HasPrefix(base, "id_ed25519")
}

// chunkFile splits content into overlapping line windows.
func chunkFile(rel, content string) []Chunk {
	lines := strings.Split(content, "\n")
	var chunks []Chunk
	step := chunkLines - chunkOverlap
	if step < 1 {
		step = chunkLines
	}
	for start := 0; start < len(lines); start += step {
		end := start + chunkLines
		if end > len(lines) {
			end = len(lines)
		}
		text := strings.Join(lines[start:end], "\n")
		if strings.TrimSpace(text) == "" {
			if end == len(lines) {
				break
			}
			continue
		}
		if len(text) > maxChunkBytes {
			text = text[:maxChunkBytes]
		}
		chunks = append(chunks, Chunk{Path: rel, Start: start + 1, End: end, Text: text})
		if end == len(lines) {
			break
		}
	}
	return chunks
}

// looksTextual rejects binary files (a NUL byte in the head is the signal).
func looksTextual(data []byte) bool {
	n := len(data)
	if n > 1024 {
		n = 1024
	}
	for _, b := range data[:n] {
		if b == 0 {
			return false
		}
	}
	return true
}

// Len reports how many chunks are currently indexed (0 = nothing to search).
func (idx *Index) Len() int { return len(idx.chunks) }

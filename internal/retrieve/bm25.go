package retrieve

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25 lexical ranking over the indexed chunks. It is the always-available
// retrieval floor: unlike the embedder (optional, needs a running service),
// BM25 needs nothing but the chunk text already on disk, so `retrieve` works
// even with no embedder — and where vectors DO exist, BM25 is fused with cosine
// (see Search) so exact-term hits and semantic hits reinforce each other.
//
// Standard Okapi BM25 with the usual defaults.
const (
	bm25K1 = 1.2  // term-frequency saturation
	bm25B  = 0.75 // length normalization
)

// tokenize lowercases text and splits it into search terms. Code-aware: it
// breaks on non-alphanumerics AND on camelCase / snake_case boundaries, then
// keeps BOTH the sub-tokens and the original joined identifier, so a query for
// "context tokens" matches a chunk containing maxContextTokens, and a query for
// the exact identifier still matches too. Tokens shorter than 2 runes are
// dropped (noise).
func tokenize(text string) []string {
	var out []string
	emit := func(tok string) {
		if len([]rune(tok)) >= 2 {
			out = append(out, strings.ToLower(tok))
		}
	}
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		w := word.String()
		word.Reset()
		emit(w)
		// Split the identifier into sub-words on camelCase boundaries; emit
		// each sub-word too (only when it actually splits, to avoid dupes).
		if subs := splitIdentifier(w); len(subs) > 1 {
			for _, s := range subs {
				emit(s)
			}
		}
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
		} else {
			flush() // separator (space, punctuation, _, etc.)
		}
	}
	flush()
	return out
}

// splitIdentifier breaks camelCase / PascalCase into sub-words: "maxContextTokens"
// -> [max context tokens], "HTTPServer" -> [http server]. (snake_case is already
// split by the non-alphanumeric tokenizer.)
func splitIdentifier(w string) []string {
	r := []rune(w)
	var subs []string
	start := 0
	for i := 1; i < len(r); i++ {
		prev, cur := r[i-1], r[i]
		// boundary: lower/digit -> Upper (maxC), or Upper run -> Upper+lower (HTTPServer -> HTTP|Server)
		boundary := (unicode.IsLower(prev) || unicode.IsDigit(prev)) && unicode.IsUpper(cur)
		if !boundary && i+1 < len(r) {
			boundary = unicode.IsUpper(prev) && unicode.IsUpper(cur) && unicode.IsLower(r[i+1])
		}
		if boundary {
			subs = append(subs, string(r[start:i]))
			start = i
		}
	}
	subs = append(subs, string(r[start:]))
	return subs
}

// bm25Index is a lexical index over a chunk set: per-chunk term frequencies,
// document frequencies, and length stats. Rebuilt from the chunk slice (cheap:
// it just re-tokenizes the in-memory chunk text), so it always reflects the
// current corpus without separate persistence.
type bm25Index struct {
	tf     []map[string]int // term frequencies per chunk (index-aligned with chunks)
	docLen []int            // token count per chunk
	df     map[string]int   // document frequency per term
	avgLen float64
	n      int
}

// buildBM25 tokenizes every chunk's path+text into the lexical index.
func buildBM25(chunks []Chunk) *bm25Index {
	bi := &bm25Index{df: map[string]int{}, n: len(chunks)}
	bi.tf = make([]map[string]int, len(chunks))
	bi.docLen = make([]int, len(chunks))
	total := 0
	for i, c := range chunks {
		// Include the path so a query naming a file/dir biases toward it.
		toks := tokenize(c.Path + " " + c.Text)
		freq := make(map[string]int, len(toks))
		for _, t := range toks {
			freq[t]++
		}
		bi.tf[i] = freq
		bi.docLen[i] = len(toks)
		total += len(toks)
		for t := range freq {
			bi.df[t]++
		}
	}
	if bi.n > 0 {
		bi.avgLen = float64(total) / float64(bi.n)
	}
	return bi
}

// score returns the BM25 score of chunk i for the query terms. Zero when no
// query term occurs in the chunk.
func (bi *bm25Index) score(i int, queryTerms []string) float64 {
	if i < 0 || i >= bi.n || bi.docLen[i] == 0 || bi.avgLen == 0 {
		return 0
	}
	tf := bi.tf[i]
	dl := float64(bi.docLen[i])
	var s float64
	for _, t := range queryTerms {
		f := tf[t]
		if f == 0 {
			continue
		}
		df := bi.df[t]
		if df == 0 {
			continue
		}
		// idf with the standard +0.5 smoothing (clamped non-negative).
		idf := math.Log(1 + (float64(bi.n)-float64(df)+0.5)/(float64(df)+0.5))
		num := float64(f) * (bm25K1 + 1)
		den := float64(f) + bm25K1*(1-bm25B+bm25B*dl/bi.avgLen)
		s += idf * num / den
	}
	return s
}

// rank returns chunk indices ordered by BM25 score (descending), excluding
// zero-score chunks. Used standalone (no embedder) and as one input to RRF.
func (bi *bm25Index) rank(query string) []int {
	terms := tokenize(query)
	if len(terms) == 0 || bi.n == 0 {
		return nil
	}
	type sc struct {
		i int
		s float64
	}
	scored := make([]sc, 0, bi.n)
	for i := 0; i < bi.n; i++ {
		if s := bi.score(i, terms); s > 0 {
			scored = append(scored, sc{i, s})
		}
	}
	sort.Slice(scored, func(a, b int) bool { return scored[a].s > scored[b].s })
	out := make([]int, len(scored))
	for j, x := range scored {
		out[j] = x.i
	}
	return out
}

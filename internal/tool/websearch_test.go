package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// clearSearchEnv unsets every backend/opt-in env var so tests start clean.
func clearSearchEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"TAVILY_API_KEY", "BRAVE_API_KEY", "EIGEN_WEBSEARCH_URL", "EIGEN_WEBSEARCH_KEY",
		"EIGEN_TAVILY_URL", "EIGEN_BRAVE_URL", "EIGEN_SEARXNG_URL", "EIGEN_WEBSEARCH_NO_MOJEEK",
		"EIGEN_WEBSEARCH_NO_DUCKDUCKGO",
		"EIGEN_WEBSEARCH_ALLOW_LOOPBACK", "EIGEN_WEBSEARCH_ALLOW_PRIVATE",
	} {
		t.Setenv(k, "")
	}
}

// fakeEngine is a scriptable engine for chain tests.
type fakeEngine struct {
	n    string
	cls  engineClass
	res  []searchResult
	err  error
	hits *int // increment on call
}

func (f *fakeEngine) name() string       { return f.n }
func (f *fakeEngine) class() engineClass { return f.cls }
func (f *fakeEngine) host() string       { return "example.com" }
func (f *fakeEngine) search(_ context.Context, _ *http.Client, _ string, _ int) ([]searchResult, error) {
	if f.hits != nil {
		*f.hits++
	}
	return f.res, f.err
}

func chainResults(t *testing.T, c *searchChain, count int) ([]searchResult, error) {
	t.Helper()
	return c.run(context.Background(), http.DefaultClient, "q", count)
}

// --- WebSearch tool surface --------------------------------------------------

func TestWebSearchAlwaysAvailable(t *testing.T) {
	clearSearchEnv(t)
	def := WebSearch() // no env: still returns the tool (keyless chain)
	if def.Name != "websearch" {
		t.Fatalf("WebSearch must always return the tool, got %q", def.Name)
	}
}

func TestWebSearchEmptyQuery(t *testing.T) {
	clearSearchEnv(t)
	def := WebSearch()
	args, _ := json.Marshal(map[string]any{"query": "   "})
	if _, err := def.Run(context.Background(), args); err == nil {
		t.Fatal("empty query should error")
	}
}

// --- chain semantics ---------------------------------------------------------

func TestChainFastPathFirstEngineSufficient(t *testing.T) {
	var h2 int
	c := &searchChain{engines: []searchEngine{
		&fakeEngine{n: "a", cls: classGeneral, res: []searchResult{
			{Title: "1", URL: "https://x/1"}, {Title: "2", URL: "https://x/2"},
		}},
		&fakeEngine{n: "b", cls: classGeneral, res: []searchResult{{Title: "3", URL: "https://x/3"}}, hits: &h2},
	}}
	got, err := chainResults(t, c, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("fast path should return the first engine's 2 results, got %d (%v)", len(got), err)
	}
	if h2 != 0 {
		t.Fatal("a sufficient first engine must short-circuit (engine b not called)")
	}
}

func TestChainGathersAcrossEnginesWithDedup(t *testing.T) {
	c := &searchChain{engines: []searchEngine{
		&fakeEngine{n: "a", cls: classNiche, res: []searchResult{{Title: "A", URL: "https://x/1"}}},
		&fakeEngine{n: "b", cls: classVertical, res: []searchResult{
			{Title: "dup", URL: "https://x/1?utm_source=z"}, // dups #1 after normalization
			{Title: "B", URL: "https://x/2"},
		}},
	}}
	got, err := chainResults(t, c, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("should accumulate + dedup to 2 unique URLs, got %d: %+v", len(got), got)
	}
	if got[0].URL != "https://x/1" || got[1].URL != "https://x/2" {
		t.Fatalf("first engine owns the row; order preserved: %+v", got)
	}
}

func TestChainFailureIsolation(t *testing.T) {
	c := &searchChain{engines: []searchEngine{
		&fakeEngine{n: "a", cls: classGeneral, err: context.DeadlineExceeded},
		&fakeEngine{n: "b", cls: classNiche, res: []searchResult{{Title: "B", URL: "https://x/2"}}},
	}}
	got, err := chainResults(t, c, 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("a failed engine should be skipped, the next serves: got %d (%v)", len(got), err)
	}
}

func TestChainGeneralEmptyIsAuthoritative(t *testing.T) {
	var h2 int
	c := &searchChain{engines: []searchEngine{
		&fakeEngine{n: "a", cls: classGeneral, res: nil}, // general engine: "the web had nothing"
		&fakeEngine{n: "b", cls: classVertical, res: []searchResult{{Title: "B", URL: "https://x/2"}}, hits: &h2},
	}}
	got, err := chainResults(t, c, 5)
	if err != nil || len(got) != 0 {
		t.Fatalf("a general engine's empty is authoritative (no results, no error): got %d (%v)", len(got), err)
	}
	if h2 != 0 {
		t.Fatal("the chain should stop after a general-empty (vertical engine not called)")
	}
}

func TestChainDegradedEmptySurfacesError(t *testing.T) {
	// General engine ERRORS, only a vertical engine returns empty → degraded:
	// surface the error so the model retries (don't trust an encyclopedic-empty).
	c := &searchChain{engines: []searchEngine{
		&fakeEngine{n: "a", cls: classGeneral, err: context.DeadlineExceeded},
		&fakeEngine{n: "b", cls: classVertical, res: nil},
	}}
	_, err := chainResults(t, c, 5)
	if err == nil {
		t.Fatal("a general failure + vertical-only empty should surface an error (degraded), not silent empty")
	}
}

// --- engine parsers ----------------------------------------------------------

func TestMarginaliaParsesAgainstFixtureServer(t *testing.T) {
	clearSearchEnv(t)
	t.Setenv("EIGEN_WEBSEARCH_ALLOW_LOOPBACK", "1") // httptest is 127.0.0.1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/public/search/") {
			t.Errorf("marginalia path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev","description":"the <b>Go</b> language","quality":3.2}]}`))
	}))
	defer srv.Close()
	e := &marginaliaEngine{base: srv.URL}
	c := &searchChain{engines: []searchEngine{e}, checkSSRF: ssrfCheck}
	got, err := chainResults(t, c, 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("marginalia: %v %+v", err, got)
	}
	if got[0].URL != "https://go.dev" || got[0].Snippet != "the Go language" {
		t.Fatalf("marginalia parse (tags stripped): %+v", got[0])
	}
}

func TestWikipediaParsesAgainstFixtureServer(t *testing.T) {
	clearSearchEnv(t)
	t.Setenv("EIGEN_WEBSEARCH_ALLOW_LOOPBACK", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"query":{"search":[{"title":"Eigenvalue","pageid":42,"snippet":"a <span>scalar</span>"}]}}`))
	}))
	defer srv.Close()
	e := &wikipediaEngine{base: srv.URL}
	c := &searchChain{engines: []searchEngine{e}, checkSSRF: ssrfCheck}
	got, err := chainResults(t, c, 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("wikipedia: %v %+v", err, got)
	}
	if !strings.Contains(got[0].URL, "curid=42") || got[0].Snippet != "a scalar" {
		t.Fatalf("wikipedia parse: %+v", got[0])
	}
}

func TestMojeekParsesSERPBlocks(t *testing.T) {
	html := `<ul class="results-standard">
<!--rs--><li><a class="title" href="https://ex.com/a">A Title</a><p class="s">snippet <strong>a</strong></p></li><!--re-->
<!--rs--><li><a class="title" href="https://ex.com/?x=1&amp;y=2">B</a><p class="s">snippet b</p></li><!--re-->
</ul>`
	got := parseMojeek(html)
	if len(got) != 2 {
		t.Fatalf("mojeek should parse 2 blocks, got %d", len(got))
	}
	if got[0].URL != "https://ex.com/a" || got[0].Title != "A Title" || got[0].Snippet != "snippet a" {
		t.Fatalf("mojeek block 0: %+v", got[0])
	}
	if got[1].URL != "https://ex.com/?x=1&y=2" {
		t.Fatalf("mojeek should decode &amp; in href: %q", got[1].URL)
	}
}

func TestMojeekChallengeDetection(t *testing.T) {
	if mojeekChallenged(`<ul class="results-standard"></ul>`) {
		t.Error("a real (even empty) SERP carries the scaffold — not a challenge")
	}
	if !mojeekChallenged(`<html><body>verify you are human</body></html>`) {
		t.Error("an interstitial with no scaffold is a challenge")
	}
}

// --- SSRF --------------------------------------------------------------------

func TestSSRFBlocksLoopbackUnlessOptedIn(t *testing.T) {
	clearSearchEnv(t)
	if err := ssrfCheck("127.0.0.1"); err == nil {
		t.Fatal("loopback must be blocked by default")
	}
	t.Setenv("EIGEN_WEBSEARCH_ALLOW_LOOPBACK", "1")
	if err := ssrfCheck("127.0.0.1"); err != nil {
		t.Fatalf("loopback allowed when opted in: %v", err)
	}
}

func TestSSRFBlocksPrivateAndMetadata(t *testing.T) {
	clearSearchEnv(t)
	for _, ip := range []string{"10.0.0.1", "192.168.1.1", "172.16.0.1", "169.254.169.254", "::1"} {
		if err := ssrfCheck(ip); err == nil {
			t.Errorf("%s must be blocked by default", ip)
		}
	}
	// Metadata/link-local is never opt-in-able via ALLOW_PRIVATE... actually
	// link-local IS under ALLOW_PRIVATE; just assert public passes.
	if err := ssrfCheck("8.8.8.8"); err != nil {
		t.Errorf("a public IP should pass: %v", err)
	}
}

func TestNormalizeURLDedupKey(t *testing.T) {
	cases := [][2]string{
		{"https://WWW.Example.com/a/", "https://example.com/a"},
		{"http://example.com:80/a?utm_source=x&id=7", "http://example.com/a?id=7"},
		{"https://x.com/p#frag", "https://x.com/p"},
	}
	for _, c := range cases {
		if got := normalizeURL(c[0]); got != c[1] {
			t.Errorf("normalizeURL(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// --- keyed heads still work (against fixture servers) ------------------------

func TestTavilyHeadAgainstFixtureServer(t *testing.T) {
	clearSearchEnv(t)
	t.Setenv("EIGEN_WEBSEARCH_ALLOW_LOOPBACK", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("tavily should POST, got %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev","content":"The Go language."},{"title":"Docs","url":"https://go.dev/doc","content":"Docs."}]}`))
	}))
	defer srv.Close()
	e := &tavilyBackend{key: "k", base: srv.URL}
	c := &searchChain{engines: []searchEngine{e}, checkSSRF: ssrfCheck}
	got, err := chainResults(t, c, 5)
	if err != nil || len(got) != 2 || got[0].URL != "https://go.dev" {
		t.Fatalf("tavily head: %v %+v", err, got)
	}
}

func TestGenericHeadParsesShapes(t *testing.T) {
	clearSearchEnv(t)
	t.Setenv("EIGEN_WEBSEARCH_ALLOW_LOOPBACK", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"title":"A","link":"https://a.test","snippet":"alpha"},{"name":"B","href":"https://b.test","description":"beta"}]`))
	}))
	defer srv.Close()
	e := &genericBackend{template: srv.URL}
	c := &searchChain{engines: []searchEngine{e}, checkSSRF: ssrfCheck}
	got, err := chainResults(t, c, 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("generic head: %v %+v", err, got)
	}
	if got[0].URL != "https://a.test" || got[1].URL != "https://b.test" {
		t.Fatalf("generic shapes: %+v", got)
	}
}

func TestBuildChainKeylessByDefault(t *testing.T) {
	clearSearchEnv(t)
	c := buildSearchChain()
	names := make([]string, 0, len(c.engines))
	for _, e := range c.engines {
		names = append(names, e.name())
	}
	want := []string{"mojeek", "duckduckgo", "marginalia", "wikipedia"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("default chain should be the keyless tail %v, got %v", want, names)
	}
	// A keyed head goes first.
	t.Setenv("BRAVE_API_KEY", "bk")
	c = buildSearchChain()
	if c.engines[0].name() != "brave" {
		t.Fatalf("a Brave key should be the head, got %q", c.engines[0].name())
	}
}

func TestBuildChainDuckDuckGoOptOut(t *testing.T) {
	clearSearchEnv(t)
	t.Setenv("EIGEN_WEBSEARCH_NO_DUCKDUCKGO", "1")
	for _, e := range buildSearchChain().engines {
		if e.name() == "duckduckgo" {
			t.Fatal("EIGEN_WEBSEARCH_NO_DUCKDUCKGO should drop DuckDuckGo from the chain")
		}
	}
}

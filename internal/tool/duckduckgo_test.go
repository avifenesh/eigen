package tool

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const ddgFixture = `<html><body>
<div class="result results_links results_links_deep web-result">
 <div class="links_main">
  <h2 class="result__title">
   <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2Fcontext&amp;rut=abc">context package - pkg.go.dev</a>
  </h2>
  <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fpkg.go.dev%2Fcontext&amp;rut=abc">Package context defines the Context type, which carries deadlines.</a>
 </div>
</div>
<div class="result results_links">
 <div class="links_main">
  <a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fstackoverflow.com%2Fq%2F76193188&amp;rut=def">request context deadline exceeded</a>
  <a class="result__snippet" href="x">I keep getting context deadline exceeded.</a>
 </div>
</div>
</body></html>`

func TestDuckDuckGoParsesFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(ddgFixture))
	}))
	defer srv.Close()
	e := &duckduckgoEngine{base: srv.URL}
	res, err := e.search(context.Background(), http.DefaultClient, "golang context deadline", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 results, got %d: %+v", len(res), res)
	}
	if res[0].URL != "https://pkg.go.dev/context" {
		t.Fatalf("uddg redirect should be unwrapped to the real URL, got %q", res[0].URL)
	}
	if !strings.Contains(res[0].Title, "pkg.go.dev") || !strings.Contains(res[0].Snippet, "deadlines") {
		t.Fatalf("title/snippet wrong: %+v", res[0])
	}
	if res[1].URL != "https://stackoverflow.com/q/76193188" {
		t.Fatalf("second URL wrong: %q", res[1].URL)
	}
}

func TestUnwrapDDGRedirect(t *testing.T) {
	cases := map[string]string{
		"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa&rut=x":     "https://example.com/a",
		"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fa&amp;rut=x": "https://example.com/a",
		"//example.com/direct":    "https://example.com/direct",
		"https://plain.example/x": "https://plain.example/x",
		"":                        "",
	}
	for in, want := range cases {
		if got := unwrapDDGRedirect(in); got != want {
			t.Errorf("unwrapDDGRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMojeekRateLimitedDuckDuckGoServes(t *testing.T) {
	// The key new behavior: Mojeek 403 → DuckDuckGo (also GENERAL) serves,
	// instead of dropping to niche/encyclopedic only.
	c := &searchChain{engines: []searchEngine{
		&fakeEngine{n: "mojeek", cls: classGeneral, err: errors.New("mojeek unavailable (HTTP 403; rate-limited or bot-blocked)")},
		&fakeEngine{n: "duckduckgo", cls: classGeneral, res: []searchResult{{Title: "DDG", URL: "https://d"}}},
		&fakeEngine{n: "wikipedia", cls: classVertical, res: []searchResult{{Title: "W", URL: "https://w"}}},
	}}
	got, err := chainResults(t, c, 5)
	if err != nil {
		t.Fatalf("mojeek 403 + DDG working must serve results: %v", err)
	}
	// DDG (general) result should be present.
	if len(got) == 0 || got[0].URL != "https://d" {
		t.Fatalf("DuckDuckGo should serve after a rate-limited Mojeek, got %+v", got)
	}
}

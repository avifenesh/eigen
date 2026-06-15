package tool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// braveFixture mirrors search.brave.com's stable structure: result anchors with
// a " l1" class + target="_self" + https href, each followed by a
// search-snippet-title element carrying the title in a title="…" attribute.
// Svelte hash classes (svelte-abc123) are deliberately varied to prove we don't
// depend on them.
const braveFixture = `<html><body>
<div class="snippet svelte-aaa">
  <a href="https://pkg.go.dev/context" target="_self" class="svelte-xyz111 l1">
    <div class="site-name-wrapper svelte-q"><span>pkg.go.dev</span></div>
  </a>
  <div class="title search-snippet-title line-clamp-1 svelte-xyz111" title="context package - context - Go &amp; Packages">context package</div>
</div>
<div class="snippet svelte-bbb">
  <a href="https://stackoverflow.com/q/76193188" target="_self" class="svelte-zzz222 l1">
    <div class="site-name-wrapper"><span>stackoverflow.com</span></div>
  </a>
  <div class="title search-snippet-title svelte-zzz222" title="Request Context Deadline Exceeded">SO</div>
</div>
<a href="/settings" target="_self" class="svelte-x l1">internal link (no https) — skipped</a>
</body></html>`

func TestBraveWebParsesStableTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(braveFixture))
	}))
	defer srv.Close()
	e := &braveWebEngine{base: srv.URL}
	res, err := e.search(context.Background(), http.DefaultClient, "golang context deadline", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("want 2 results (internal /settings link skipped), got %d: %+v", len(res), res)
	}
	if res[0].URL != "https://pkg.go.dev/context" || res[0].Title != "context package - context - Go & Packages" {
		t.Fatalf("first result wrong (title entity should be decoded): %+v", res[0])
	}
	if res[1].URL != "https://stackoverflow.com/q/76193188" {
		t.Fatalf("second URL wrong: %q", res[1].URL)
	}
}

package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// clearSearchEnv unsets every backend env var so tests start from a known state.
func clearSearchEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"TAVILY_API_KEY", "BRAVE_API_KEY", "EIGEN_WEBSEARCH_URL", "EIGEN_WEBSEARCH_KEY",
		"EIGEN_TAVILY_URL", "EIGEN_BRAVE_URL",
	} {
		t.Setenv(k, "")
	}
}

func runSearch(t *testing.T, query string) (string, error) {
	t.Helper()
	def, ok := WebSearch()
	if !ok {
		t.Fatal("WebSearch should be configured")
	}
	args, _ := json.Marshal(map[string]any{"query": query})
	return def.Run(context.Background(), args)
}

func TestWebSearchUnconfigured(t *testing.T) {
	clearSearchEnv(t)
	if _, ok := WebSearch(); ok {
		t.Fatal("WebSearch must not be available with no backend configured")
	}
}

func TestWebSearchTavily(t *testing.T) {
	clearSearchEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("tavily should POST, got %s", r.Method)
		}
		_, _ = w.Write([]byte(`{"results":[
			{"title":"Go","url":"https://go.dev","content":"The Go language."},
			{"title":"Docs","url":"https://go.dev/doc","content":"Docs."}
		]}`))
	}))
	defer srv.Close()
	t.Setenv("TAVILY_API_KEY", "k")
	t.Setenv("EIGEN_TAVILY_URL", srv.URL)

	out, err := runSearch(t, "go language")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "https://go.dev") || !strings.Contains(out, "The Go language.") {
		t.Fatalf("tavily result missing content:\n%s", out)
	}
}

func TestWebSearchBrave(t *testing.T) {
	clearSearchEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "bk" {
			t.Errorf("brave token header not set, got %q", r.Header.Get("X-Subscription-Token"))
		}
		if r.URL.Query().Get("q") == "" {
			t.Error("brave query param q missing")
		}
		_, _ = w.Write([]byte(`{"web":{"results":[
			{"title":"Rust","url":"https://rust-lang.org","description":"Systems language."}
		]}}`))
	}))
	defer srv.Close()
	t.Setenv("BRAVE_API_KEY", "bk")
	t.Setenv("EIGEN_BRAVE_URL", srv.URL)

	out, err := runSearch(t, "rust")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rust-lang.org") || !strings.Contains(out, "Systems language.") {
		t.Fatalf("brave result missing content:\n%s", out)
	}
}

func TestWebSearchBackendPrecedence(t *testing.T) {
	clearSearchEnv(t)
	t.Setenv("TAVILY_API_KEY", "k")
	t.Setenv("BRAVE_API_KEY", "bk")
	t.Setenv("EIGEN_WEBSEARCH_URL", "https://example.com/s?q={query}")
	if be := resolveBackend(); be == nil || be.name() != "tavily" {
		t.Fatalf("tavily should win precedence, got %v", be)
	}
}

func TestWebSearchGenericEndpoint(t *testing.T) {
	clearSearchEnv(t)
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`[
			{"title":"A","link":"https://a.test","snippet":"alpha"},
			{"name":"B","href":"https://b.test","description":"beta"}
		]`))
	}))
	defer srv.Close()
	// No {query}/%s placeholder → appended as ?q=.
	t.Setenv("EIGEN_WEBSEARCH_URL", srv.URL)

	out, err := runSearch(t, "hello world")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "hello world" {
		t.Fatalf("query not passed through, got %q", gotQuery)
	}
	// Both differently-keyed result shapes parsed.
	if !strings.Contains(out, "https://a.test") || !strings.Contains(out, "https://b.test") {
		t.Fatalf("generic results missing:\n%s", out)
	}
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("generic snippets missing:\n%s", out)
	}
}

func TestWebSearchGenericPlaceholder(t *testing.T) {
	clearSearchEnv(t)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"results":[{"title":"X","url":"https://x.test","content":"x"}]}`))
	}))
	defer srv.Close()
	t.Setenv("EIGEN_WEBSEARCH_URL", srv.URL+"/find/{query}")

	if _, err := runSearch(t, "a b"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotPath, "/find/a+b") {
		t.Fatalf("{query} placeholder not substituted, path=%q", gotPath)
	}
}

func TestWebSearchEmptyQuery(t *testing.T) {
	clearSearchEnv(t)
	t.Setenv("TAVILY_API_KEY", "k")
	def, _ := WebSearch()
	args, _ := json.Marshal(map[string]any{"query": "   "})
	if _, err := def.Run(context.Background(), args); err == nil {
		t.Fatal("empty query should error")
	}
}

func TestWebSearchCountClamp(t *testing.T) {
	clearSearchEnv(t)
	var gotMax float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotMax, _ = body["max_results"].(float64)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	t.Setenv("TAVILY_API_KEY", "k")
	t.Setenv("EIGEN_TAVILY_URL", srv.URL)

	def, _ := WebSearch()
	args, _ := json.Marshal(map[string]any{"query": "q", "count": 100})
	if _, err := def.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if int(gotMax) != maxSearchResults {
		t.Fatalf("count should clamp to %d, got %v", maxSearchResults, gotMax)
	}
}

func TestWebSearchHTTPError(t *testing.T) {
	clearSearchEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	t.Setenv("TAVILY_API_KEY", "k")
	t.Setenv("EIGEN_TAVILY_URL", srv.URL)
	if _, err := runSearch(t, "q"); err == nil {
		t.Fatal("a 500 from the backend should error")
	}
}

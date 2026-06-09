package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runFetch(t *testing.T, url string) (string, error) {
	t.Helper()
	def := Fetch()
	args, _ := json.Marshal(map[string]string{"url": url})
	return def.Run(context.Background(), args)
}

func TestFetchReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello from server"))
	}))
	defer srv.Close()

	out, err := runFetch(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "HTTP 200") {
		t.Fatalf("missing status line: %q", out)
	}
	if !strings.Contains(out, "hello from server") {
		t.Fatalf("missing body: %q", out)
	}
}

func TestFetchTruncatesLargeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		big := strings.Repeat("x", maxFetchBytes+5000)
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	out, err := runFetch(t, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[truncated]") {
		t.Fatal("oversized body should be truncated")
	}
}

func TestFetchRejectsNonHTTPScheme(t *testing.T) {
	if _, err := runFetch(t, "file:///etc/passwd"); err == nil {
		t.Fatal("file:// scheme must be rejected")
	}
	if _, err := runFetch(t, "ftp://example.com"); err == nil {
		t.Fatal("ftp:// scheme must be rejected")
	}
}

func TestFetchRequiresURL(t *testing.T) {
	def := Fetch()
	if _, err := def.Run(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing url must error")
	}
}

func TestFetchIsRegisterable(t *testing.T) {
	if Fetch().Run == nil {
		t.Fatal("fetch has nil Run")
	}
	if Fetch().ReadOnly {
		t.Fatal("fetch should be mutating (network egress) so gated mode prompts")
	}
}

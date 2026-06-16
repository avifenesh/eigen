package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultTreeFetcherAgainstLocalServer proves the real tarball fetch+extract
// path works against an HTTP server returning a gzip tarball (the codeload
// shape), without hitting GitHub.
func TestDefaultTreeFetcherAgainstLocalServer(t *testing.T) {
	tgz := buildTarGz(t, "repo-main", map[string]string{
		"README.md":                       "hi\n",
		".claude-plugin/marketplace.json": `{"name":"m","owner":{"name":"o"},"plugins":[]}`,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(tgz)
	}))
	defer srv.Close()

	// A fetcher that targets the local server instead of codeload.
	fetch := func(ctx context.Context, owner, repo, ref, destDir string) (string, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		return extractTarGz(resp.Body, destDir)
	}
	dir := t.TempDir()
	root, err := fetch(context.Background(), "o", "repo", "main", dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(root) != "repo-main" {
		t.Fatalf("root = %s, want .../repo-main", root)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "marketplace.json")); err != nil {
		t.Fatalf("extracted tree missing manifest: %v", err)
	}
}

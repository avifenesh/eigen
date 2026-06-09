package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrokRequiresKey(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("EIGEN_GROK_API_KEY", "")
	// Point the CLI auth file at a nonexistent path so the OIDC fallback fails too.
	t.Setenv("EIGEN_GROK_AUTH_FILE", filepath.Join(t.TempDir(), "nope.json"))
	if _, err := NewGrok("grok-build"); err == nil {
		t.Fatal("NewGrok should require credentials when neither key nor CLI token is present")
	}
}

func TestGrokDefaultsAndSearch(t *testing.T) {
	t.Setenv("XAI_API_KEY", "xai-test")
	t.Setenv("EIGEN_GROK_SEARCH", "")
	t.Setenv("EIGEN_GROK_SEARCH_SOURCES", "")

	// grok-build is search-capable in the catalog → search defaults to "auto".
	g, err := NewGrok("")
	if err != nil {
		t.Fatal(err)
	}
	if g.c.model != "grok-build" {
		t.Fatalf("empty model should default to grok-build, got %q", g.c.model)
	}
	if g.SearchMode() != "auto" {
		t.Fatalf("search-capable model should default search to auto, got %q", g.SearchMode())
	}
	// search_parameters should be present in the extra fields.
	extra := g.c.extra()
	sp, ok := extra["search_parameters"].(map[string]any)
	if !ok {
		t.Fatalf("auto search should emit search_parameters, got %+v", extra)
	}
	if sp["mode"] != "auto" {
		t.Fatalf("search mode wrong: %+v", sp)
	}
	srcs, _ := sp["sources"].([]map[string]any)
	if len(srcs) == 0 {
		t.Fatalf("search should include sources: %+v", sp)
	}
}

func TestGrokSearchOffOmitsParams(t *testing.T) {
	t.Setenv("XAI_API_KEY", "xai-test")
	// A non-search model defaults to off.
	g, err := NewGrok("grok-composer-2.5-fast")
	if err != nil {
		t.Fatal(err)
	}
	if g.SearchMode() != "off" {
		t.Fatalf("non-search model should default off, got %q", g.SearchMode())
	}
	if g.c.extra() != nil {
		t.Fatal("search off should omit search_parameters entirely")
	}
	// Toggling works.
	if !g.SetSearch("on") || g.SearchMode() != "on" {
		t.Fatal("SetSearch(on) should switch to on")
	}
	if g.SetSearch("bogus") {
		t.Fatal("invalid search mode should be rejected")
	}
}

func TestGrokSearchEnvOverride(t *testing.T) {
	t.Setenv("XAI_API_KEY", "xai-test")
	t.Setenv("EIGEN_GROK_SEARCH", "on")
	t.Setenv("EIGEN_GROK_SEARCH_SOURCES", "web, news")
	g, err := NewGrok("grok-composer-2.5-fast")
	if err != nil {
		t.Fatal(err)
	}
	if g.SearchMode() != "on" {
		t.Fatalf("env should force search on, got %q", g.SearchMode())
	}
	if len(g.sources) != 2 || g.sources[0] != "web" || g.sources[1] != "news" {
		t.Fatalf("env sources wrong: %v", g.sources)
	}
}

func TestGrokName(t *testing.T) {
	t.Setenv("XAI_API_KEY", "xai-test")
	g, _ := NewGrok("grok-build")
	if !strings.Contains(g.Name(), "grok-build") || !strings.Contains(g.Name(), "xai") {
		t.Fatalf("unexpected name %q", g.Name())
	}
}

func TestGrokFallsBackToCLIToken(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("EIGEN_GROK_API_KEY", "")
	t.Setenv("EIGEN_GROK_BASE_URL", "")

	// Write a fake grok-cli auth.json with one unexpired token.
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	entries := map[string]any{
		"https://auth.x.ai::client": map[string]any{
			"key":        "cli-jwt-token",
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		},
	}
	b, _ := json.Marshal(entries)
	if err := os.WriteFile(authPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EIGEN_GROK_AUTH_FILE", authPath)

	g, err := NewGrok("grok-build")
	if err != nil {
		t.Fatalf("should fall back to CLI token: %v", err)
	}
	if g.c.apiKey != "cli-jwt-token" {
		t.Fatalf("CLI token not used, got %q", g.c.apiKey)
	}
	// CLI token should target the cli-chat-proxy base.
	if g.c.baseURL != grokCLIProxyBaseURL {
		t.Fatalf("CLI fallback should use the cli proxy base, got %q", g.c.baseURL)
	}
}

func TestGrokSkipsExpiredCLIToken(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("EIGEN_GROK_API_KEY", "")
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	entries := map[string]any{
		"k": map[string]any{
			"key":        "expired-token",
			"expires_at": time.Now().Add(-time.Hour).Format(time.RFC3339),
		},
	}
	b, _ := json.Marshal(entries)
	_ = os.WriteFile(authPath, b, 0o600)
	t.Setenv("EIGEN_GROK_AUTH_FILE", authPath)

	if _, err := NewGrok("grok-build"); err == nil {
		t.Fatal("an expired CLI token should not authenticate")
	}
}

func TestGrokKeyBeatsCLIToken(t *testing.T) {
	t.Setenv("XAI_API_KEY", "xai-real-key")
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	_ = os.WriteFile(authPath, []byte(`{"k":{"key":"cli","expires_at":"2999-01-01T00:00:00Z"}}`), 0o600)
	t.Setenv("EIGEN_GROK_AUTH_FILE", authPath)

	g, err := NewGrok("grok-build")
	if err != nil {
		t.Fatal(err)
	}
	if g.c.apiKey != "xai-real-key" {
		t.Fatalf("explicit XAI_API_KEY should win over CLI token, got %q", g.c.apiKey)
	}
	if g.c.baseURL != grokDefaultBaseURL {
		t.Fatalf("with a key, base should stay the public API, got %q", g.c.baseURL)
	}
}

func TestGrokCLIProxySetsHeadersAndDisablesSearch(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("EIGEN_GROK_API_KEY", "")
	t.Setenv("EIGEN_GROK_BASE_URL", "")
	t.Setenv("EIGEN_GROK_CLIENT_VERSION", "9.9.9")
	dir := t.TempDir()
	authPath := filepath.Join(dir, "auth.json")
	_ = os.WriteFile(authPath, []byte(`{"k":{"key":"cli-tok","expires_at":"2999-01-01T00:00:00Z"}}`), 0o600)
	t.Setenv("EIGEN_GROK_AUTH_FILE", authPath)

	// grok-build is search-capable, but on the CLI proxy search must default off
	// (legacy Live Search is deprecated there).
	g, err := NewGrok("grok-build")
	if err != nil {
		t.Fatal(err)
	}
	if g.SearchMode() != "off" {
		t.Fatalf("CLI proxy should default search off, got %q", g.SearchMode())
	}
	h := g.c.extraHeaders
	if h["X-XAI-Token-Auth"] != "xai-grok-cli" {
		t.Fatalf("missing token-auth header: %v", h)
	}
	if h["x-grok-client-version"] != "9.9.9" {
		t.Fatalf("client-version header wrong: %v", h)
	}
	if h["x-grok-model-override"] != "grok-build" {
		t.Fatalf("model-override header wrong: %v", h)
	}
}

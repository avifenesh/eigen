package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestLazyClientFailedConnectCooldown proves a failed connect is cached for the
// cooldown window: repeated tool calls fail fast (returning the cached error)
// without spawning a fresh subprocess each time. Past the window a retry is
// attempted again, so a server that recovers is picked up. A zero cooldown (the
// default for directly-built literals, as the sibling tests use) must NOT cache,
// preserving the immediate-retry contract.
func TestLazyClientFailedConnectCooldown(t *testing.T) {
	now := time.Unix(0, 0)
	starts := 0
	lc := &lazyClient{
		name:         "srv",
		failCooldown: 5 * time.Second,
		clock:        func() time.Time { return now },
		dial: func(context.Context) (session, error) {
			starts++
			return nil, context.DeadlineExceeded
		},
	}

	// First call connects and fails.
	if _, err := lc.get(context.Background()); err == nil {
		t.Fatal("first connect should fail")
	}
	if starts != 1 {
		t.Fatalf("first call should attempt one connect, starts=%d", starts)
	}

	// Within the cooldown: fail fast, no new subprocess.
	now = now.Add(2 * time.Second)
	if _, err := lc.get(context.Background()); err == nil {
		t.Fatal("call within cooldown should still fail")
	}
	if starts != 1 {
		t.Fatalf("call within cooldown must not reconnect, starts=%d", starts)
	}

	// Past the cooldown: retry is attempted again.
	now = now.Add(5 * time.Second)
	if _, err := lc.get(context.Background()); err == nil {
		t.Fatal("call past cooldown should attempt a fresh connect and fail")
	}
	if starts != 2 {
		t.Fatalf("call past cooldown should reconnect, starts=%d", starts)
	}
}

// TestLazyClientCooldownClearsAfterSuccess proves that once a connect succeeds
// the cached failure is cleared, so a later read-loop death doesn't get
// short-circuited by a stale cooldown.
func TestLazyClientCooldownClearsAfterSuccess(t *testing.T) {
	now := time.Unix(0, 0)
	fail := true
	starts := 0
	lc := &lazyClient{
		name:         "srv",
		failCooldown: 5 * time.Second,
		clock:        func() time.Time { return now },
		dial: func(context.Context) (session, error) {
			starts++
			if fail {
				return nil, context.DeadlineExceeded
			}
			return newTestClient(t), nil
		},
	}
	defer lc.Close()

	if _, err := lc.get(context.Background()); err == nil {
		t.Fatal("first connect should fail")
	}

	// Server recovers; advance past the cooldown and connect successfully.
	fail = false
	now = now.Add(6 * time.Second)
	client, err := lc.get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Fatalf("recovered server should reconnect, starts=%d", starts)
	}

	// A live client is reused without re-consulting the cooldown.
	if got, err := lc.get(context.Background()); err != nil || got != client {
		t.Fatalf("live client should be reused: got=%p err=%v", got, err)
	}
	if starts != 2 {
		t.Fatalf("reuse must not reconnect, starts=%d", starts)
	}
}

// TestRemoteHTTPServerConnects proves a remote (Streamable HTTP) MCP server
// entry (url+type, no command) is CONNECTED: LoadTools dials it, lists its
// tools, and exposes them as eigen tools — the same path a stdio server takes.
// A fake httptest server speaks JSON-RPC over the Streamable HTTP transport.
func TestRemoteHTTPServerConnects(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	t.Setenv("EIGEN_WORKSPACE_BIN", missing)
	t.Setenv("EIGEN_COMPUTER_USE_BIN", missing)
	t.Setenv("EIGEN_CHROME_BRIDGE", missing)
	t.Setenv("EIGEN_NODE_BIN", missing)

	srv := newFakeHTTPMCPServer(t)
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	cfg := `{"servers":[
		{"name":"remote","type":"http","url":"` + srv.URL + `","description":"a remote connector"}
	]}`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	defs, clients, errs := LoadTools(context.Background(), path)
	if len(errs) != 0 {
		t.Fatalf("remote connect should succeed, got errors: %v", errs)
	}
	if len(clients) != 1 {
		t.Fatalf("remote server should open one client, got %d", len(clients))
	}
	if len(defs) != 1 {
		t.Fatalf("remote server should expose its one tool, got %d", len(defs))
	}
	if defs[0].Name != "remote_echo" {
		t.Fatalf("wrapped tool name = %q, want remote_echo", defs[0].Name)
	}
	// Invoke through the lazy client end-to-end.
	res, err := defs[0].Invoke(context.Background(), json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "echoed") {
		t.Fatalf("unexpected tool result: %q", res.Text)
	}
	for _, c := range clients {
		_ = c.Close()
	}
}

func TestIsRemoteServer(t *testing.T) {
	cases := []struct {
		name string
		sc   serverConfig
		want bool
	}{
		{"http type", serverConfig{Name: "r", Type: "http", URL: "https://x"}, true},
		{"sse type", serverConfig{Name: "r", Type: "sse", URL: "https://x"}, true},
		{"streamable-http", serverConfig{Name: "r", Type: "streamable-http", URL: "https://x"}, true},
		{"bare url, no type", serverConfig{Name: "r", URL: "https://x"}, true},
		{"type cased + spaced", serverConfig{Name: "r", Type: " HTTP ", URL: "https://x"}, true},
		{"stdio command wins over url", serverConfig{Name: "r", Command: []string{"node", "x.js"}, URL: "https://x"}, false},
		{"plain stdio", serverConfig{Name: "r", Command: []string{"node", "x.js"}}, false},
		{"empty everything", serverConfig{Name: "r"}, false},
	}
	for _, c := range cases {
		if got := isRemoteServer(c.sc); got != c.want {
			t.Errorf("%s: isRemoteServer = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestFirstSentenceRuneTruncation proves the long-line truncation cuts on rune
// boundaries: a first line of multi-byte runes (accented/CJK/emoji) longer than
// 120 runes must yield valid UTF-8 ending in the ellipsis, never a broken
// half-rune from a byte slice. Short and sentence/line cases are kept as a
// regression guard for the surrounding behavior.
func TestFirstSentenceRuneTruncation(t *testing.T) {
	// 130 accented runes — each is 2 bytes in UTF-8, so a byte slice s[:117]
	// would land mid-rune and corrupt the output.
	long := strings.Repeat("é", 130)
	got := firstSentence(long)
	if !utf8.ValidString(got) {
		t.Fatalf("firstSentence produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
	if r := []rune(got); len(r) != 118 { // 117 kept + ellipsis
		t.Errorf("expected 118 runes (117 + ellipsis), got %d", len(r))
	}

	cases := []struct {
		name, in, want string
	}{
		{"empty", "   ", ""},
		{"first line only", "drive Chrome\nmore detail", "drive Chrome"},
		{"first sentence", "isolated Linux sandbox. Run apps.", "isolated Linux sandbox"},
		{"short unicode untouched", "café ☕", "café ☕"},
	}
	for _, c := range cases {
		if got := firstSentence(c.in); got != c.want {
			t.Errorf("%s: firstSentence(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

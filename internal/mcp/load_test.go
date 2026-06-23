package mcp

import (
	"context"
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
		command:      []string{"fake"},
		failCooldown: 5 * time.Second,
		clock:        func() time.Time { return now },
		connect: func(context.Context, []string, []string) (*Client, error) {
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
		command:      []string{"fake"},
		failCooldown: 5 * time.Second,
		clock:        func() time.Time { return now },
		connect: func(context.Context, []string, []string) (*Client, error) {
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

// TestRemoteHTTPServerReportedUnsupported proves that an http/sse MCP server
// entry (url+type, no command) is RECOGNIZED rather than silently dropped:
// LoadTools must surface a clear "remote MCP (http/sse) not yet supported"
// error instead of either connecting it or rejecting it as a malformed stdio
// entry. This guards the plugin layer's url/type fields round-tripping into
// serverConfig.
func TestRemoteHTTPServerReportedUnsupported(t *testing.T) {
	// Neutralize built-in server auto-detection so the test sees only our config
	// (otherwise a dev box with the workspace/chrome helpers installed would add
	// extra clients/errors and make the assertions flaky). Each override points
	// at a non-existent path, which makes the *Binary() detectors return "".
	missing := filepath.Join(t.TempDir(), "nope")
	t.Setenv("EIGEN_WORKSPACE_BIN", missing)
	t.Setenv("EIGEN_COMPUTER_USE_BIN", missing)
	t.Setenv("EIGEN_CHROME_BRIDGE", missing)
	t.Setenv("EIGEN_NODE_BIN", missing)

	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	cfg := `{"servers":[
		{"name":"remote","type":"http","url":"https://example.com/mcp"},
		{"name":"remote-sse","type":"sse","url":"https://example.com/sse"},
		{"name":"remote-bareurl","url":"https://example.com/bare"}
	]}`
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	defs, clients, errs := LoadTools(context.Background(), path)

	// No stdio command means nothing connects; but the entries must NOT vanish
	// silently — each must produce an explicit "not yet supported" error.
	if len(defs) != 0 {
		t.Fatalf("remote servers should expose no tools, got %d", len(defs))
	}
	if len(clients) != 0 {
		t.Fatalf("remote servers should open no clients, got %d", len(clients))
	}
	if len(errs) != 3 {
		t.Fatalf("each remote server should be reported once, got %d errors: %v", len(errs), errs)
	}
	for _, err := range errs {
		msg := err.Error()
		if !strings.Contains(msg, "remote MCP") || !strings.Contains(msg, "not yet supported") {
			t.Errorf("error should explain remote MCP is unsupported, got: %v", err)
		}
		// The generic malformed-entry message must NOT be what we get — that's
		// the silent-drop bug this fix closes.
		if strings.Contains(msg, "empty name or command") {
			t.Errorf("remote server misreported as a malformed stdio entry: %v", err)
		}
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

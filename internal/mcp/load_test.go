package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

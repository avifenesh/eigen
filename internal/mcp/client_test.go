package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeServer reads JSON-RPC requests from r and writes canned responses to w,
// implementing the minimal initialize/tools.list/tools.call surface.
func fakeServer(r io.Reader, w io.Writer) {
	enc := json.NewEncoder(w)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var req struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			continue
		}
		if req.ID == 0 {
			continue // notification (e.g. initialized)
		}
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			resp["result"] = map[string]any{"protocolVersion": protocolVersion}
		case "tools/list":
			resp["result"] = map[string]any{
				"tools": []map[string]any{
					{"name": "echo", "description": "echo text", "inputSchema": map[string]any{"type": "object"}},
					{"name": "peek", "description": "read state", "inputSchema": map[string]any{"type": "object"},
						"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": false}},
					{"name": "nuke", "description": "delete state", "inputSchema": map[string]any{"type": "object"},
						"annotations": map[string]any{"readOnlyHint": true, "destructiveHint": true}},
				},
			}
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.Name == "shot" {
				// A 1x1 PNG as a base64 image content block.
				resp["result"] = map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "screenshot taken"},
						{"type": "image", "mimeType": "image/png", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="},
					},
				}
			} else {
				resp["result"] = map[string]any{
					"content": []map[string]any{{"type": "text", "text": "called " + p.Name + " with " + string(p.Arguments)}},
				}
			}
		default:
			resp["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		_ = enc.Encode(resp)
	}
}

// newTestClient wires a Client to an in-memory fake server.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	cr, cw := io.Pipe() // client -> server
	sr, sw := io.Pipe() // server -> client
	go fakeServer(cr, sw)
	return newClient(cw, sr, func() error { cw.Close(); sw.Close(); return nil })
}

func TestCallDeletesPendingOnContextCancellation(t *testing.T) {
	c := &Client{
		enc:     json.NewEncoder(io.Discard),
		pending: map[int]chan rpcResponse{},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.call(ctx, "tools/list", map[string]any{}, nil); err == nil {
		t.Fatal("expected canceled context error")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) != 0 {
		t.Fatalf("pending request leaked after cancellation: %d", len(c.pending))
	}
}

func TestLateMCPResponseAfterCancellationDoesNotBlockReader(t *testing.T) {
	pr, pw := io.Pipe()
	c := newClient(io.Discard, pr, func() error { return pw.Close() })
	defer c.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.call(ctx, "tools/list", map[string]any{}, nil); err == nil {
		t.Fatal("expected canceled context error")
	}

	done := make(chan struct{})
	go func() {
		_, _ = pw.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}` + "\n"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("late response write blocked; read loop likely wedged")
	}
}

func TestInitializeAndListTools(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 3 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	// Annotations are parsed when present.
	if tools[1].Name != "peek" || tools[1].Annotations == nil || !tools[1].Annotations.ReadOnlyHint {
		t.Fatalf("peek should carry readOnlyHint: %+v", tools[1].Annotations)
	}
}

func TestWrapMapsReadOnlyHint(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ToolSpec{}
	for _, sp := range tools {
		byName[sp.Name] = sp
	}
	// No annotations → mutating (fail safe).
	if d := wrap(c, "srv", "srv gist", byName["echo"]); d.ReadOnly {
		t.Error("echo (no hint) should be mutating")
	}
	// readOnly + not destructive → read-only, auto-runs in gated mode.
	if d := wrap(c, "srv", "srv gist", byName["peek"]); !d.ReadOnly {
		t.Error("peek (readOnlyHint, non-destructive) should be read-only")
	}
	// readOnly BUT destructive → stay mutating (the destructive flag wins).
	if d := wrap(c, "srv", "srv gist", byName["nuke"]); d.ReadOnly {
		t.Error("nuke (destructiveHint) must stay mutating despite readOnlyHint")
	}
	// Name is server-prefixed and sanitized.
	if d := wrap(c, "srv", "srv gist", byName["echo"]); d.Name != "srv_echo" {
		t.Errorf("wrapped name = %q, want srv_echo", d.Name)
	}
}

func TestCallTool(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := c.CallTool(ctx, "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != `called echo with {"x":1}` {
		t.Fatalf("unexpected result: %q", out)
	}
}

func TestCallToolUnknownMethodErrors(t *testing.T) {
	c := newTestClient(t)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// "resources/list" is not handled by the fake server -> JSON-RPC error.
	if err := c.call(ctx, "resources/list", map[string]any{}, nil); err == nil {
		t.Fatal("an unknown method should return the server's error")
	}
}

func TestCallContextCancel(t *testing.T) {
	// A server that never responds: writes are discarded and the read end never
	// yields, so the call must respect context cancellation.
	nr, nw := io.Pipe()
	defer nw.Close()
	c := newClient(io.Discard, nr, func() error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.call(ctx, "initialize", map[string]any{}, nil); err == nil {
		t.Fatal("expected context deadline error")
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("weird name/v1.2"); got != "weird_name_v1_2" {
		t.Fatalf("sanitize wrong: %q", got)
	}
}

func TestToolAllowed(t *testing.T) {
	cases := []struct {
		tools, exclude []string
		name           string
		want           bool
	}{
		// no filters: everything allowed
		{nil, nil, "anything", true},
		// allowlist exact
		{[]string{"workspace_start"}, nil, "workspace_start", true},
		{[]string{"workspace_start"}, nil, "workspace_stop", false},
		// allowlist prefix
		{[]string{"workspace_terminal_*"}, nil, "workspace_terminal_read", true},
		{[]string{"workspace_terminal_*"}, nil, "workspace_click", false},
		// exclude wins over allow
		{[]string{"workspace_*"}, []string{"workspace_cleanup_stale"}, "workspace_cleanup_stale", false},
		{[]string{"workspace_*"}, []string{"workspace_cleanup_stale"}, "workspace_start", true},
		// exclude alone
		{nil, []string{"profile_*"}, "profile_put", false},
		{nil, []string{"profile_*"}, "workspace_start", true},
	}
	for _, c := range cases {
		sc := serverConfig{Tools: c.tools, ExcludeTools: c.exclude}
		if got := toolAllowed(sc, c.name); got != c.want {
			t.Errorf("toolAllowed(tools=%v exclude=%v, %q) = %v, want %v", c.tools, c.exclude, c.name, got, c.want)
		}
	}
}

func TestSlimSchema(t *testing.T) {
	in := json.RawMessage(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title": "BigParams",
		"type": "object",
		"properties": {
			"x": {"title": "X", "type": "integer", "description": "keep me"},
			"nested": {"type": "object", "properties": {"y": {"$schema": "x", "type": "string"}}}
		},
		"required": ["x"]
	}`)
	out := slimSchema(in)
	s := string(out)
	if strings.Contains(s, "$schema") || strings.Contains(s, "title") {
		t.Fatalf("noise keys not stripped: %s", s)
	}
	if !strings.Contains(s, "keep me") || !strings.Contains(s, `"required":["x"]`) {
		t.Fatalf("schema content damaged: %s", s)
	}
	// Non-object input passes through unchanged.
	if got := slimSchema(json.RawMessage(`not json`)); string(got) != "not json" {
		t.Fatalf("invalid input should pass through, got %s", got)
	}
	if got := slimSchema(nil); got != nil {
		t.Fatalf("nil should pass through, got %s", got)
	}
}

func TestCallToolRichPreservesImages(t *testing.T) {
	c := newTestClient(t)
	res, err := c.CallToolRich(context.Background(), "shot", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "screenshot taken" {
		t.Fatalf("text = %q", res.Text)
	}
	if len(res.Images) != 1 {
		t.Fatalf("want 1 image, got %d", len(res.Images))
	}
	if res.Images[0].MediaType != "image/png" || len(res.Images[0].Data) == 0 {
		t.Fatalf("image not decoded: %+v", res.Images[0])
	}
	// CallTool (text-only) still works and drops the image.
	txt, err := c.CallTool(context.Background(), "shot", json.RawMessage(`{}`))
	if err != nil || txt != "screenshot taken" {
		t.Fatalf("CallTool text path: %q %v", txt, err)
	}
}

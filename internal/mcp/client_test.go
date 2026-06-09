package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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
				},
			}
		case "tools/call":
			var p struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(req.Params, &p)
			resp["result"] = map[string]any{
				"content": []map[string]any{{"type": "text", "text": "called " + p.Name + " with " + string(p.Arguments)}},
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
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("unexpected tools: %+v", tools)
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

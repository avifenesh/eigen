package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeHTTPMCP is a minimal in-process MCP server speaking the Streamable HTTP
// transport, for transport tests. It answers initialize / tools/list /
// tools/call, assigns a session id, and (when sse is set) replies as an SSE
// event-stream instead of inline JSON.
type fakeHTTPMCP struct {
	*httptest.Server
	sse        bool   // reply as text/event-stream
	requireSID bool   // require the client to echo the session id after initialize
	wantAuth   string // when set, demand this Authorization header (else 401)
	sessionID  string
}

func newFakeHTTPMCPServer(t *testing.T) *fakeHTTPMCP {
	return newFakeHTTPMCPServerOpts(t, false, "")
}

func newFakeHTTPMCPServerOpts(t *testing.T, sse bool, wantAuth string) *fakeHTTPMCP {
	t.Helper()
	f := &fakeHTTPMCP{sse: sse, wantAuth: wantAuth, sessionID: "sess-123"}
	f.Server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeHTTPMCP) handle(w http.ResponseWriter, r *http.Request) {
	if f.wantAuth != "" && r.Header.Get("Authorization") != f.wantAuth {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp", resource_metadata="`+f.URL+`/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	_ = json.Unmarshal(body, &req)

	// Notifications (no id) get a bare 202.
	if req.Method == "notifications/initialized" || req.ID == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var result string
	switch req.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", f.sessionID)
		result = `{"instructions":"a fake remote server","serverInfo":{"name":"fake-remote"}}`
	case "tools/list":
		result = `{"tools":[{"name":"echo","description":"echo back","inputSchema":{"type":"object"}}]}`
	case "tools/call":
		result = `{"content":[{"type":"text","text":"echoed"}],"isError":false}`
	default:
		result = `{}`
	}
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, req.ID, result)

	if f.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Emit a keep-alive comment + an unrelated notification frame first, to
		// prove the reader skips noise and matches by id.
		fmt.Fprintf(w, ": keep-alive\n\n")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"x/note\"}\n\n")
		fmt.Fprintf(w, "data: %s\n\n", payload)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, payload)
}

func TestHTTPClientInlineJSON(t *testing.T) {
	srv := newFakeHTTPMCPServer(t)
	defer srv.Close()

	c, err := ConnectHTTP(context.Background(), httpDialer{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.ServerName() != "fake-remote" {
		t.Errorf("server name = %q, want fake-remote", c.ServerName())
	}
	if !strings.Contains(c.Instructions(), "fake remote") {
		t.Errorf("instructions = %q", c.Instructions())
	}
	if c.sessionID != "sess-123" {
		t.Errorf("session id not captured: %q", c.sessionID)
	}
	tools, err := c.ListTools(context.Background())
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("ListTools = %v, %v", tools, err)
	}
	res, err := c.CallToolRich(context.Background(), "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "echoed" {
		t.Fatalf("tool result = %q, want echoed", res.Text)
	}
}

func TestHTTPClientSSEResponse(t *testing.T) {
	srv := newFakeHTTPMCPServerOpts(t, true, "")
	defer srv.Close()

	c, err := ConnectHTTP(context.Background(), httpDialer{URL: srv.URL})
	if err != nil {
		t.Fatalf("connect over SSE: %v", err)
	}
	defer c.Close()

	// The response rides on an SSE data: frame after a comment + a noise frame;
	// the reader must skip those and match the response by id.
	res, err := c.CallToolRich(context.Background(), "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "echoed" {
		t.Fatalf("SSE tool result = %q, want echoed", res.Text)
	}
}

func TestHTTPClientSendsAuthHeader(t *testing.T) {
	srv := newFakeHTTPMCPServerOpts(t, false, "Bearer tok-42")
	defer srv.Close()

	// Without auth → 401 surfaced as *httpAuthError carrying the challenge.
	_, err := ConnectHTTP(context.Background(), httpDialer{URL: srv.URL})
	if err == nil {
		t.Fatal("expected 401 without auth")
	}
	var authErr *httpAuthError
	if !asHTTPAuthError(err, &authErr) {
		t.Fatalf("want *httpAuthError, got %T: %v", err, err)
	}
	if !strings.Contains(authErr.WWWAuthenticate(), "oauth-protected-resource") {
		t.Errorf("challenge should carry resource metadata, got %q", authErr.WWWAuthenticate())
	}

	// With the right bearer → connects.
	c, err := ConnectHTTP(context.Background(), httpDialer{
		URL:        srv.URL,
		AuthHeader: func() string { return "Bearer tok-42" },
	})
	if err != nil {
		t.Fatalf("connect with auth: %v", err)
	}
	defer c.Close()
	if c.ServerName() != "fake-remote" {
		t.Errorf("server name = %q", c.ServerName())
	}
}

// asHTTPAuthError is a tiny errors.As shim kept local so the test file doesn't
// need the errors import just for one call.
func asHTTPAuthError(err error, target **httpAuthError) bool {
	for err != nil {
		if e, ok := err.(*httpAuthError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

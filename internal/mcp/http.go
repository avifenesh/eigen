package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Remote MCP transport (Streamable HTTP, with SSE responses). A remote server —
// the shape a "connector" (Google Workspace, Slack, Notion, …) takes — exposes a
// single HTTP endpoint that speaks JSON-RPC: the client POSTs a request and the
// server answers either inline as application/json OR as a text/event-stream
// (SSE) carrying one-or-more `data:` events, the last of which holds the
// JSON-RPC response. This is the MCP "Streamable HTTP" transport
// (https://modelcontextprotocol.io/specification — 2025-03-26 revision); we do
// NOT implement the older HTTP+SSE dual-endpoint transport.
//
// httpClient satisfies the same `session` interface as the stdio *Client, so the
// loader + lazyClient wire a remote connector identically to a local server.

// httpRequestTimeout bounds a single JSON-RPC round trip over HTTP. Tool calls
// to a remote connector can be slow (the server may itself call a third-party
// API), so this is generous; the caller's ctx still applies and wins if shorter.
const httpRequestTimeout = 120 * time.Second

// httpClient is a connected remote MCP session over Streamable HTTP.
type httpClient struct {
	url           string
	httpDo        *http.Client
	authHeader    func() string     // returns the current "Authorization" value (may be "")
	staticHeaders map[string]string // extra headers sent on every request

	mu        sync.Mutex
	nextID    int
	sessionID string // server-assigned Mcp-Session-Id (set at initialize), echoed back
	dead      bool

	instructions string
	serverName   string
}

// httpDialer carries the inputs to open a remote MCP session.
type httpDialer struct {
	URL string
	// AuthHeader, when set, is called before each request to get the current
	// Authorization header value (e.g. "Bearer <token>"). It's a func, not a
	// static string, so a token refreshed mid-session is picked up on the next
	// call without re-dialing. Returns "" for no auth (public server).
	AuthHeader func() string
	// HTTPHeaders are extra static headers sent on every request.
	HTTPHeaders map[string]string
	// Client overrides the http.Client (tests inject a transport). nil = default.
	Client *http.Client
}

// ConnectHTTP opens a remote MCP server over Streamable HTTP and performs the
// initialize handshake. The returned httpClient is a `session` like the stdio
// *Client.
func ConnectHTTP(ctx context.Context, d httpDialer) (*httpClient, error) {
	if strings.TrimSpace(d.URL) == "" {
		return nil, fmt.Errorf("mcp: empty remote URL")
	}
	hc := d.Client
	if hc == nil {
		hc = &http.Client{Timeout: httpRequestTimeout}
	}
	c := &httpClient{
		url:           d.URL,
		httpDo:        hc,
		authHeader:    d.AuthHeader,
		staticHeaders: d.HTTPHeaders,
	}
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *httpClient) Instructions() string { return c.instructions }
func (c *httpClient) ServerName() string   { return c.serverName }

func (c *httpClient) alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.dead
}

func (c *httpClient) Close() error {
	c.mu.Lock()
	c.dead = true
	c.mu.Unlock()
	return nil
}

func (c *httpClient) initialize(ctx context.Context) error {
	var res initResult
	// initialize is the one call whose response headers we read (for the
	// server-assigned session id), so it goes through doRPC with captureSession.
	if err := c.rpc(ctx, "initialize", initializeParams(), &res, true); err != nil {
		return err
	}
	c.instructions = strings.TrimSpace(res.Instructions)
	c.serverName = strings.TrimSpace(res.ServerInfo.Name)
	// MCP requires an `initialized` notification after a successful initialize.
	return c.notify(ctx, "notifications/initialized", map[string]any{})
}

func (c *httpClient) ListTools(ctx context.Context) ([]ToolSpec, error) {
	var out struct {
		Tools []ToolSpec `json:"tools"`
	}
	if err := c.rpc(ctx, "tools/list", map[string]any{}, &out, false); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

func (c *httpClient) CallToolRich(ctx context.Context, name string, args json.RawMessage) (ToolResult, error) {
	var out rawToolResult
	if err := c.rpc(ctx, "tools/call", toolCallParams(name, args), &out, false); err != nil {
		return ToolResult{}, err
	}
	return decodeToolResult(out, name, c.serverName)
}

// notify POSTs a JSON-RPC notification (no id, no response expected). The server
// answers 202 Accepted; we ignore the body.
func (c *httpClient) notify(ctx context.Context, method string, params any) error {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	resp, err := c.post(ctx, body)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

// rpc sends a JSON-RPC request and decodes the matching response into result.
// When captureSession is true the server-assigned Mcp-Session-Id response header
// is stored for subsequent requests (used on initialize).
func (c *httpClient) rpc(ctx context.Context, method string, params any, result any, captureSession bool) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	resp, err := c.post(ctx, body)
	if err != nil {
		c.markDead()
		return err
	}
	defer resp.Body.Close()

	if captureSession {
		if sid := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id")); sid != "" {
			c.mu.Lock()
			c.sessionID = sid
			c.mu.Unlock()
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return &httpAuthError{status: resp.StatusCode, wwwAuth: resp.Header.Get("WWW-Authenticate")}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("mcp http %s: HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	rpcResp, err := readJSONRPCResponse(resp, id)
	if err != nil {
		return fmt.Errorf("mcp http %s: %w", method, err)
	}
	if rpcResp.Error != nil {
		return rpcResp.Error
	}
	if result != nil && len(rpcResp.Result) > 0 {
		return json.Unmarshal(rpcResp.Result, result)
	}
	return nil
}

// post issues the HTTP POST with the headers every MCP Streamable-HTTP request
// needs (content type, the dual Accept that lets the server pick json or SSE,
// the session id once known, and auth/static headers).
func (c *httpClient) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Accept BOTH so a server may answer inline or stream — required by the spec.
	req.Header.Set("Accept", "application/json, text/event-stream")
	c.mu.Lock()
	sid := c.sessionID
	c.mu.Unlock()
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}
	if c.authHeader != nil {
		if v := strings.TrimSpace(c.authHeader()); v != "" {
			req.Header.Set("Authorization", v)
		}
	}
	for k, v := range c.staticHeaders {
		req.Header.Set(k, v)
	}
	return c.httpDo.Do(req)
}

func (c *httpClient) markDead() {
	c.mu.Lock()
	c.dead = true
	c.mu.Unlock()
}

// readJSONRPCResponse reads a JSON-RPC response from an HTTP response that is
// either application/json (a single object) or text/event-stream (SSE: one or
// more `data:` lines; we return the first data payload whose id matches, which
// for a request is the response). wantID is the request id we're matching.
func readJSONRPCResponse(resp *http.Response, wantID int) (rpcResponse, error) {
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") {
		return readSSEResponse(resp.Body, wantID)
	}
	// Inline JSON. Some servers wrap a single response; others (batch) an array —
	// match the id either way.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRPCLineBytes))
	if err != nil {
		return rpcResponse{}, err
	}
	return matchResponse(data, wantID)
}

// readSSEResponse scans an SSE stream for the JSON-RPC response with wantID. SSE
// frames are blank-line-delimited; the payload rides on `data:` lines (possibly
// multi-line, concatenated). The server may emit notifications/requests before
// the response; we skip any frame whose id doesn't match.
func readSSEResponse(r io.Reader, wantID int) (rpcResponse, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxRPCLineBytes)
	var data strings.Builder
	flush := func() (rpcResponse, bool, error) {
		if data.Len() == 0 {
			return rpcResponse{}, false, nil
		}
		payload := data.String()
		data.Reset()
		resp, err := matchResponse([]byte(payload), wantID)
		if err != nil {
			return rpcResponse{}, false, nil // not our id (or noise) — keep scanning
		}
		return resp, true, nil
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" { // frame boundary
			if resp, ok, _ := flush(); ok {
				return resp, nil
			}
			continue
		}
		if strings.HasPrefix(line, ":") { // SSE comment / keep-alive
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimPrefix(v, " "))
		}
		// other SSE fields (event:, id:, retry:) are ignored
	}
	if err := sc.Err(); err != nil {
		return rpcResponse{}, err
	}
	// Stream ended; try a final unterminated frame.
	if resp, ok, _ := flush(); ok {
		return resp, nil
	}
	return rpcResponse{}, fmt.Errorf("mcp: no JSON-RPC response for id %d in event stream", wantID)
}

// matchResponse parses data as a single JSON-RPC response or an array of them
// and returns the one whose id == wantID.
func matchResponse(data []byte, wantID int) (rpcResponse, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return rpcResponse{}, fmt.Errorf("empty response")
	}
	if data[0] == '[' {
		var arr []rpcResponse
		if err := json.Unmarshal(data, &arr); err != nil {
			return rpcResponse{}, err
		}
		for _, r := range arr {
			if r.ID == wantID {
				return r, nil
			}
		}
		return rpcResponse{}, fmt.Errorf("id %d not in batch", wantID)
	}
	var r rpcResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return rpcResponse{}, err
	}
	if r.ID != wantID {
		return rpcResponse{}, fmt.Errorf("id mismatch: got %d want %d", r.ID, wantID)
	}
	return r, nil
}

// httpAuthError signals a 401 from a remote MCP server — the trigger for the
// OAuth flow (Phase 2). It carries the WWW-Authenticate header so the OAuth
// layer can discover the authorization server.
type httpAuthError struct {
	status  int
	wwwAuth string
}

func (e *httpAuthError) Error() string {
	if e.wwwAuth != "" {
		return fmt.Sprintf("mcp: remote server requires authorization (HTTP %d; WWW-Authenticate: %s)", e.status, e.wwwAuth)
	}
	return fmt.Sprintf("mcp: remote server requires authorization (HTTP %d)", e.status)
}

// WWWAuthenticate exposes the challenge header for the OAuth discovery layer.
func (e *httpAuthError) WWWAuthenticate() string { return e.wwwAuth }

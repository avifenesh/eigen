// Package mcp is a minimal Model Context Protocol (MCP) stdio client: it speaks
// JSON-RPC 2.0 over newline-delimited messages, performs the initialize
// handshake, lists a server's tools, and calls them — so eigen can expose any
// MCP server's tools as native tools. The transport is abstracted (io.Reader /
// io.Writer) so the client is testable without spawning a process.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

const protocolVersion = "2024-11-05"

// ToolSpec describes one MCP tool.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message) }

// Client is a connected MCP session.
type Client struct {
	enc *json.Encoder

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcResponse

	closeFn func() error
}

// newClient starts the read loop over r and writes requests to w.
func newClient(w io.Writer, r io.Reader, closeFn func() error) *Client {
	c := &Client{
		enc:     json.NewEncoder(w),
		pending: map[int]chan rpcResponse{},
		closeFn: closeFn,
	}
	go c.readLoop(r)
	return c
}

// Connect spawns an MCP server process and performs the initialize handshake.
func Connect(ctx context.Context, command []string, env []string) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("mcp: empty command")
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := newClient(stdin, stdout, func() error {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		return cmd.Wait()
	})
	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) readLoop(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if json.Unmarshal(line, &resp) != nil || resp.ID == 0 {
			continue // notification or noise
		}
		c.mu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- resp
		}
	}
	// Stream closed: fail any in-flight calls.
	c.mu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
}

// call sends a request and waits for the matching response.
func (c *Client) call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	err := c.enc.Encode(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	c.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case resp, ok := <-ch:
		if !ok {
			return fmt.Errorf("mcp: connection closed during %s", method)
		}
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			return json.Unmarshal(resp.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// notify sends a notification (no response expected).
func (c *Client) notify(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enc.Encode(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "eigen", "version": "0.1.0"},
	}
	if err := c.call(ctx, "initialize", params, nil); err != nil {
		return err
	}
	return c.notify("notifications/initialized", map[string]any{})
}

// ListTools returns the server's advertised tools.
func (c *Client) ListTools(ctx context.Context) ([]ToolSpec, error) {
	var out struct {
		Tools []ToolSpec `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// CallTool invokes a tool and returns its text content (concatenated).
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = json.RawMessage(args)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", params, &out); err != nil {
		return "", err
	}
	var text string
	for _, c := range out.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	if out.IsError {
		return "", fmt.Errorf("%s", text)
	}
	return text, nil
}

// Close shuts down the connection and the underlying process.
func (c *Client) Close() error {
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}
